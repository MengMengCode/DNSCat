package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	xacme "golang.org/x/crypto/acme"
)

// issueTimeout 覆盖一次完整签发（含 DNS 传播等待与 CA 校验轮询）的上限。
const issueTimeout = 10 * time.Minute

// Issuer 持有 ACME 签发的全局配置。
type Issuer struct {
	// email 兜底联络邮箱：申请人未填邮箱时使用。
	email string
	// caURLOverride 来自系统设置 acme_ca_url。非空时覆盖所有服务商的
	// Directory 地址，用于对接私有 CA 或本地 Pebble 测试服务。
	caURLOverride string
	// staging 为 true 时优先使用各 CA 的测试环境，避免消耗生产配额。
	staging bool
}

// email 不设出厂默认值：该地址会真实提交给 ACME 服务商用于证书到期通知，
// 填一个不属于使用者的邮箱等于让通知永远收不到。由系统设置 acme_email 提供。
var GlobalIssuer = &Issuer{}

// InitIssuer 配置全局签发器。caURL 留空表示按服务商自动选择 Directory。
func InitIssuer(email, caURL string, staging bool) *Issuer {
	if email != "" {
		GlobalIssuer.email = email
	}
	GlobalIssuer.caURLOverride = strings.TrimSpace(caURL)
	GlobalIssuer.staging = staging

	mode := "生产环境"
	if staging {
		mode = "测试环境 (staging)"
	}
	override := GlobalIssuer.caURLOverride
	if override == "" {
		override = "按服务商自动选择"
	}
	log.Printf("[ACME] 签发器已初始化：联络邮箱=%s，模式=%s，Directory=%s",
		GlobalIssuer.email, mode, override)
	return GlobalIssuer
}

// RegisterApplicantAccount 为申请人在其服务商注册（或复用）ACME 账户，
// 并把结果持久化到申请人记录。用于「创建 / 变更申请人后自动完成账户配置」，
// 使控制台无需等到首次签发即可看到账户是否可用。
//
// 会阻塞至一次注册尝试完成（含到 CA 的网络往返），调用方通常放在后台协程中执行。
// acmeClientFor 内部语义为「可复用账户则复用，否则注册」，并在失败时把原因写回申请人，
// 因此这里无需再单独处理失败落库。
func (issuer *Issuer) RegisterApplicantAccount(ctx context.Context, applicant *model.CertApplicant) error {
	if applicant == nil {
		return fmt.Errorf("未指定申请人")
	}
	provider := LookupProvider(applicant.Provider)
	directoryURL := ResolveDirectoryURL(provider, issuer.caURLOverride, issuer.staging)
	_, err := acmeClientFor(ctx, applicant, directoryURL, issuer.email)
	return err
}

