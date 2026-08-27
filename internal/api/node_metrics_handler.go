package api

import (
	"net/http"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

// metricPoint 是单节点性能曲线上的一个时间桶。
// 资源类指标在桶内取平均值，更能反映区间整体水位；无采样的桶 Has=false，
// 前端据此断开曲线，而不是把空桶画成 0 造成误读。
type metricPoint struct {
	Timestamp   int64   `json:"ts"`
	Has         bool    `json:"has"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
	NetRxBps    int64   `json:"net_rx_bps"`
	NetTxBps    int64   `json:"net_tx_bps"`
	QPS         int64   `json:"qps"`
	LatencyMs   int64   `json:"latency_ms"`
	Uptime      float64 `json:"uptime"`
}

// NodeMetrics 返回单个边缘节点在选定时间范围内的性能时序，
// 供节点状态页绘制 CPU / 内存 / 磁盘 / 网络 / QPS / 延迟曲线。
// 时间范围与分桶复用 uptimeRanges，保证与 Uptime 状态条口径一致。
func (h *NodeHandler) NodeMetrics(c *gin.Context) {
	// 用查询参数而非路径参数：与既有 /nodes/install-script?node_id= 保持一致，
	// 也避免在 gin 路由同层混用静态段与通配符段。
	nodeID := c.Query("node_id")

	var node model.Node
	if err := database.DB.Where("node_id = ?", nodeID).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	rangeKey := c.DefaultQuery("range", defaultUptimeRange)
	spec, ok := uptimeRanges[rangeKey]
	if !ok {
		rangeKey = defaultUptimeRange
		spec = uptimeRanges[defaultUptimeRange]
	}

	now := time.Now()
	start := now.Add(-spec.Window)
	bucketDur := spec.Window / time.Duration(spec.Buckets)

	type sampleRow struct {
		IsOnline    bool
		LatencyMs   int64
		CPUUsage    float64
		MemoryUsage float64
		DiskUsage   float64
		NetRxBps    int64
		NetTxBps    int64
		QPS         int64
		SampledAt   time.Time
	}
	var rows []sampleRow
	database.DB.Model(&model.NodeStatusSample{}).
		Select("is_online", "latency_ms", "cpu_usage", "memory_usage",
			"disk_usage", "net_rx_bps", "net_tx_bps", "qps", "sampled_at").
		Where("node_id = ? AND sampled_at >= ?", nodeID, start).
		Order("sampled_at asc").
		Find(&rows)

	// 每个桶的累加器：资源指标求和后取均值，在线率按采样计数。
	type acc struct {
		cpuSum, memSum, diskSum float64
		rxSum, txSum            int64
		qpsSum, latencySum      int64
		latencyCnt              int
		up, total               int
	}
	accs := make([]acc, spec.Buckets)

	for _, r := range rows {
		idx := int(r.SampledAt.Sub(start) / bucketDur)
		if idx < 0 {
			idx = 0
		}
		if idx >= spec.Buckets {
			idx = spec.Buckets - 1
		}
		a := &accs[idx]
		a.cpuSum += r.CPUUsage
		a.memSum += r.MemoryUsage
		a.diskSum += r.DiskUsage
		a.rxSum += r.NetRxBps
		a.txSum += r.NetTxBps
		a.qpsSum += r.QPS
		a.total++
		if r.IsOnline {
			a.up++
		}
		// 延迟只统计有效值，避免离线期间的 0 拉低均值。
		if r.LatencyMs > 0 {
			a.latencySum += r.LatencyMs
			a.latencyCnt++
		}
	}

	points := make([]metricPoint, spec.Buckets)
	var peakQPS, peakLatency int64
	var cpuSum, memSum float64
	var filled int
	for i := 0; i < spec.Buckets; i++ {
		a := accs[i]
		p := metricPoint{Timestamp: start.Add(time.Duration(i) * bucketDur).Unix()}
		if a.total > 0 {
			n := float64(a.total)
			p.Has = true
			p.CPUUsage = a.cpuSum / n
			p.MemoryUsage = a.memSum / n
			p.DiskUsage = a.diskSum / n
			p.NetRxBps = a.rxSum / int64(a.total)
			p.NetTxBps = a.txSum / int64(a.total)
			p.QPS = a.qpsSum / int64(a.total)
			p.Uptime = float64(a.up) / n * 100
			if a.latencyCnt > 0 {
				p.LatencyMs = a.latencySum / int64(a.latencyCnt)
			}

			if p.QPS > peakQPS {
				peakQPS = p.QPS
			}
			if p.LatencyMs > peakLatency {
				peakLatency = p.LatencyMs
			}
			cpuSum += p.CPUUsage
			memSum += p.MemoryUsage
			filled++
		}
		points[i] = p
	}

	summary := gin.H{
		"peak_qps":     peakQPS,
		"peak_latency": peakLatency,
		"sample_count": len(rows),
	}
	if filled > 0 {
		summary["avg_cpu"] = cpuSum / float64(filled)
		summary["avg_memory"] = memSum / float64(filled)
	} else {
		summary["avg_cpu"] = 0
		summary["avg_memory"] = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"node":           node,
		"range":          rangeKey,
		"start":          start.Unix(),
		"end":            now.Unix(),
		"bucket_seconds": int64(bucketDur / time.Second),
		"points":         points,
		"summary":        summary,
	})
}
