package api

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/geo"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/miekg/dns"
)

type DomainHandler struct {
	cfg *config.Config
}

func NewDomainHandler(cfg *config.Config) *DomainHandler {
	return &DomainHandler{cfg: cfg}
}

type CreateDomainReq struct {
	Name       string `json:"name" binding:"required"`
	PrimaryNS  string `json:"primary_ns"`
	AdminEmail string `json:"admin_email"`
}

type UpdateDomainReq struct {
	Status     model.DomainStatus `json:"status"`
	PrimaryNS  string             `json:"primary_ns"`
	AdminEmail string             `json:"admin_email"`
	SOARefresh uint32             `json:"soa_refresh"`
	SOARetry   uint32             `json:"soa_retry"`
	SOAExpire  uint32             `json:"soa_expire"`
	SOAMinimum uint32             `json:"soa_minimum"`
}

func (h *DomainHandler) ListDomains(c *gin.Context) {
	userID := c.GetUint("user_id")
	role := c.GetString("role")
	search := strings.TrimSpace(c.Query("search"))

	query := database.DB.Model(&model.Domain{})
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}
	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}

	var domains []model.Domain
	if err := query.Order("id desc").Find(&domains).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domains"})
		return
	}

	// Attach record count
	for i := range domains {
		var count int64
		database.DB.Model(&model.Record{}).
			Where("domain_id = ?", domains[i].ID).
			Where("NOT (type = ? AND (name = ? OR name = ?))", model.RecordTypeNS, "@", "").
			Count(&count)
		domains[i].RecordCount = count

		// 附带 apex 上实际配置的全部权威 NS，供列表页完整展示。
		var nsValues []string
		database.DB.Model(&model.Record{}).
			Where("domain_id = ? AND type = ? AND (name = ? OR name = ?)",
				domains[i].ID, model.RecordTypeNS, "@", "").
			Order("value asc").
			Pluck("value", &nsValues)
		domains[i].NSRecords = nsValues
	}

	c.JSON(http.StatusOK, gin.H{"domains": domains, "total": len(domains)})
}

// purgeSoftDeletedDomain 物理清除一个已软删除域名及其所有子表残留。
// 仅在新建同名域名时调用：此时该域名对用户已不可见，保留其残留没有意义，
// 却会因唯一索引阻塞同名域名的重新添加。
// 返回 true 表示同名软删除残留已确实不存在（已清除或本来就没有）。
//
// 这里刻意使用原生 SQL 而非 GORM 的 Unscoped().Delete()：后者在实测中会
// 出现「影响 0 行但返回 nil error」的情况，导致清理误报成功，紧随其后的
// INSERT 撞上 name 唯一索引，只能抛出一个不透明的 500。改为原生 SQL 并校验
// RowsAffected，可以明确知道残留是否真的被删掉。
func purgeSoftDeletedDomain(domainID uint, name string) bool {
	// 子表按 domain_id 物理清除；失败只记日志，不阻断主流程。
	for _, stmt := range []struct {
		sql   string
		label string
	}{
		{"DELETE FROM records WHERE domain_id = ?", "记录"},
		// 表名按 GORM 命名策略为 dns_sec_keys（DNSSECKey 的缩写会被拆开），已核对实际库。
		{"DELETE FROM dns_sec_keys WHERE domain_id = ?", "DNSSEC"},
		{"DELETE FROM health_checks WHERE domain_id = ?", "健康检查"},
	} {
		if err := database.DB.Exec(stmt.sql, domainID).Error; err != nil {
			log.Printf("[Domain] 清除 %q 的%s残留失败: %v", name, stmt.label, err)
		}
	}

	// 按名字清除所有软删除残留：既覆盖同名多行的历史脏数据，
	// 也保证只动已删除的行（活跃同名域名在调用前已返回 409）。
	res := database.DB.Exec("DELETE FROM domains WHERE name = ? AND deleted_at IS NOT NULL", name)
	if res.Error != nil {
		log.Printf("[Domain] 清除软删除域名 %q (id=%d) 失败: %v", name, domainID, res.Error)
		return false
	}
	log.Printf("[Domain] 已清除同名软删除残留 %q (id=%d)，删除 %d 行", name, domainID, res.RowsAffected)

	// 复核：确认唯一索引上确实不再有同名行。
	var remaining int64
	if err := database.DB.Unscoped().Model(&model.Domain{}).
		Where("name = ?", name).Count(&remaining).Error; err != nil {
		log.Printf("[Domain] 复核 %q 残留数量失败: %v", name, err)
		return false
	}
	if remaining > 0 {
		log.Printf("[Domain] %q 仍存在 %d 行同名记录，无法重新添加", name, remaining)
		return false
	}
	return true
}

