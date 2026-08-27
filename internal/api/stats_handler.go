package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type StatsHandler struct{}

func NewStatsHandler() *StatsHandler {
	return &StatsHandler{}
}

func (h *StatsHandler) GetSummary(c *gin.Context) {
	var domainCount int64
	var recordCount int64
	var nodeCount int64
	var onlineNodeCount int64
	var healthCheckCount int64

	database.DB.Model(&model.Domain{}).Count(&domainCount)
	database.DB.Model(&model.Record{}).Count(&recordCount)
	database.DB.Model(&model.Node{}).Count(&nodeCount)
	database.DB.Model(&model.Node{}).Where("is_online = ?", true).Count(&onlineNodeCount)
	database.DB.Model(&model.HealthCheck{}).Count(&healthCheckCount)

	totalQ, blockedQ := dnsengine.GlobalTelemetry.GetTotals()
	currentQPS := dnsengine.GlobalTelemetry.GetCurrentQPS()

	c.JSON(http.StatusOK, gin.H{
		"domain_count":       domainCount,
		"record_count":       recordCount,
		"node_count":         nodeCount,
		"online_node_count":  onlineNodeCount,
		"health_check_count": healthCheckCount,
		"total_queries":      totalQ,
		"blocked_queries":    blockedQ,
		"qps":                currentQPS,
		"uptime_percent":     100.0,
	})
}

func (h *StatsHandler) GetQPSTrend(c *gin.Context) {
	rangeKey := dnsengine.NormalizeTrendRange(c.Query("range"))
	points := dnsengine.GlobalTelemetry.GetTrend(rangeKey)
	c.JSON(http.StatusOK, gin.H{"points": points, "range": rangeKey})
}

func (h *StatsHandler) GetTypeBreakdown(c *gin.Context) {
	// 指定区间时按分钟时序聚合（含边缘节点心跳增量）；未指定时返回主控累计。
	if rangeKey := c.Query("range"); rangeKey != "" {
		types := dnsengine.GlobalTelemetry.GetTypeBreakdownRange(dnsengine.NormalizeTrendRange(rangeKey))
		c.JSON(http.StatusOK, gin.H{"types": types, "range": dnsengine.NormalizeTrendRange(rangeKey)})
		return
	}
	types := dnsengine.GlobalTelemetry.GetTypeBreakdown()
	c.JSON(http.StatusOK, gin.H{"types": types})
}

func (h *StatsHandler) GetGeoBreakdown(c *gin.Context) {
	// 指定区间时按分钟时序聚合；边缘节点的相邻累计心跳差值已经由集群管理器
	// 写进该环形缓冲，所以这里得到的是全网 Anycast 的区间口径。
	if rangeKey := c.Query("range"); rangeKey != "" {
		counts := dnsengine.GlobalTelemetry.GetGeoCountsRange(dnsengine.NormalizeTrendRange(rangeKey))
		c.JSON(http.StatusOK, gin.H{"geo": dnsengine.BuildGeoBreakdown(counts), "range": dnsengine.NormalizeTrendRange(rangeKey)})
		return
	}

	// 未指定区间：主控累计 + 合并各边缘节点累计快照。
	counts := dnsengine.GlobalTelemetry.GetGeoCounts()
	var nodes []model.Node
	database.DB.Select("geo_stats").Find(&nodes)
	for _, node := range nodes {
		if node.GeoStats == "" {
			continue
		}
		var nodeCounts map[string]uint64
		if err := json.Unmarshal([]byte(node.GeoStats), &nodeCounts); err != nil {
			continue
		}
		for code, count := range nodeCounts {
			counts[code] += count
		}
	}
	geo := dnsengine.BuildGeoBreakdown(counts)
	c.JSON(http.StatusOK, gin.H{"geo": geo})
}

// GetDomainStats 返回单个托管域名的遥测数据，聚合主控与全部边缘节点的上报结果。
func (h *StatsHandler) GetDomainStats(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain id"})
		return
	}

	userID := c.GetUint("user_id")
	role := c.GetString("role")

	query := database.DB.Where("id = ?", id)
	if role != string(model.RoleAdmin) {
		query = query.Where("user_id = ?", userID)
	}

	var domain model.Domain
	if err := query.First(&domain).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	zoneKey := dnsengine.NormalizeZoneKey(domain.Name)

	// 主控自身的遥测
	aggregated := dnsengine.GlobalTelemetry.GetDomainTelemetry(zoneKey)

	// 合并各边缘节点心跳上报的区域遥测
	var nodes []model.Node
	database.DB.Select("domain_stats").Find(&nodes)
	for _, node := range nodes {
		if node.DomainStats == "" {
			continue
		}
		var nodeStats map[string]dnsengine.DomainTelemetry
		if err := json.Unmarshal([]byte(node.DomainStats), &nodeStats); err != nil {
			continue
		}
		if stats, ok := nodeStats[zoneKey]; ok {
			dnsengine.MergeDomainTelemetry(&aggregated, stats)
		}
	}

	var recordCount int64
	database.DB.Model(&model.Record{}).Where("domain_id = ?", domain.ID).Count(&recordCount)

	var healthCheckCount int64
	database.DB.Model(&model.HealthCheck{}).Where("domain_id = ?", domain.ID).Count(&healthCheckCount)

	var dnssecKeyCount int64
	database.DB.Model(&model.DNSSECKey{}).Where("domain_id = ?", domain.ID).Count(&dnssecKeyCount)

	// summary 保持累计口径（含边缘节点上报）。points/types/geo：
	// 指定区间时用全网分钟时序按区间聚合；未指定时用 24h 小时桶 + 累计分布。
	points := dnsengine.BuildHourlyTrend(aggregated.Hourly, aggregated.HourlyBlocked)
	typesBreakdown := dnsengine.BuildTypeBreakdownFromNames(aggregated.Types)
	geoBreakdown := dnsengine.BuildGeoBreakdown(aggregated.Countries)
	if rangeKey := c.Query("range"); rangeKey != "" {
		points, typesBreakdown, geoBreakdown = dnsengine.GlobalTelemetry.GetDomainRangeStats(zoneKey, dnsengine.NormalizeTrendRange(rangeKey))
	}

	c.JSON(http.StatusOK, gin.H{
		"domain": gin.H{
			"id":             domain.ID,
			"name":           domain.Name,
			"status":         domain.Status,
			"ns_status":      domain.NSStatus,
			"dnssec_enabled": domain.DNSSECEnabled,
			"created_at":     domain.CreatedAt,
		},
		"summary": gin.H{
			"total_queries":      aggregated.Queries,
			"blocked_queries":    aggregated.Blocked,
			"qps":                aggregated.QPS,
			"record_count":       recordCount,
			"health_check_count": healthCheckCount,
			"dnssec_key_count":   dnssecKeyCount,
		},
		"points": points,
		"types":  typesBreakdown,
		"geo":    geoBreakdown,
	})
}

func (h *StatsHandler) GetCarrierBreakdown(c *gin.Context) {
	carriers := dnsengine.GlobalTelemetry.GetCarrierBreakdown()
	c.JSON(http.StatusOK, gin.H{"carriers": carriers})
}

func (h *StatsHandler) GetAuditLogs(c *gin.Context) {
	var logs []model.AuditLog
	database.DB.Order("id desc").Limit(50).Find(&logs)
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}
