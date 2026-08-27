package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
	"dnscat/internal/seclogstore"
)

type SecurityHandler struct{}

func NewSecurityHandler() *SecurityHandler {
	return &SecurityHandler{}
}

// loadDomainScoped 按 URL 中的 :id 取域名，并做归属校验（非管理员只能操作自己的域名）。
func (h *SecurityHandler) loadDomainScoped(c *gin.Context) (*model.Domain, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain id"})
		return nil, false
	}

	// 归属校验沿用与域名接口一致的取值方式，保证权限行为在各接口间统一。
	userID := c.GetUint("user_id")
	role := c.GetString("role")
	query := database.DB.Where("id = ?", id)
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}

	var domain model.Domain
	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return nil, false
	}
	return &domain, true
}

// getOrCreatePolicy 取该域名的安全策略，不存在时按默认值创建，
// 使老域名首次打开安全防护页即拥有防护基线。
func getOrCreatePolicy(domainID uint) (model.DomainSecurityPolicy, error) {
	var policy model.DomainSecurityPolicy
	err := database.DB.Where("domain_id = ?", domainID).First(&policy).Error
	if err == nil {
		return policy, nil
	}
	policy = model.DefaultSecurityPolicy(domainID)
	if createErr := database.DB.Create(&policy).Error; createErr != nil {
		return policy, createErr
	}
	return policy, nil
}

// GetSecurity 返回某域名的安全策略、累计攻击统计、DNSSEC 状态与当前封禁数。
func (h *SecurityHandler) GetSecurity(c *gin.Context) {
	domain, ok := h.loadDomainScoped(c)
	if !ok {
		return
	}

	policy, err := getOrCreatePolicy(domain.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load security policy"})
		return
	}

	stat := dnsengine.GlobalSecurity.Stat(domain.ID)
	c.JSON(http.StatusOK, gin.H{
		"domain":         gin.H{"id": domain.ID, "name": domain.Name},
		"policy":         policy,
		"stats":          stat,
		"dnssec_enabled": domain.DNSSECEnabled,
		"active_bans":    dnsengine.GlobalSecurity.ActiveBans(domain.Name),
	})
}

// UpdateSecurityReq 是安全策略的更新载荷。全部为指针，只更新显式传入的字段，
// 避免前端漏传某个开关时把它意外置为 false。
type UpdateSecurityReq struct {
	Enabled *bool `json:"enabled"`

	RateLimitEnabled *bool `json:"rate_limit_enabled"`
	RateLimitQPS     *int  `json:"rate_limit_qps"`
	RateLimitBurst   *int  `json:"rate_limit_burst"`

	FloodProtectionEnabled *bool `json:"flood_protection_enabled"`
	FloodThresholdQPS      *int  `json:"flood_threshold_qps"`
	FloodBanSeconds        *int  `json:"flood_ban_seconds"`

	RRLEnabled         *bool `json:"rrl_enabled"`
	RRLResponsesPerSec *int  `json:"rrl_responses_per_sec"`
	RRLSlipRatio       *int  `json:"rrl_slip_ratio"`

	BlacklistEnabled *bool   `json:"blacklist_enabled"`
	BlockedIPs       *string `json:"blocked_ips"`
	BlockedASNs      *string `json:"blocked_asns"`
	BlockedCountries *string `json:"blocked_countries"`
	AllowedIPs       *string `json:"allowed_ips"`

	AXFRPolicy     *string `json:"axfr_policy"`
	AXFRAllowedIPs *string `json:"axfr_allowed_ips"`

	QTypeFilterEnabled *bool   `json:"qtype_filter_enabled"`
	BlockedQTypes      *string `json:"blocked_qtypes"`

	AnyPolicy *string `json:"any_policy"`

	ZoneQPSLimitEnabled *bool `json:"zone_qps_limit_enabled"`
	ZoneQPSLimit        *int  `json:"zone_qps_limit"`

	ACLEnabled    *bool   `json:"acl_enabled"`
	ACLAllowedIPs *string `json:"acl_allowed_ips"`

	NXProtectionEnabled *bool `json:"nx_protection_enabled"`
	NXThresholdPerMin   *int  `json:"nx_threshold_per_min"`
	NXBanSeconds        *int  `json:"nx_ban_seconds"`

	QueryLogEnabled  *bool `json:"query_log_enabled"`
	AttackLogEnabled *bool `json:"attack_log_enabled"`

	LogRetentionLimit *int `json:"log_retention_limit"`
}

