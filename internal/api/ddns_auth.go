package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

// DDNS 密钥的明文形态：ddns_<prefix8>_<secret32>
// 前缀随机但可公开，用于在控制台辨认是哪一把钥匙；秘密段才是真正的凭据。
const (
	ddnsKeyPrefixLen = 8
	ddnsKeySecretLen = 32
	ddnsKeyPrefixTag = "ddns_"
)

// gin.Context 中存放已认证 DDNS 密钥的键。
const ddnsKeyContextKey = "ddns_key"

// GenerateDDNSKey 生成一把新的 DDNS 密钥，返回（明文, 展示前缀, SHA-256 摘要）。
// 明文只在此刻存在，调用方必须立即返回给用户，之后无法再取回。
func GenerateDDNSKey() (plain string, prefix string, hash string, err error) {
	prefixBytes := make([]byte, ddnsKeyPrefixLen/2)
	secretBytes := make([]byte, ddnsKeySecretLen/2)
	if _, err = rand.Read(prefixBytes); err != nil {
		return "", "", "", err
	}
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", "", err
	}

	prefix = ddnsKeyPrefixTag + hex.EncodeToString(prefixBytes)
	plain = prefix + "_" + hex.EncodeToString(secretBytes)
	return plain, prefix, HashDDNSKey(plain), nil
}

// HashDDNSKey 计算密钥的存储摘要。密钥本身是高熵随机串，无需加盐或慢哈希：
// 它不像用户口令那样可被字典穷举，SHA-256 足以保证库泄露后无法还原明文。
func HashDDNSKey(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}

// DDNSKeyPrefixOf 从明文密钥中截出展示前缀，用于日志与错误提示。
// 不足以还原密钥，可以安全地写进日志。
func DDNSKeyPrefixOf(plain string) string {
	plain = strings.TrimSpace(plain)
	if idx := strings.LastIndex(plain, "_"); idx > 0 {
		return plain[:idx]
	}
	if len(plain) > ddnsKeyPrefixLen {
		return plain[:ddnsKeyPrefixLen]
	}
	return plain
}

// -----------------------------------------------------------------------------
// 失败认证限流
// -----------------------------------------------------------------------------

// ddnsFailureLimiter 按来源 IP 限制认证失败次数。
// DDNS 端点必须公开可达（客户端在任意网络里），又只靠一个静态密钥保护，
// 不限流就等于允许离线穷举。成功的更新不受影响，只惩罚失败。
type ddnsFailureLimiter struct {
	mu       sync.Mutex
	attempts map[string]*ddnsFailureRecord
	window   time.Duration
	limit    int
	lastGC   time.Time
}

type ddnsFailureRecord struct {
	count      int
	windowFrom time.Time
}

var ddnsLimiter = &ddnsFailureLimiter{
	attempts: make(map[string]*ddnsFailureRecord),
	window:   10 * time.Minute,
	limit:    20,
}

// blocked 判断该来源当前是否已被限流。
func (l *ddnsFailureLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.attempts[ip]
	if !ok {
		return false
	}
	if time.Since(rec.windowFrom) > l.window {
		delete(l.attempts, ip)
		return false
	}
	return rec.count >= l.limit
}

// recordFailure 记一次失败，并顺带清理过期条目避免 map 无界增长。
func (l *ddnsFailureLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, ok := l.attempts[ip]
	if !ok || now.Sub(rec.windowFrom) > l.window {
		l.attempts[ip] = &ddnsFailureRecord{count: 1, windowFrom: now}
	} else {
		rec.count++
	}

	if now.Sub(l.lastGC) > l.window {
		for key, item := range l.attempts {
			if now.Sub(item.windowFrom) > l.window {
				delete(l.attempts, key)
			}
		}
		l.lastGC = now
	}
}

// recordSuccess 清除该来源的失败计数，避免正常客户端偶发失败后被长期惩罚。
func (l *ddnsFailureLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// -----------------------------------------------------------------------------
// 来源 IP 白名单
// -----------------------------------------------------------------------------

// ddnsSourceAllowed 判断来源地址是否落在密钥配置的白名单内。
// 白名单为空表示不限制。条目可以是单个 IP 或 CIDR，IPv4 与 IPv6 都支持。
func ddnsSourceAllowed(allowedIPs string, clientIP string) bool {
	entries := splitDDNSList(allowedIPs)
	if len(entries) == 0 {
		return true
	}

	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		// 取不到可判定的来源地址时，有白名单就必须拒绝：
		// 这种情况下无法证明请求来自允许的网络。
		return false
	}

	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if network.Contains(ip) {
				return true
			}
			continue
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

// ValidateDDNSAllowedIPs 校验白名单配置的每一项都是合法 IP 或 CIDR，
// 并返回归一化后的字符串。在写入前拦下笔误，避免出现一条永远不匹配的规则。
func ValidateDDNSAllowedIPs(raw string) (string, error) {
	entries := splitDDNSList(raw)
	normalized := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				return "", &ddnsValidationError{msg: "无效的 CIDR 网段: " + entry}
			}
			normalized = append(normalized, network.String())
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return "", &ddnsValidationError{msg: "无效的 IP 地址: " + entry}
		}
		normalized = append(normalized, ip.String())
	}
	return strings.Join(normalized, ","), nil
}