// nsFQDN 把主机名规整成 DNS 记录值需要的绝对域名（结尾带点）。
func nsFQDN(host string) string {
	h := strings.TrimSpace(host)
	if h == "" {
		return ""
	}
	if strings.HasSuffix(h, ".") {
		return h
	}
	return h + "."
}

// authoritativeNSHostnames 汇总本系统对外提供的全部权威 NS 主机名。
//
// 管理员可能在「权威 NS 服务器」页面配置多台（不止两台），因此这里不能只取固定的
// 前两个：新建域名时要把所有已启用的 NS 都写进区，用户才能在注册商处把它们全部配上。
// 顺序为「NS 服务器表（已启用）」优先，再补配置里的 default_ns，返回不带结尾点的小写
// 主机名并已去重。
func authoritativeNSHostnames(cfgDefaults []string) []string {
	seen := make(map[string]bool)
	var hosts []string

	add := func(raw string) {
		h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}

	var dbNS []model.Nameserver
	database.DB.Where("is_active = ?", true).Order("id asc").Find(&dbNS)
	for _, ns := range dbNS {
		add(ns.Hostname)
	}
	for _, def := range cfgDefaults {
		add(def)
	}
	return hosts
}

func (h *DomainHandler) CreateDomain(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req CreateDomainReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain name"})
		return
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	name = strings.TrimSuffix(name, ".")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, " ") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain format"})
		return
	}

	var existing model.Domain
	if err := database.DB.Where("name = ?", name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Domain already exists"})
		return
	}

	// domains.name 是唯一索引，而 Domain 走 gorm 软删除：被删除的行仍占用该索引。
	// 若不清理，用户删掉一个域名后就再也无法添加同名域名（INSERT 触发
	// Duplicate entry，只会回一个不透明的 500）。这里把同名的软删除残留连同其
	// 子表记录一并物理清除，让「删除后重新添加」成为可用操作。
	var stale model.Domain
	if err := database.DB.Unscoped().Where("name = ?", name).First(&stale).Error; err == nil {
		// 清理失败时直接给出可读原因，避免后续 INSERT 抛出不透明的 500。
		if !purgeSoftDeletedDomain(stale.ID, name) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Domain name is occupied by a deleted record that could not be purged; please retry or contact the administrator",
			})
			return
		}
	}

	// 取本系统全部权威 NS：管理员可能配置了多台，新建域名要把它们全部写入区。
	nsHosts := authoritativeNSHostnames(h.cfg.DNS.DefaultNS)

	primaryNS := req.PrimaryNS
	if primaryNS == "" {
		if len(nsHosts) > 0 {
			primaryNS = nsFQDN(nsHosts[0])
		} else {
			// 系统尚未配置任何权威 NS：用域名自身派生占位，不写死任何主机名。
			// 使用者在「权威 NS 服务器」页添加后，NS 记录会随区域重载更新。
			primaryNS = nsFQDN("ns1." + name)
		}
	}

	adminEmail := req.AdminEmail
	if adminEmail == "" {
		adminEmail = "admin." + name + "."
	}

	domain := model.Domain{
		UserID:     userID,
		Name:       name,
		Status:     model.DomainStatusActive,
		NSStatus:   "pending",
		PrimaryNS:  primaryNS,
		AdminEmail: adminEmail,
		SOARefresh: 10000,
		SOARetry:   2400,
		SOAExpire:  604800,
		SOAMinimum: 300,
		SOASerial:  uint32(time.Now().Unix()),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := database.DB.Create(&domain).Error; err != nil {
		// 带上底层错误：此前统一回 "Failed to create domain"，
		// 唯一索引冲突之类的问题在前端完全无法定位。
		log.Printf("[Domain] 创建域名 %q 失败: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to create domain: %v", err),
		})
		return
	}

	// 为每一台权威 NS 生成一条 apex NS 记录（数量随管理员配置变化，不再固定两条）。
	records := make([]model.Record, 0, len(nsHosts))
	for i, host := range nsHosts {
		comment := "Primary Authoritative NS"
		if i > 0 {
			comment = fmt.Sprintf("Authoritative NS #%d", i+1)
		}
		records = append(records, model.Record{
			DomainID: domain.ID,
			Name:     "@",
			Type:     model.RecordTypeNS,
			Value:    nsFQDN(host),
			TTL:      86400,
			Enabled:  true,
			Comment:  comment,
		})
	}
	database.DB.Create(&records)

	// 新建域名即写入默认安全策略，使其从第一次解析起就有防护基线，
	// 而不是等到管理员打开安全防护页面才生效。
	securityPolicy := model.DefaultSecurityPolicy(domain.ID)
	if err := database.DB.Create(&securityPolicy).Error; err != nil {
		log.Printf("[security] 为新域名 %s 创建默认安全策略失败: %v", domain.Name, err)
	}

	// Load into in-memory engine
	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusCreated, domain)
}