// clampInt 把阈值收敛到合理区间，避免填 0 或负数导致规则失效或除零。
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// UpdateSecurity 更新安全策略，并立刻重载区域 + 广播到边缘节点使其生效。
func (h *SecurityHandler) UpdateSecurity(c *gin.Context) {
	domain, ok := h.loadDomainScoped(c)
	if !ok {
		return
	}

	var req UpdateSecurityReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid security policy payload"})
		return
	}

	policy, err := getOrCreatePolicy(domain.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load security policy"})
		return
	}

	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}

	if req.RateLimitEnabled != nil {
		policy.RateLimitEnabled = *req.RateLimitEnabled
	}
	if req.RateLimitQPS != nil {
		policy.RateLimitQPS = clampInt(*req.RateLimitQPS, 1, 1000000)
	}
	if req.RateLimitBurst != nil {
		policy.RateLimitBurst = clampInt(*req.RateLimitBurst, 1, 2000000)
	}

	if req.FloodProtectionEnabled != nil {
		policy.FloodProtectionEnabled = *req.FloodProtectionEnabled
	}
	if req.FloodThresholdQPS != nil {
		policy.FloodThresholdQPS = clampInt(*req.FloodThresholdQPS, 1, 1000000)
	}
	if req.FloodBanSeconds != nil {
		policy.FloodBanSeconds = clampInt(*req.FloodBanSeconds, 1, 86400)
	}

	if req.RRLEnabled != nil {
		policy.RRLEnabled = *req.RRLEnabled
	}
	if req.RRLResponsesPerSec != nil {
		policy.RRLResponsesPerSec = clampInt(*req.RRLResponsesPerSec, 1, 1000000)
	}
	if req.RRLSlipRatio != nil {
		policy.RRLSlipRatio = clampInt(*req.RRLSlipRatio, 0, 100)
	}

	if req.BlacklistEnabled != nil {
		policy.BlacklistEnabled = *req.BlacklistEnabled
	}
	if req.BlockedIPs != nil {
		policy.BlockedIPs = strings.TrimSpace(*req.BlockedIPs)
	}
	if req.BlockedASNs != nil {
		policy.BlockedASNs = strings.TrimSpace(*req.BlockedASNs)
	}
	if req.BlockedCountries != nil {
		policy.BlockedCountries = strings.TrimSpace(*req.BlockedCountries)
	}
	if req.AllowedIPs != nil {
		policy.AllowedIPs = strings.TrimSpace(*req.AllowedIPs)
	}

	if req.AXFRPolicy != nil {
		switch *req.AXFRPolicy {
		case model.AXFRPolicyDeny, model.AXFRPolicyAllowlist:
			policy.AXFRPolicy = *req.AXFRPolicy
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "axfr_policy must be deny or allowlist"})
			return
		}
	}
	if req.AXFRAllowedIPs != nil {
		policy.AXFRAllowedIPs = strings.TrimSpace(*req.AXFRAllowedIPs)
	}

	if req.QTypeFilterEnabled != nil {
		policy.QTypeFilterEnabled = *req.QTypeFilterEnabled
	}
	if req.BlockedQTypes != nil {
		policy.BlockedQTypes = strings.ToUpper(strings.TrimSpace(*req.BlockedQTypes))
	}

	if req.AnyPolicy != nil {
		switch *req.AnyPolicy {
		case model.AnyPolicyAllow, model.AnyPolicyMinimal, model.AnyPolicyRefuse:
			policy.AnyPolicy = *req.AnyPolicy
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "any_policy must be allow, minimal or refuse"})
			return
		}
	}

	if req.ZoneQPSLimitEnabled != nil {
		policy.ZoneQPSLimitEnabled = *req.ZoneQPSLimitEnabled
	}
	if req.ZoneQPSLimit != nil {
		policy.ZoneQPSLimit = clampInt(*req.ZoneQPSLimit, 1, 10000000)
	}

	if req.ACLEnabled != nil {
		policy.ACLEnabled = *req.ACLEnabled
	}
	if req.ACLAllowedIPs != nil {
		policy.ACLAllowedIPs = strings.TrimSpace(*req.ACLAllowedIPs)
	}

	if req.NXProtectionEnabled != nil {
		policy.NXProtectionEnabled = *req.NXProtectionEnabled
	}
	if req.NXThresholdPerMin != nil {
		policy.NXThresholdPerMin = clampInt(*req.NXThresholdPerMin, 1, 1000000)
	}
	if req.NXBanSeconds != nil {
		policy.NXBanSeconds = clampInt(*req.NXBanSeconds, 1, 86400)
	}

	if req.QueryLogEnabled != nil {
		policy.QueryLogEnabled = *req.QueryLogEnabled
	}
	if req.AttackLogEnabled != nil {
		policy.AttackLogEnabled = *req.AttackLogEnabled
	}
	if req.LogRetentionLimit != nil {
		policy.LogRetentionLimit = clampInt(
			*req.LogRetentionLimit, model.MinLogRetentionLimit, model.MaxLogRetentionLimit)
	}
	// 历史数据可能没有该字段（老策略行），补上默认值避免写入 0 导致按兜底值处理。
	if policy.LogRetentionLimit <= 0 {
		policy.LogRetentionLimit = model.DefaultLogRetentionLimit
	}

	// 开了 ACL 却没填任何地址会把本区域彻底锁死（连自己都查不了），直接拒绝这种配置。
	if policy.ACLEnabled && strings.TrimSpace(policy.ACLAllowedIPs) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Resolver ACL is enabled but the allow list is empty; this would block all queries",
		})
		return
	}
	if policy.AXFRPolicy == model.AXFRPolicyAllowlist && strings.TrimSpace(policy.AXFRAllowedIPs) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AXFR allowlist is selected but empty; add secondary server IPs or switch to deny",
		})
		return
	}

	if err := database.DB.Save(&policy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save security policy"})
		return
	}

	// 保留上限被调小时立刻裁剪已有日志，让新上限即时生效（内存与 Redis 同步收敛）。
	dnsengine.GlobalSecurity.TrimEvents(domain.ID, policy.LogRetentionLimit)
	seclogstore.Trim(domain.ID, policy.LogRetentionLimit)

	// 重新编译该区域的安全运行时，并广播给边缘节点。
	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Security policy updated", "policy": policy})
}