// PrepareCertificate 校验参数并落一条 issuing 状态的证书记录，随即返回。
//
// 拆出这一步是为了让 HTTP 接口能立刻响应：真实签发要等 DNS 传播与 CA 校验，
// 耗时以分钟计，阻塞在请求里必然撞上浏览器或反向代理的超时。
// 前端拿到 issuing 记录即可先渲染出「签发中」，再靠刷新查看最终结果。
func (issuer *Issuer) PrepareCertificate(
	domain *model.Domain,
	domainList []string,
	keyType string,
	applicant *model.CertApplicant,
) (*model.Certificate, []string, error) {
	if domain == nil {
		return nil, nil, fmt.Errorf("未指定托管域名")
	}
	normalized := normalizeDomainList(domain, domainList)
	if len(normalized) == 0 {
		return nil, nil, fmt.Errorf("待签发域名列表为空")
	}

	// 所有待签发域名都必须归属该托管区域，否则我们无法写入其挑战记录。
	if err := validateDomainsInZone(domain, normalized); err != nil {
		return nil, nil, err
	}

	provider := LookupProvider(providerNameOf(applicant))
	// EAB 前置校验放在这里，使非法配置在提交时就被拒绝，
	// 而不是等后台任务跑起来才失败。
	if provider.RequiresEAB && applicant != nil &&
		(strings.TrimSpace(applicant.EABKID) == "" || strings.TrimSpace(applicant.EABHMACKey) == "") {
		return nil, nil, fmt.Errorf(
			"%s 要求 External Account Binding，请先为申请人「%s」填写 EAB Key ID 与 HMAC Key",
			provider.Name, applicant.Name)
	}

	normalizedKeyType := normalizeKeyType(keyType)
	cert := &model.Certificate{
		DomainID:  domain.ID,
		Name:      fmt.Sprintf("%s (%s / %s)", normalized[0], normalizedKeyType, provider.Name),
		Domains:   strings.Join(normalized, ","),
		KeyType:   normalizedKeyType,
		Issuer:    provider.Name,
		AutoRenew: true,
		Status:    model.CertStatusIssuing,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if applicant != nil {
		cert.ApplicantID = &applicant.ID
	}
	if err := database.DB.Create(cert).Error; err != nil {
		return nil, nil, fmt.Errorf("创建证书记录失败: %w", err)
	}

	return cert, normalized, nil
}

// CompleteIssuance 执行真正的 ACME 签发并把结果写回既有证书记录。
//
// 完整流程遵循 RFC 8555：
//  1. 注册/复用申请人对应的 ACME 账户（ZeroSSL、Google 走 EAB）
//  2. 下单（newOrder），拿到每个域名的授权
//  3. 为每个待校验授权写入 _acme-challenge TXT 记录
//  4. 等待记录在自有权威 NS 上全网生效
//  5. 通知 CA 校验，等待授权变为 valid
//  6. 生成证书密钥与 CSR，finalize 订单并下载证书链
//  7. 落库并清理临时 TXT 记录
//
// 任一环节失败都会清理已写入的挑战记录，并把原因写入证书的 last_error。
func (issuer *Issuer) CompleteIssuance(
	cert *model.Certificate,
	domain *model.Domain,
	domainList []string,
	applicant *model.CertApplicant,
) error {
	provider := LookupProvider(providerNameOf(applicant))
	directoryURL := ResolveDirectoryURL(provider, issuer.caURLOverride, issuer.staging)

	log.Printf("[ACME] 开始签发证书 #%d: domains=%s, 算法=%s, 服务商=%s, Directory=%s",
		cert.ID, cert.Domains, cert.KeyType, provider.Name, directoryURL)

	certPEM, keyPEM, notBefore, notAfter, err := issuer.runOrder(
		domain, domainList, cert.KeyType, applicant, directoryURL)
	if err != nil {
		markCertFailed(cert, err)
		return err
	}

	now := time.Now()
	if err := database.DB.Model(cert).Updates(map[string]interface{}{
		"cert_pem":   certPEM,
		"key_pem":    keyPEM,
		"valid_from": notBefore,
		"valid_to":   notAfter,
		"status":     model.CertStatusValid,
		"last_error": "",
		"updated_at": now,
	}).Error; err != nil {
		return fmt.Errorf("保存签发结果失败: %w", err)
	}

	log.Printf("[ACME] 证书 #%d 签发成功: %s (有效期至 %s)",
		cert.ID, cert.Domains, notAfter.Format(time.RFC3339))
	return nil
}

// IssueCertificate 同步完成「建记录 + 签发」全过程。
// 供自动续期 worker 使用——它本身已在后台协程中运行，无需再异步化。
func (issuer *Issuer) IssueCertificate(
	domain *model.Domain,
	domainList []string,
	keyType string,
	applicant *model.CertApplicant,
) (*model.Certificate, error) {
	cert, normalized, err := issuer.PrepareCertificate(domain, domainList, keyType, applicant)
	if err != nil {
		return nil, err
	}
	if err := issuer.CompleteIssuance(cert, domain, normalized, applicant); err != nil {
		return nil, err
	}
	database.DB.Preload("Applicant").First(cert, cert.ID)
	return cert, nil
}

// validateDomainsInZone 确认每个待签发域名都在该托管区域之内。
//
// 这是必需的前置校验：DNS-01 要求我们能在目标域名下写入 TXT 记录，
// 若域名不属于本区域（例如给 example.com 的区域申请 other.com 的证书），
// 挑战记录无处可写，只会在后台白跑一轮后失败。
func validateDomainsInZone(domain *model.Domain, domainList []string) error {
	zone := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain.Name), "."))
	for _, d := range domainList {
		base := strings.TrimPrefix(d, "*.")
		if base != zone && !strings.HasSuffix(base, "."+zone) {
			return fmt.Errorf("域名 %s 不属于托管区域 %s，无法通过 DNS-01 校验", d, zone)
		}
	}
	return nil
}

