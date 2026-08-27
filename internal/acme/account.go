package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	xacme "golang.org/x/crypto/acme"
)

// accountKeyPEMType 账户私钥的 PEM 块类型。账户密钥与证书密钥无关，
// 固定使用 ECDSA P-256：四家 CA 均支持，且签名体积小。
const accountKeyPEMType = "EC PRIVATE KEY"

// acmeClientFor 为指定申请人构建可用的 ACME 客户端，并确保其账户已在目标
// CA 完成注册。
//
// 账户复用是关键：ACME 账户由密钥对唯一标识，若每次签发都新建账户，
// 会迅速触发 CA 的账户注册速率限制。因此这里的语义是
// 「有可复用账户就复用，没有才注册」，并把结果持久化。
//
// 返回的 *xacme.Client 已带好账户密钥与 kid，可直接下单。
func acmeClientFor(
	ctx context.Context,
	applicant *model.CertApplicant,
	directoryURL string,
	contactEmail string,
) (*xacme.Client, error) {
	provider := LookupProvider(applicant.Provider)

	// EAB 前置校验：ZeroSSL / Google 必须携带 EAB 才能建账户。
	// 提前拦截可以给出明确的中文原因，而不是把 CA 的 403 原样抛给用户。
	if provider.RequiresEAB && (strings.TrimSpace(applicant.EABKID) == "" ||
		strings.TrimSpace(applicant.EABHMACKey) == "") {
		return nil, fmt.Errorf(
			"%s 要求 External Account Binding，请先在其控制台获取 EAB Key ID 与 HMAC Key 并填入申请人配置",
			provider.Name)
	}

	key, err := ensureAccountKey(applicant)
	if err != nil {
		return nil, err
	}

	client := &xacme.Client{
		Key:          key,
		DirectoryURL: directoryURL,
		UserAgent:    "DnsCat/1.0 (+https://github.com/dnscat)",
	}

	// 账户与 Directory 一一绑定：同一密钥在 staging 与 prod 下是两个不同账户。
	// Directory 变了就必须重新注册，否则拿旧 kid 去请求会被拒。
	reusable := applicant.ACMEAccountURI != "" &&
		applicant.ACMEDirectoryURL == directoryURL &&
		applicant.ACMEAccountState == model.ACMEAccountValid

	if reusable {
		client.KID = xacme.KeyID(applicant.ACMEAccountURI)
		// 轻量探活：确认账户在 CA 侧仍然有效（可能已被吊销或停用）。
		if _, err := client.GetReg(ctx, applicant.ACMEAccountURI); err == nil {
			return client, nil
		}
		log.Printf("[ACME] 申请人 #%d 的既有账户已失效，将重新注册", applicant.ID)
		client.KID = ""
	}

	if err := registerAccount(ctx, client, applicant, provider, directoryURL, contactEmail); err != nil {
		persistAccountFailure(applicant, err)
		return nil, err
	}

	return client, nil
}

// ensureAccountKey 取出或生成申请人的 ACME 账户私钥。
// 首次生成后立刻落库，保证后续签发复用同一账户。
func ensureAccountKey(applicant *model.CertApplicant) (crypto.Signer, error) {
	if strings.TrimSpace(applicant.ACMEAccountKey) != "" {
		key, err := parseECPrivateKeyPEM(applicant.ACMEAccountKey)
		if err == nil {
			return key, nil
		}
		// 存量数据损坏时重新生成，而不是让签发彻底卡死。
		log.Printf("[ACME] 申请人 #%d 的账户私钥解析失败，将重新生成: %v", applicant.ID, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成 ACME 账户密钥失败: %w", err)
	}

	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("序列化 ACME 账户密钥失败: %w", err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: accountKeyPEMType, Bytes: der}))

	applicant.ACMEAccountKey = keyPEM
	// 密钥先落库再注册：若注册中途失败，下次重试仍用同一密钥，
	// 避免在 CA 侧留下一堆无主账户。
	if database.DB != nil {
		if err := database.DB.Model(applicant).
			Update("acme_account_key", keyPEM).Error; err != nil {
			return nil, fmt.Errorf("保存 ACME 账户密钥失败: %w", err)
		}
	}

	return key, nil
}