// GetSecurityEvents 分页返回安全日志。保留上限可达上万条，因此搜索、
// 规则筛选与分页全部在服务端完成，响应里只带当前页。
func (h *SecurityHandler) GetSecurityEvents(c *gin.Context) {
	domain, ok := h.loadDomainScoped(c)
	if !ok {
		return
	}

	page := 1
	if n, err := strconv.Atoi(c.Query("page")); err == nil {
		page = clampInt(n, 1, 1000000)
	}
	pageSize := 20
	if n, err := strconv.Atoi(c.Query("page_size")); err == nil {
		pageSize = clampInt(n, 1, 200)
	}

	outcome := c.Query("outcome")
	if outcome != "blocked" && outcome != "allowed" {
		outcome = ""
	}

	items, matched, stored := dnsengine.GlobalSecurity.QueryEvents(domain.ID, dnsengine.EventFilter{
		Rule:    c.Query("rule"),
		Outcome: outcome,
		Search:  c.Query("search"),
		Offset:  (page - 1) * pageSize,
		Limit:   pageSize,
	})

	policy, _ := getOrCreatePolicy(domain.ID)
	retention := policy.LogRetentionLimit
	if retention <= 0 {
		retention = model.DefaultLogRetentionLimit
	}

	c.JSON(http.StatusOK, gin.H{
		"events":          items,
		"total":           matched,
		"stored":          stored,
		"page":            page,
		"page_size":       pageSize,
		"retention_limit": retention,
		"rules":           dnsengine.GlobalSecurity.EventRules(domain.ID),
	})
}

// ResetSecurityStats 清零该域名的攻击统计与事件。
func (h *SecurityHandler) ResetSecurityStats(c *gin.Context) {
	domain, ok := h.loadDomainScoped(c)
	if !ok {
		return
	}
	dnsengine.GlobalSecurity.ResetStats(domain.ID)
	// 一并删除 Redis 中的持久化日志，否则重启后又会把已清空的日志恢复回来。
	seclogstore.Drop(domain.ID)
	// 同步清库，避免下次进程重启又把旧计数装载回来。
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.DomainSecurityStat{})
	c.JSON(http.StatusOK, gin.H{"message": "Security statistics reset"})
}

// ClearSecurityBans 解除该域名当前的全部临时封禁。
func (h *SecurityHandler) ClearSecurityBans(c *gin.Context) {
	domain, ok := h.loadDomainScoped(c)
	if !ok {
		return
	}
	n := dnsengine.GlobalSecurity.ClearBans(domain.Name)
	c.JSON(http.StatusOK, gin.H{"message": "Bans cleared", "cleared": n})
}
