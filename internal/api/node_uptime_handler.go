package api

import (
	"net/http"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

// uptimeRangeSpec 定义某个时间范围的窗口长度与状态条分桶数量。
type uptimeRangeSpec struct {
	Window  time.Duration
	Buckets int
}

// uptimeRanges 枚举前端可选的时间范围。分桶数量按范围调整，保证状态条视觉密度合理。
var uptimeRanges = map[string]uptimeRangeSpec{
	"1h":  {time.Hour, 30},
	"12h": {12 * time.Hour, 36},
	"24h": {24 * time.Hour, 48},
	"3d":  {3 * 24 * time.Hour, 36},
	"7d":  {7 * 24 * time.Hour, 42},
	"14d": {14 * 24 * time.Hour, 56},
	"30d": {30 * 24 * time.Hour, 60},
}

// defaultUptimeRange 是 range 参数缺失或非法时的回退值。
const defaultUptimeRange = "24h"

// uptimeBucket 是状态条上的一个时间片。
type uptimeBucket struct {
	Timestamp    int64   `json:"ts"`     // 桶起始时间（Unix 秒）
	Status       string  `json:"status"` // up / down / partial / none
	Uptime       float64 `json:"uptime"` // 该桶在线率（0-100），无数据为 0
	Up           int     `json:"up"`     // 在线采样数
	Down         int     `json:"down"`   // 离线采样数
	Total        int     `json:"total"`  // 采样总数
	AvgLatencyMs int64   `json:"avg_latency_ms"`
}

// uptimeNode 是单个边缘节点在选定范围内的聚合结果。
type uptimeNode struct {
	NodeID           string         `json:"node_id"`
	Name             string         `json:"name"`
	IP               string         `json:"ip"`
	Region           string         `json:"region"`
	IsOnline         bool           `json:"is_online"`
	CurrentLatencyMs int64          `json:"current_latency_ms"`
	OverallUptime    float64        `json:"overall_uptime"` // 整个范围内在线率（0-100）
	AvgLatencyMs     int64          `json:"avg_latency_ms"`
	SampleCount      int            `json:"sample_count"`
	Buckets          []uptimeBucket `json:"buckets"`
}

// NodeUptime 返回各边缘节点在选定时间范围内的分桶在线率，供前端绘制 Uptime 风格状态条。
func (h *NodeHandler) NodeUptime(c *gin.Context) {
	rangeKey := c.DefaultQuery("range", defaultUptimeRange)
	spec, ok := uptimeRanges[rangeKey]
	if !ok {
		rangeKey = defaultUptimeRange
		spec = uptimeRanges[defaultUptimeRange]
	}

	now := time.Now()
	start := now.Add(-spec.Window)
	bucketDur := spec.Window / time.Duration(spec.Buckets)

	// 载入节点清单（稳定顺序）。
	var nodes []model.Node
	if err := database.DB.Order("id asc").Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nodes"})
		return
	}

	// 载入范围内的采样，仅取聚合所需列以降低内存开销。
	type sampleRow struct {
		NodeID    string
		IsOnline  bool
		LatencyMs int64
		SampledAt time.Time
	}
	var rows []sampleRow
	database.DB.Model(&model.NodeStatusSample{}).
		Select("node_id", "is_online", "latency_ms", "sampled_at").
		Where("sampled_at >= ?", start).
		Order("sampled_at asc").
		Find(&rows)

	// 每个节点一组桶累加器。
	type acc struct {
		up         int
		down       int
		latencySum int64
		latencyCnt int
	}
	nodeBuckets := make(map[string][]acc, len(nodes))
	for _, n := range nodes {
		nodeBuckets[n.NodeID] = make([]acc, spec.Buckets)
	}

	for _, r := range rows {
		buckets, exists := nodeBuckets[r.NodeID]
		if !exists {
			continue // 采样归属的节点已删除，跳过
		}
		idx := int(r.SampledAt.Sub(start) / bucketDur)
		if idx < 0 {
			idx = 0
		}
		if idx >= spec.Buckets {
			idx = spec.Buckets - 1
		}
		if r.IsOnline {
			buckets[idx].up++
		} else {
			buckets[idx].down++
		}
		if r.LatencyMs > 0 {
			buckets[idx].latencySum += r.LatencyMs
			buckets[idx].latencyCnt++
		}
	}

	result := make([]uptimeNode, 0, len(nodes))
	for _, n := range nodes {
		accs := nodeBuckets[n.NodeID]
		buckets := make([]uptimeBucket, spec.Buckets)
		var totalUp, totalAll int
		var totalLatencySum int64
		var totalLatencyCnt int
		for i := 0; i < spec.Buckets; i++ {
			a := accs[i]
			total := a.up + a.down
			b := uptimeBucket{
				Timestamp: start.Add(time.Duration(i) * bucketDur).Unix(),
				Up:        a.up,
				Down:      a.down,
				Total:     total,
			}
			if total == 0 {
				b.Status = "none"
			} else {
				b.Uptime = float64(a.up) / float64(total) * 100
				switch {
				case a.up == total:
					b.Status = "up"
				case a.up == 0:
					b.Status = "down"
				default:
					b.Status = "partial"
				}
			}
			if a.latencyCnt > 0 {
				b.AvgLatencyMs = a.latencySum / int64(a.latencyCnt)
			}
			buckets[i] = b

			totalUp += a.up
			totalAll += total
			totalLatencySum += a.latencySum
			totalLatencyCnt += a.latencyCnt
		}

		un := uptimeNode{
			NodeID:           n.NodeID,
			Name:             n.Name,
			IP:               n.IP,
			Region:           n.Region,
			IsOnline:         n.IsOnline,
			CurrentLatencyMs: n.LatencyMs,
			SampleCount:      totalAll,
			Buckets:          buckets,
		}
		if totalAll > 0 {
			un.OverallUptime = float64(totalUp) / float64(totalAll) * 100
		}
		if totalLatencyCnt > 0 {
			un.AvgLatencyMs = totalLatencySum / int64(totalLatencyCnt)
		}
		result = append(result, un)
	}

	c.JSON(http.StatusOK, gin.H{
		"range":          rangeKey,
		"start":          start.Unix(),
		"end":            now.Unix(),
		"bucket_count":   spec.Buckets,
		"bucket_seconds": int64(bucketDur / time.Second),
		"nodes":          result,
	})
}