func (h *DomainHandler) GetDomain(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userID := c.GetUint("user_id")
	role := c.GetString("role")

	var domain model.Domain
	query := database.DB.Preload("Records").Preload("DNSSECKeys").Where("id = ?", id)
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// 记录数与解析记录列表口径一致：排除系统自动维护的 apex 权威 NS 记录。
	var visibleCount int64
	for _, r := range domain.Records {
		if r.Type == model.RecordTypeNS && (r.Name == "@" || r.Name == "") {
			continue
		}
		visibleCount++
	}
	domain.RecordCount = visibleCount
	c.JSON(http.StatusOK, domain)
}

func (h *DomainHandler) UpdateDomain(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userID := c.GetUint("user_id")
	role := c.GetString("role")

	var domain model.Domain
	query := database.DB.Where("id = ?", id)
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	var req UpdateDomainReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update parameters"})
		return
	}

	updates := map[string]interface{}{
		"soa_serial": domain.SOASerial + 1,
		"updated_at": time.Now(),
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.PrimaryNS != "" {
		updates["primary_ns"] = req.PrimaryNS
	}
	if req.AdminEmail != "" {
		updates["admin_email"] = req.AdminEmail
	}
	if req.SOARefresh > 0 {
		updates["soa_refresh"] = req.SOARefresh
	}
	if req.SOARetry > 0 {
		updates["soa_retry"] = req.SOARetry
	}
	if req.SOAExpire > 0 {
		updates["soa_expire"] = req.SOAExpire
	}
	if req.SOAMinimum > 0 {
		updates["soa_minimum"] = req.SOAMinimum
	}

	database.DB.Model(&domain).Updates(updates)
	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Domain updated successfully"})
}

func (h *DomainHandler) DeleteDomain(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userID := c.GetUint("user_id")
	role := c.GetString("role")

	var domain model.Domain
	query := database.DB.Where("id = ?", id)
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// Delete domain and related records
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.Record{})
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.DNSSECKey{})
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.HealthCheck{})
	database.DB.Delete(&domain)

	dnsengine.GlobalZoneStore.DeleteZone(domain.Name)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Domain deleted successfully"})
}

func (h *DomainHandler) VerifyNS(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.First(&domain, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// 复用与后台定时扫描一致的校验逻辑，避免手动与自动两条路径判定规则漂移。
	matched, expectedNSList, foundNS := verifyDomainNS(h.cfg, &domain)

	c.JSON(http.StatusOK, gin.H{
		"ns_status":   domain.NSStatus,
		"expected_ns": expectedNSList,
		"found_ns":    foundNS,
		"verified":    matched,
	})
}

type TestRoutingReq struct {
	ClientIP  string `json:"client_ip" binding:"required"`
	QueryName string `json:"query_name"`
	QueryType string `json:"query_type"`
}

func (h *DomainHandler) TestRouting(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.Preload("Records").First(&domain, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	var req TestRoutingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client IP parameter"})
		return
	}

	ip := net.ParseIP(strings.TrimSpace(req.ClientIP))
	if ip == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IPv4 or IPv6 address"})
		return
	}

	loc := geo.MatchLocation(ip)

	qName := strings.TrimSpace(req.QueryName)
	if qName == "" {
		qName = "@"
	}
	qType := strings.ToUpper(strings.TrimSpace(req.QueryType))
	if qType == "" {
		qType = "A"
	}

	// 拼出完整查询 FQDN："@" 表示区域顶点，其余为子域名。
	fqdn := domain.Name
	if qName != "@" && qName != "" {
		fqdn = qName + "." + domain.Name
	}

	qtype := dns.TypeA
	if qType == "ANY" {
		qtype = dns.TypeANY
	} else if t, ok := dns.StringToType[qType]; ok {
		qtype = t
	}

	// 复用真实 DNS 引擎的分线决策，保证检测结果与线上解析完全一致。
	decision := dnsengine.GlobalZoneStore.ExplainRouting(fqdn, qtype, ip)

	// matched_records 仅包含最终会被应答的记录，与真实解析结果对齐。
	matchedRecords := make([]model.Record, 0, len(decision.Candidates))
	for _, cand := range decision.Candidates {
		if cand.Selected {
			matchedRecords = append(matchedRecords, cand.Record)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"client_ip":       ip.String(),
		"matched_line":    decision.MatchedLine,
		"client_line":     decision.ClientLine,
		"country_code":    loc.CountryCode,
		"country_name":    loc.CountryName,
		"continent":       loc.Continent,
		"asn":             loc.ASN,
		"isp":             loc.ISP,
		"source":          loc.Source,
		"query_name":      decision.QueryName,
		"query_type":      decision.QueryType,
		"selection_mode":  decision.SelectionMode,
		"candidates":      decision.Candidates,
		"matched_records": matchedRecords,
		"total_matched":   len(matchedRecords),
		"total_candidates": decision.TotalMatched,
	})
}