type ddnsValidationError struct{ msg string }

func (e *ddnsValidationError) Error() string { return e.msg }

// splitDDNSList 把逗号/空白/换行分隔的配置串拆成去空条目。
func splitDDNSList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// 密钥提取与认证中间件
// -----------------------------------------------------------------------------

// extractDDNSKey 按常见 DDNS 客户端的习惯依次尝试取出密钥。
//
// dyndns2 标准用 HTTP Basic 认证，因此 Basic 是首选：ddclient、inadyn、
// OpenWrt、群晖等都走这条路。其余几种是为便于用 curl / 自定义脚本调试而支持。
func extractDDNSKey(c *gin.Context) string {
	// 1. HTTP Basic：密码位是密钥。部分客户端强制要求用户名非空，
	//    所以用户名不参与校验；若用户名位填了密钥（个别客户端把 token 放这里），也接受。
	if user, pass, ok := c.Request.BasicAuth(); ok {
		if strings.TrimSpace(pass) != "" {
			return strings.TrimSpace(pass)
		}
		if strings.HasPrefix(strings.TrimSpace(user), ddnsKeyPrefixTag) {
			return strings.TrimSpace(user)
		}
	}

	// 2. Authorization: Bearer <key>
	if authHeader := c.GetHeader("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		if key := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")); key != "" {
			return key
		}
	}

	// 3. 专用请求头
	if key := strings.TrimSpace(c.GetHeader("X-DDNS-Key")); key != "" {
		return key
	}

	// 4. 查询参数：No-IP / DuckDNS 风格客户端常用
	for _, param := range []string{"key", "token", "password", "pass"} {
		if key := strings.TrimSpace(c.Query(param)); key != "" {
			return key
		}
	}

	return ""
}

// ddnsAuthResult 描述一次认证的结果，便于调用方按 dyndns2 语义组织响应。
type ddnsAuthResult struct {
	Key      *model.DDNSKey
	Domain   *model.Domain
	ClientIP string
	// Status 为空表示认证通过；否则是应当回给客户端的 dyndns2 状态码。
	Status string
}

// authenticateDDNS 完成「取密钥 → 查库 → 校验启用状态 → 校验来源白名单」全过程。
// 任何一步失败都返回对应的 dyndns2 状态码，且不透露失败的具体原因
// （避免把「密钥有效但来源被拒」这类信息泄露给攻击者用于探测）。
func authenticateDDNS(c *gin.Context) ddnsAuthResult {
	clientIP := c.ClientIP()

	if ddnsLimiter.blocked(clientIP) {
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusAbuse}
	}

	plain := extractDDNSKey(c)
	if plain == "" {
		ddnsLimiter.recordFailure(clientIP)
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusBadAuth}
	}

	var key model.DDNSKey
	if err := database.DB.Where("key_hash = ?", HashDDNSKey(plain)).First(&key).Error; err != nil {
		ddnsLimiter.recordFailure(clientIP)
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusBadAuth}
	}

	if !key.Enabled {
		ddnsLimiter.recordFailure(clientIP)
		markDDNSRejected(&key, clientIP, c.Request.UserAgent(), ddnsStatusBadAuth)
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusBadAuth}
	}

	// 来源白名单是密钥泄露后的最后一道防线，必须在任何写操作之前判定。
	if !ddnsSourceAllowed(key.AllowedIPs, clientIP) {
		ddnsLimiter.recordFailure(clientIP)
		markDDNSRejected(&key, clientIP, c.Request.UserAgent(), ddnsStatusNotAllowedIP)
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusBadAuth}
	}

	var domain model.Domain
	if err := database.DB.First(&domain, key.DomainID).Error; err != nil {
		return ddnsAuthResult{ClientIP: clientIP, Status: ddnsStatusNoHost}
	}

	ddnsLimiter.recordSuccess(clientIP)
	c.Set(ddnsKeyContextKey, &key)

	return ddnsAuthResult{Key: &key, Domain: &domain, ClientIP: clientIP}
}

// markDDNSRejected 记录一次被拒绝的调用，使控制台能看出密钥是否正在被滥用。
func markDDNSRejected(key *model.DDNSKey, clientIP, userAgent, status string) {
	now := time.Now()
	_ = database.DB.Model(&model.DDNSKey{}).Where("id = ?", key.ID).Updates(map[string]interface{}{
		"reject_count":    key.RejectCount + 1,
		"last_client_ip":  clientIP,
		"last_user_agent": truncateDDNSString(userAgent, 255),
		"last_status":     status,
		"last_used_at":    &now,
		"updated_at":      now,
	}).Error
}

func truncateDDNSString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// writeDDNSResponse 以 dyndns2 约定的纯文本单行形式回应。
//
// 协议要求客户端解析响应体而非状态码，因此业务性失败（nohost/notfqdn 等）
// 一律用 200 承载；只有认证失败按 DynDNS 规范返回 401，让 ddclient
// 能明确区分「密钥不对」和「主机名不对」。
func writeDDNSResponse(c *gin.Context, status string) {
	httpCode := http.StatusOK
	switch status {
	case ddnsStatusBadAuth:
		httpCode = http.StatusUnauthorized
	case ddnsStatusServerError:
		httpCode = http.StatusInternalServerError
	}
	c.String(httpCode, status)
}
