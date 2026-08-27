package api

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/geo"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type RoutingHandler struct{}

func NewRoutingHandler() *RoutingHandler { return &RoutingHandler{} }

type RoutingLineReq struct {
	Key         string `json:"key"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Continents  string `json:"continents"`
	Countries   string `json:"countries"`
	ASNs        string `json:"asns"`
	Priority    int    `json:"priority"`
	Enabled     *bool  `json:"enabled"`
}

var lineKeyRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = lineKeyRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// normalizeCSV 去重、去空并对每一项应用归一化函数，返回逗号分隔字符串。
func normalizeCSV(s string, fn func(string) string) string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, p := range parts {
		p = fn(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

func normalizeLineFields(req RoutingLineReq) (continents, countries, asns string) {
	continents = normalizeCSV(req.Continents, strings.ToUpper)
	countries = normalizeCSV(req.Countries, strings.ToLower)
	asns = normalizeCSV(req.ASNs, func(s string) string {
		return strings.TrimPrefix(strings.ToUpper(s), "AS")
	})
	return
}

func reloadAndBroadcastLines() {
	_ = dnsengine.GlobalLineStore.LoadFromDB()
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastLineChange()
	}
}

// countLineReferences 统计引用某条线路 key 的解析记录数。
// Record.GeoLine 可能是逗号分隔的多线路，这里按 CSV 成员精确匹配，
// 避免用简单子串（如把 "us" 误命中 "aus"）造成误判。
func countLineReferences(key string) int64 {
	var n int64
	database.DB.Model(&model.Record{}).
		Where("geo_line = ? OR geo_line LIKE ? OR geo_line LIKE ? OR geo_line LIKE ?",
			key, key+",%", "%,"+key, "%,"+key+",%").
		Count(&n)
	return n
}

func (h *RoutingHandler) ListLines(c *gin.Context) {
	var lines []model.RoutingLine
	if err := database.DB.Order("priority asc, id asc").Find(&lines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch routing lines"})
		return
	}
	// 为非内置线路统计引用它的解析记录数，供前端把「使用中」的线路禁用删除；
	// 内置线路本就不可删除，跳过以省去对 default 线路的全表扫描。
	for i := range lines {
		if !lines[i].IsBuiltin {
			lines[i].RefCount = countLineReferences(lines[i].Key)
		}
	}
	c.JSON(http.StatusOK, gin.H{"lines": lines, "total": len(lines)})
}

func (h *RoutingHandler) CreateLine(c *gin.Context) {
	var req RoutingLineReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid routing line parameters"})
		return
	}

	key := slugify(req.Key)
	if key == "" {
		key = slugify(req.Name)
	}
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Line key or name is required"})
		return
	}

	var count int64
	database.DB.Model(&model.RoutingLine{}).Where("`key` = ?", key).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "线路标识已存在 (line key already exists): " + key})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 100
	}
	cont, ctry, asns := normalizeLineFields(req)

	line := model.RoutingLine{
		Key:         key,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Continents:  cont,
		Countries:   ctry,
		ASNs:        asns,
		Priority:    priority,
		Enabled:     enabled,
		IsBuiltin:   false,
	}
	if err := database.DB.Create(&line).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create routing line"})
		return
	}

	reloadAndBroadcastLines()
	c.JSON(http.StatusCreated, line)
}

func (h *RoutingHandler) UpdateLine(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var line model.RoutingLine
	if err := database.DB.First(&line, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Routing line not found"})
		return
	}

	var req RoutingLineReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update parameters"})
		return
	}

	// 允许修改非内置线路的 key（需保持唯一）；内置线路 key 固定。
	if !line.IsBuiltin {
		if k := slugify(req.Key); k != "" && k != line.Key {
			var count int64
			database.DB.Model(&model.RoutingLine{}).Where("`key` = ? AND id <> ?", k, line.ID).Count(&count)
			if count == 0 {
				line.Key = k
			}
		}
	}

	cont, ctry, asns := normalizeLineFields(req)
	line.Name = strings.TrimSpace(req.Name)
	line.Description = strings.TrimSpace(req.Description)
	line.Continents = cont
	line.Countries = ctry
	line.ASNs = asns
	if req.Priority > 0 {
		line.Priority = req.Priority
	}
	if req.Enabled != nil {
		line.Enabled = *req.Enabled
	}

	if err := database.DB.Save(&line).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update routing line"})
		return
	}

	reloadAndBroadcastLines()
	c.JSON(http.StatusOK, line)
}

func (h *RoutingHandler) DeleteLine(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var line model.RoutingLine
	if err := database.DB.First(&line, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Routing line not found"})
		return
	}
	if line.IsBuiltin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "内置线路不可删除 (built-in lines cannot be deleted)"})
		return
	}

	// 仍被解析记录引用的线路不允许删除，否则这些记录会指向一条不存在的线路，
	// 解析时被迫回退到默认线路，产生非预期的分流行为。
	if refCount := countLineReferences(line.Key); refCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("该线路仍被 %d 条解析记录使用，无法删除；请先在 DNS 解析记录中修改这些记录的分线策略 (line is still used by %d DNS record(s); reassign their routing line first)", refCount, refCount),
		})
		return
	}

	if err := database.DB.Delete(&line).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete routing line"})
		return
	}

	reloadAndBroadcastLines()
	c.JSON(http.StatusOK, gin.H{"message": "Routing line deleted successfully"})
}

// TestRouting 全局路由测试：输入客户端 IP，返回其地理定位（国家/大洲/ASN）与命中的线路列表。
func (h *RoutingHandler) TestRouting(c *gin.Context) {
	var req struct {
		ClientIP string `json:"client_ip" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_ip is required"})
		return
	}
	ip := net.ParseIP(strings.TrimSpace(req.ClientIP))
	if ip == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IPv4 or IPv6 address"})
		return
	}

	loc := geo.MatchLocation(ip)

	var lines []model.RoutingLine
	database.DB.Where("enabled = ?", true).Order("priority asc, id asc").Find(&lines)
	matched := make([]model.RoutingLine, 0)
	for _, l := range lines {
		if strings.EqualFold(l.Key, "default") {
			continue
		}
		if geo.MatchLineCSV(loc, l.Continents, l.Countries, l.ASNs) {
			matched = append(matched, l)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"client_ip":     ip.String(),
		"country_code":  loc.CountryCode,
		"country_name":  loc.CountryName,
		"continent":     loc.Continent,
		"asn":           loc.ASN,
		"isp":           loc.ISP,
		"line":          string(loc.Line),
		"source":        loc.Source,
		"matched_lines": matched,
	})
}