// runOrder 执行一次完整的 ACME 订单，返回证书链 PEM、私钥 PEM 与有效期。
func (issuer *Issuer) runOrder(
	domain *model.Domain,
	domainList []string,
	keyType string,
	applicant *model.CertApplicant,
	directoryURL string,
) (certPEM string, keyPEM string, notBefore time.Time, notAfter time.Time, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), issueTimeout)
	defer cancel()

	// 申请人缺失时构造一个内存态的临时申请人，使无 Profile 也能签发。
	// 它不落库，因此账户密钥不会持久化——仅作为兜底路径。
	effectiveApplicant := applicant
	if effectiveApplicant == nil {
		effectiveApplicant = &model.CertApplicant{
			Email:    issuer.email,
			Provider: DefaultProviderName,
		}
	}

	client, err := acmeClientFor(ctx, effectiveApplicant, directoryURL, issuer.email)
	if err != nil {
		return "", "", notBefore, notAfter, err
	}

	authzIDs := make([]xacme.AuthzID, 0, len(domainList))
	for _, d := range domainList {
		authzIDs = append(authzIDs, xacme.AuthzID{Type: "dns", Value: d})
	}

	order, err := client.AuthorizeOrder(ctx, authzIDs)
	if err != nil {
		return "", "", notBefore, notAfter, fmt.Errorf("向 CA 下单失败: %w", err)
	}

	solver := newDNS01Solver(domain)
	// 无论成功失败都必须清理挑战记录，否则 _acme-challenge 会残留在用户区域里。
	defer solver.cleanUp()

	if err := issuer.solveAuthorizations(ctx, client, order, solver); err != nil {
		return "", "", notBefore, notAfter, err
	}

	// 全部授权通过后，订单应进入 ready 状态方可 finalize。
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return "", "", notBefore, notAfter, fmt.Errorf("等待订单就绪失败: %w", err)
	}

	certKey, keyPEM, err := generateCertificateKey(keyType)
	if err != nil {
		return "", "", notBefore, notAfter, err
	}

	csrDER, err := buildCSR(certKey, domainList)
	if err != nil {
		return "", "", notBefore, notAfter, err
	}

	// bundle=true 让 CA 返回完整证书链（叶证书 + 中间证书）。
	chainDER, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return "", "", notBefore, notAfter, fmt.Errorf("finalize 订单并获取证书失败: %w", err)
	}
	if len(chainDER) == 0 {
		return "", "", notBefore, notAfter, fmt.Errorf("CA 返回了空证书链")
	}

	leaf, err := x509.ParseCertificate(chainDER[0])
	if err != nil {
		return "", "", notBefore, notAfter, fmt.Errorf("解析签发的证书失败: %w", err)
	}

	var chainPEM strings.Builder
	for _, der := range chainDER {
		if err := pem.Encode(&chainPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return "", "", notBefore, notAfter, fmt.Errorf("编码证书链失败: %w", err)
		}
	}

	return chainPEM.String(), keyPEM, leaf.NotBefore, leaf.NotAfter, nil
}

// solveAuthorizations 处理订单中所有待校验的授权。
//
// 采用「先全部写入 TXT → 统一等待传播 → 再一次性提交校验」的顺序，
// 而不是逐个域名串行处理。原因有两点：
//   - 泛域名与 apex 共用同一挑战名，必须两条记录同时存在才能都通过；
//   - 传播等待是整个流程最慢的一步，合并等待可显著缩短总耗时。
func (issuer *Issuer) solveAuthorizations(
	ctx context.Context,
	client *xacme.Client,
	order *xacme.Order,
	solver *dns01Solver,
) error {
	type acceptTarget struct {
		authzURL  string
		challenge *xacme.Challenge
	}
	var targets []acceptTarget

	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return fmt.Errorf("获取域名授权失败: %w", err)
		}

		// 已经通过的授权可直接跳过（CA 会在一段时间内复用有效授权）。
		if authz.Status == xacme.StatusValid {
			continue
		}
		if authz.Status != xacme.StatusPending {
			return fmt.Errorf("域名 %s 的授权状态异常: %s", authz.Identifier.Value, authz.Status)
		}

		challenge := findDNS01Challenge(authz)
		if challenge == nil {
			return fmt.Errorf("CA 未为域名 %s 提供 dns-01 挑战方式", authz.Identifier.Value)
		}

		value, err := client.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return fmt.Errorf("计算 DNS-01 挑战值失败: %w", err)
		}

		if err := solver.present(challengeFQDNFor(authz.Identifier.Value), value); err != nil {
			return err
		}

		targets = append(targets, acceptTarget{authzURL: authzURL, challenge: challenge})
	}

	// 所有授权都已有效时无需再校验。
	if len(targets) == 0 {
		return nil
	}

	if err := solver.waitForPropagation(); err != nil {
		return err
	}

	for _, t := range targets {
		if _, err := client.Accept(ctx, t.challenge); err != nil {
			return fmt.Errorf("提交 DNS-01 校验失败: %w", err)
		}
	}

	for _, t := range targets {
		authz, err := client.WaitAuthorization(ctx, t.authzURL)
		if err != nil {
			return fmt.Errorf("CA 校验未通过: %w", err)
		}
		if authz.Status != xacme.StatusValid {
			return fmt.Errorf("域名 %s 校验失败，最终状态: %s", authz.Identifier.Value, authz.Status)
		}
	}

	return nil
}