func parseECPrivateKeyPEM(keyPEM string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, fmt.Errorf("账户私钥 PEM 解码失败")
	}
	switch block.Type {
	case accountKeyPEMType:
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signer, ok := parsed.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("账户私钥类型不支持签名")
		}
		return signer, nil
	default:
		return nil, fmt.Errorf("未知的账户私钥 PEM 类型: %s", block.Type)
	}
}

// registerAccount 向 CA 注册账户并持久化结果。
func registerAccount(
	ctx context.Context,
	client *xacme.Client,
	applicant *model.CertApplicant,
	provider Provider,
	directoryURL string,
	contactEmail string,
) error {
	email := strings.TrimSpace(applicant.Email)
	if email == "" {
		email = strings.TrimSpace(contactEmail)
	}

	account := &xacme.Account{}
	if email != "" {
		account.Contact = []string{"mailto:" + email}
	} else {
		// RFC 8555 允许不带联系人，但那样 CA 无法发送到期提醒。
		// 提示一次，避免使用者在证书静默过期后才发现。
		log.Printf("[ACME] 申请人与系统设置 acme_email 均未填写联系邮箱，"+
			"将以无联系人方式注册 %s 账户，CA 无法发送证书到期提醒", provider.Name)
	}

	// EAB：把 CA 控制台给的 HMAC Key（base64url）解码后交给客户端，
	// 由其按 RFC 8555 §7.3.4 生成外部账户绑定 JWS。
	if strings.TrimSpace(applicant.EABKID) != "" &&
		strings.TrimSpace(applicant.EABHMACKey) != "" {
		hmacKey, err := decodeEABKey(applicant.EABHMACKey)
		if err != nil {
			return fmt.Errorf("EAB HMAC Key 格式无效（应为 base64url 编码）: %w", err)
		}
		account.ExternalAccountBinding = &xacme.ExternalAccountBinding{
			KID: strings.TrimSpace(applicant.EABKID),
			Key: hmacKey,
		}
	}

	// prompt 返回 true 表示同意服务条款。这是使用 ACME 的前提，
	// 由用户在控制台创建申请人这一动作代表其接受。
	registered, err := client.Register(ctx, account, xacme.AcceptTOS)
	if err != nil {
		return fmt.Errorf("向 %s 注册 ACME 账户失败: %w", provider.Name, err)
	}

	client.KID = xacme.KeyID(registered.URI)

	now := time.Now()
	applicant.ACMEAccountURI = registered.URI
	applicant.ACMEDirectoryURL = directoryURL
	applicant.ACMEAccountState = model.ACMEAccountValid
	applicant.ACMEAccountError = ""
	applicant.ACMERegisteredAt = &now

	if database.DB != nil {
		if err := database.DB.Model(applicant).Updates(map[string]interface{}{
			"acme_account_uri":   registered.URI,
			"acme_directory_url": directoryURL,
			"acme_account_state": model.ACMEAccountValid,
			"acme_account_error": "",
			"acme_registered_at": &now,
			"updated_at":         now,
		}).Error; err != nil {
			log.Printf("[ACME] 保存申请人 #%d 的账户注册结果失败: %v", applicant.ID, err)
		}
	}

	log.Printf("[ACME] 申请人 #%d (%s) 已在 %s 注册账户: %s",
		applicant.ID, email, provider.Name, registered.URI)
	return nil
}

// decodeEABKey 解码 CA 控制台提供的 HMAC 密钥。
// 各家给出的编码不完全一致：ZeroSSL 用 base64url 无填充，
// 部分文档给的是带填充的标准 base64，这里都接受。
func decodeEABKey(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if decoded, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(trimmed)
}

// persistAccountFailure 把账户注册失败的原因写回申请人，
// 便于前端直接展示「为什么这个申请人不能用」。
func persistAccountFailure(applicant *model.CertApplicant, cause error) {
	if database.DB == nil || applicant == nil {
		return
	}
	applicant.ACMEAccountState = model.ACMEAccountFailed
	applicant.ACMEAccountError = cause.Error()
	database.DB.Model(applicant).Updates(map[string]interface{}{
		"acme_account_state": model.ACMEAccountFailed,
		"acme_account_error": cause.Error(),
		"updated_at":         time.Now(),
	})
}
