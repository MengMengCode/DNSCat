package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

// DDNSKeyReq 是创建与更新 DDNS 密钥的请求体。
// 指针字段用于区分「未提供」与「显式设为 false/0」，避免更新时把开关静默重置。
type DDNSKeyReq struct {
	Name       string  `json:"name"`
	Hostnames  string  `json:"hostnames"`
	AllowedIPs *string `json:"allowed_ips"`
	RecordTTL  *uint32 `json:"record_ttl"`
	AllowIPv4  *bool   `json:"allow_ipv4"`
	AllowIPv6  *bool   `json:"allow_ipv6"`
	AutoCreate *bool   `json:"auto_create"`
	Enabled    *bool   `json:"enabled"`
}

// ddnsKeyOwnedDomain 取出域名并校验调用者有权操作它。
// 非管理员只能操作自己名下的域名，避免越权管理他人域名的 DDNS 凭据。
func ddnsKeyOwnedDomain(c *gin.Context, domainID uint) (*model.Domain, bool) {
	query := database.DB.Where("id = ?", domainID)
	if c.GetString("role") != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", c.GetUint("user_id"))
	}

	var domain model.Domain
	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return nil, false
	}
	return &domain, true
}

// ddnsKeyWithDomain 按密钥 ID 取出密钥及其所属域名，并做同样的归属校验。
func ddnsKeyWithDomain(c *gin.Context) (*model.DDNSKey, *model.Domain, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid DDNS key id"})
		return nil, nil, false
	}

	var key model.DDNSKey
	if err := database.DB.First(&key, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "DDNS key not found"})
		return nil, nil, false
	}

	domain, ok := ddnsKeyOwnedDomain(c, key.DomainID)
	if !ok {
		return nil, nil, false
	}
	return &key, domain, true
}

// ListDDNSKeys 返回某域名下的全部 DDNS 密钥。
// 响应里不含密钥明文，也不含哈希（KeyHash 的 json 标签是 "-"）。
func (h *DDNSHandler) ListDDNSKeys(c *gin.Context) {
	domainID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain id"})
		return
	}
	if _, ok := ddnsKeyOwnedDomain(c, uint(domainID)); !ok {
		return
	}

	var keys []model.DDNSKey
	if err := database.DB.Where("domain_id = ?", domainID).
		Order("id desc").Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch DDNS keys"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ddns_keys": keys, "total": len(keys)})
}

// CreateDDNSKey 新建一把 DDNS 密钥，并在响应中一次性返回明文。
// 明文不入库，此后任何接口都无法再取回，只能轮换。
func (h *DDNSHandler) CreateDDNSKey(c *gin.Context) {
	domainID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain id"})
		return
	}
	domain, ok := ddnsKeyOwnedDomain(c, uint(domainID))
	if !ok {
		return
	}

	var req DDNSKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid DDNS key parameters"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DDNS key name is required"})
		return
	}

	hostnames := NormalizeDDNSHostnames(req.Hostnames, domain.Name)
	if hostnames == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least one hostname is required (use @ for the apex, or * for any host under this zone)",
		})
		return
	}

	allowedIPs := ""
	if req.AllowedIPs != nil {
		normalized, err := ValidateDDNSAllowedIPs(*req.AllowedIPs)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		allowedIPs = normalized
	}

	ttl := uint32(60)
	if req.RecordTTL != nil && *req.RecordTTL > 0 {
		ttl = *req.RecordTTL
	}

	plain, prefix, hash, err := GenerateDDNSKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate DDNS key"})
		return
	}

	key := model.DDNSKey{
		DomainID:   domain.ID,
		UserID:     domain.UserID,
		Name:       name,
		KeyPrefix:  prefix,
		KeyHash:    hash,
		Hostnames:  hostnames,
		AllowedIPs: allowedIPs,
		RecordTTL:  ttl,
		AllowIPv4:  boolOrDefault(req.AllowIPv4, true),
		AllowIPv6:  boolOrDefault(req.AllowIPv6, true),
		AutoCreate: boolOrDefault(req.AutoCreate, true),
		Enabled:    boolOrDefault(req.Enabled, true),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := database.DB.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create DDNS key"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"ddns_key": key,
		// 明文只在此处出现一次，前端必须提示用户立即保存。
		"key":     plain,
		"warning": "This key is shown only once and cannot be retrieved later. Store it now.",
	})
}

// UpdateDDNSKey 修改密钥的作用域与开关。密钥本身不可通过此接口更改，需走轮换。
func (h *DDNSHandler) UpdateDDNSKey(c *gin.Context) {
	key, domain, ok := ddnsKeyWithDomain(c)
	if !ok {
		return
	}

	var req DDNSKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid DDNS key parameters"})
		return
	}

	updates := map[string]interface{}{"updated_at": time.Now()}

	if name := strings.TrimSpace(req.Name); name != "" {
		updates["name"] = name
	}
	if strings.TrimSpace(req.Hostnames) != "" {
		hostnames := NormalizeDDNSHostnames(req.Hostnames, domain.Name)
		if hostnames == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid hostname list"})
			return
		}
		updates["hostnames"] = hostnames
	}
	if req.AllowedIPs != nil {
		normalized, err := ValidateDDNSAllowedIPs(*req.AllowedIPs)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updates["allowed_ips"] = normalized
	}
	if req.RecordTTL != nil && *req.RecordTTL > 0 {
		updates["record_ttl"] = *req.RecordTTL
	}
	if req.AllowIPv4 != nil {
		updates["allow_ipv4"] = *req.AllowIPv4
	}
	if req.AllowIPv6 != nil {
		updates["allow_ipv6"] = *req.AllowIPv6
	}
	if req.AutoCreate != nil {
		updates["auto_create"] = *req.AutoCreate
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := database.DB.Model(&model.DDNSKey{}).Where("id = ?", key.ID).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update DDNS key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "DDNS key updated"})
}

// RotateDDNSKey 换发密钥：旧密钥立即失效，新明文一次性返回。
// 密钥疑似泄露时用这个，而不是删掉重建（保留统计与作用域配置）。
func (h *DDNSHandler) RotateDDNSKey(c *gin.Context) {
	key, _, ok := ddnsKeyWithDomain(c)
	if !ok {
		return
	}

	plain, prefix, hash, err := GenerateDDNSKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate DDNS key"})
		return
	}

	err = database.DB.Model(&model.DDNSKey{}).Where("id = ?", key.ID).Updates(map[string]interface{}{
		"key_prefix": prefix,
		"key_hash":   hash,
		// 轮换意味着换了一把新钥匙，旧的使用留痕不再具有参考意义。
		"last_status":   "",
		"reject_count":  0,
		"updated_at":    time.Now(),
	}).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rotate DDNS key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"key":     plain,
		"warning": "The previous key is now invalid. This new key is shown only once.",
	})
}

// DeleteDDNSKey 删除密钥。已由该密钥创建的 DNS 记录保持原样，
// 只是不再会被自动更新——删凭据不该顺手改动线上解析。
func (h *DDNSHandler) DeleteDDNSKey(c *gin.Context) {
	key, _, ok := ddnsKeyWithDomain(c)
	if !ok {
		return
	}

	if err := database.DB.Delete(&model.DDNSKey{}, key.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete DDNS key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "DDNS key deleted"})
}

func boolOrDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