func findDNS01Challenge(authz *xacme.Authorization) *xacme.Challenge {
	for _, c := range authz.Challenges {
		if c.Type == "dns-01" {
			return c
		}
	}
	return nil
}

// normalizeDomainList 清洗待签发域名列表：去空白、转小写、去重、去尾点。
// 列表为空时默认签发 apex + 泛域名。
func normalizeDomainList(domain *model.Domain, domainList []string) []string {
	if len(domainList) == 0 {
		domainList = []string{domain.Name, "*." + domain.Name}
	}
	seen := make(map[string]struct{}, len(domainList))
	out := make([]string, 0, len(domainList))
	for _, d := range domainList {
		cleaned := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if cleaned == "" {
			continue
		}
		if _, dup := seen[cleaned]; dup {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

// splitDomains 把数据库中逗号分隔的域名串还原为列表。
func splitDomains(joined string) []string {
	parts := strings.Split(joined, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if cleaned := strings.TrimSpace(p); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func providerNameOf(applicant *model.CertApplicant) string {
	if applicant != nil && strings.TrimSpace(applicant.Provider) != "" {
		return applicant.Provider
	}
	return DefaultProviderName
}

// normalizeKeyType 把请求的算法名归一化为受支持的取值，未知值回退 ECDSAP256。
func normalizeKeyType(keyType string) string {
	normalized := strings.ToUpper(strings.TrimSpace(keyType))
	switch normalized {
	case "RSA2048", "RSA3072", "RSA4096",
		"ECDSAP256", "ECDSAP384", "ECDSAP521", "ED25519":
		return normalized
	default:
		return "ECDSAP256"
	}
}

// generateCertificateKey 按算法生成证书私钥，返回签名器与 PEM 编码。
//
// 注意 Ed25519 目前仅有部分 CA 支持（Let's Encrypt 尚未支持），
// 选用后若被 CA 拒绝，错误会通过 finalize 阶段如实反馈。
func generateCertificateKey(keyType string) (crypto.Signer, string, error) {
	switch keyType {
	case "RSA2048", "RSA3072", "RSA4096":
		bits := map[string]int{"RSA2048": 2048, "RSA3072": 3072, "RSA4096": 4096}[keyType]
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, "", fmt.Errorf("生成 %s 密钥失败: %w", keyType, err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		return key, string(keyPEM), nil

	case "ECDSAP384", "ECDSAP521", "ECDSAP256":
		curve := map[string]elliptic.Curve{
			"ECDSAP256": elliptic.P256(),
			"ECDSAP384": elliptic.P384(),
			"ECDSAP521": elliptic.P521(),
		}[keyType]
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return nil, "", fmt.Errorf("生成 %s 密钥失败: %w", keyType, err)
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, "", fmt.Errorf("序列化 %s 密钥失败: %w", keyType, err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		return key, string(keyPEM), nil

	case "ED25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, "", fmt.Errorf("生成 Ed25519 密钥失败: %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, "", fmt.Errorf("序列化 Ed25519 密钥失败: %w", err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		return key, string(keyPEM), nil

	default:
		return nil, "", fmt.Errorf("不支持的密钥算法: %s", keyType)
	}
}

// buildCSR 构造证书签名请求。
//
// CommonName 只填第一个域名，其余全部进 SAN：现代 CA 与浏览器均以 SAN 为准，
// 且 CN 长度上限 64 字节，长域名会被拒。
func buildCSR(key crypto.Signer, domainList []string) ([]byte, error) {
	tmpl := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domainList[0]},
		DNSNames: domainList,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, fmt.Errorf("构造 CSR 失败: %w", err)
	}
	return csrDER, nil
}

// markCertFailed 把签发失败的原因写回证书记录，供前端展示。
func markCertFailed(cert *model.Certificate, cause error) {
	if cert == nil || database.DB == nil {
		return
	}
	log.Printf("[ACME] 证书签发失败 (domains=%s): %v", cert.Domains, cause)
	database.DB.Model(cert).Updates(map[string]interface{}{
		"status":     model.CertStatusFailed,
		"last_error": cause.Error(),
		"updated_at": time.Now(),
	})
}
