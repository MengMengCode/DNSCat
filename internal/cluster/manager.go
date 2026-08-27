package cluster

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"dnscat/internal/cache"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
)

const ZoneSyncChannel = "dnscat:cluster:zone_sync"

// NodeSampleRetention 是节点在线状态时序采样的保留期，超期的采样点会被周期性清理。
const NodeSampleRetention = 30 * 24 * time.Hour

// nodeSampleCleanupInterval 控制清理任务的最小间隔，避免每次存活检查都执行删除。
const nodeSampleCleanupInterval = time.Hour

type NodeHeartbeatReq struct {
	NodeID string `json:"node_id" binding:"required"`
	// PublicIP 由节点自己声明对外服务地址。主控看到的 TCP 源地址在以下场景并不可用：
	//   - 节点与主控同机、经容器网络回连时，源地址是网桥内网地址（如 172.18.0.5）；
	//   - 节点位于 NAT 之后时，源地址可能与其对外权威服务地址不一致。
	// 因此优先采用节点声明值，留空时才回退到连接源地址。
	PublicIP    string  `json:"public_ip"`
	Version     string  `json:"version"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
	NetRxBps    int64   `json:"net_rx_bps"`
	NetTxBps    int64   `json:"net_tx_bps"`
	// QPS 是最近 60s 滚动窗口的真实瞬时速率；TotalQueries 是自节点启动以来的累计查询数。
	// 两者分离后，控制台不再把累计查询数误当成瞬时 QPS 展示。
	QPS          int64 `json:"qps"`
	TotalQueries int64 `json:"total_queries"`
	LatencyMs    int64 `json:"latency_ms"`
	// GlobalCounts 是边缘节点的全局累计快照。主控按相邻心跳求差，写入分钟级
	// 区间桶，使全局 1H~30D 看板包含全部 Anycast 节点。指针用于兼容尚未升级、
	// 不会上报该字段的旧节点。
	GlobalCounts *dnsengine.DomainTelemetry           `json:"global_counts,omitempty"`
	GeoCounts    map[string]uint64                    `json:"geo_counts,omitempty"`
	DomainCounts map[string]dnsengine.DomainTelemetry `json:"domain_counts,omitempty"`
}

type ZoneSnapshot struct {
	Timestamp         int64               `json:"timestamp"`
	Domains           []model.Domain      `json:"domains"`
	DNSSECPrivateKeys map[uint]string     `json:"dnssec_private_keys,omitempty"`
	RoutingLines      []model.RoutingLine `json:"routing_lines,omitempty"`
}

// RestoreDNSSECPrivateKeys reattaches signing material after JSON decoding.
// DNSSECKey.PrivateKey stays hidden from normal management APIs; the separate
// map is only returned by the node-token-protected cluster sync endpoint.
func (s *ZoneSnapshot) RestoreDNSSECPrivateKeys() {
	for domainIndex := range s.Domains {
		for keyIndex := range s.Domains[domainIndex].DNSSECKeys {
			key := &s.Domains[domainIndex].DNSSECKeys[keyIndex]
			key.PrivateKey = s.DNSSECPrivateKeys[key.ID]
		}
	}
}

type ClusterManager struct {
	secretToken       string
	syncInterval      time.Duration
	nodes             sync.Map
	lastSampleCleanup time.Time
}

var GlobalCluster *ClusterManager

func InitClusterManager(secretToken string, syncInterval time.Duration) *ClusterManager {
	if syncInterval <= 0 {
		syncInterval = 10 * time.Second
	}
	GlobalCluster = &ClusterManager{
		secretToken:  secretToken,
		syncInterval: syncInterval,
	}
	return GlobalCluster
}

func (m *ClusterManager) Start(ctx context.Context) {
	log.Printf("[ClusterManager] Started node manager with heartbeat timeout check")

	// Subscribe to Redis PubSub for cross-node instant cache invalidation
	go m.listenPubSub(ctx)

	// Periodic liveness check
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkNodeLiveness()
			}
		}
	}()
}

func (m *ClusterManager) ValidateToken(token string) bool {
	return token == m.secretToken
}

// usableAdvertiseIP 判断一个地址是否适合作为节点对外服务地址展示。
// 回环、未指定、链路本地与 RFC1918/RFC4193 私有地址都不可用：
// 它们通常来自容器网桥或 NAT 内网，写进节点列表会让人误判节点的真实位置。
func usableAdvertiseIP(raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsPrivate()
}

// resolveNodeIP 依次尝试：节点声明的公网地址 -> 连接源地址 -> 库中既有地址。
func resolveNodeIP(declared, clientIP, existing string) string {
	if usableAdvertiseIP(declared) {
		return strings.TrimSpace(declared)
	}
	if usableAdvertiseIP(clientIP) {
		return clientIP
	}
	if existing != "" {
		return existing
	}
	// 全都不可用时保留源地址，至少不写空值（NOT NULL 字段）。
	if clientIP != "" {
		return clientIP
	}
	return declared
}

func (m *ClusterManager) RecordHeartbeat(req *NodeHeartbeatReq, clientIP string) error {
	geoStats := ""
	if len(req.GeoCounts) > 0 {
		if encoded, err := json.Marshal(req.GeoCounts); err == nil {
			geoStats = string(encoded)
		}
	}

	domainStats := ""
	if len(req.DomainCounts) > 0 {
		if encoded, err := json.Marshal(req.DomainCounts); err == nil {
			domainStats = string(encoded)
		}
	}

	var node model.Node
	err := database.DB.Where("node_id = ?", req.NodeID).First(&node).Error
	if err != nil {
		// Auto-register node if token was valid
		node = model.Node{
			NodeID:        req.NodeID,
			Name:          "Edge PoP (" + req.NodeID + ")",
			IP:            resolveNodeIP(req.PublicIP, clientIP, ""),
			SecretToken:   m.secretToken,
			IsOnline:      true,
			LastHeartbeat: time.Now(),
			Version:       req.Version,
			CPUUsage:      req.CPUUsage,
			MemoryUsage:   req.MemoryUsage,
			DiskUsage:     req.DiskUsage,
			NetRxBps:      req.NetRxBps,
			NetTxBps:      req.NetTxBps,
			QPS:           req.QPS,
			TotalQueries:  req.TotalQueries,
			LatencyMs:     req.LatencyMs,
			GeoStats:      geoStats,
			DomainStats:   domainStats,
		}
		if err := database.DB.Create(&node).Error; err != nil {
			return err
		}
		if req.GlobalCounts != nil {
			dnsengine.GlobalTelemetry.MergeEdgeTelemetry(req.NodeID, *req.GlobalCounts, req.DomainCounts)
		}
		m.nodes.Store(req.NodeID, time.Now())
		return nil
	}

	m.nodes.Store(req.NodeID, time.Now())

	ipToUpdate := resolveNodeIP(req.PublicIP, clientIP, node.IP)

	updates := map[string]interface{}{
		"ip":             ipToUpdate,
		"version":        req.Version,
		"is_online":      true,
		"last_heartbeat": time.Now(),
		"cpu_usage":      req.CPUUsage,
		"memory_usage":   req.MemoryUsage,
		"disk_usage":     req.DiskUsage,
		"net_rx_bps":     req.NetRxBps,
		"net_tx_bps":     req.NetTxBps,
		"qps":            req.QPS,
		"total_queries":  req.TotalQueries,
		"latency_ms":     req.LatencyMs,
	}
	if geoStats != "" {
		updates["geo_stats"] = geoStats
	}
	if domainStats != "" {
		updates["domain_stats"] = domainStats
	}
	if err := database.DB.Model(&node).Updates(updates).Error; err != nil {
		return err
	}
	if req.GlobalCounts != nil {
		dnsengine.GlobalTelemetry.MergeEdgeTelemetry(req.NodeID, *req.GlobalCounts, req.DomainCounts)
	}
	return nil
}

func (m *ClusterManager) GetFullSnapshot() (*ZoneSnapshot, error) {
	var domains []model.Domain
	err := database.DB.
		Preload("Records").
		Preload("DNSSECKeys").
		// 安全策略随快照下发，边缘节点才能执行与主控一致的防护规则。
		Preload("SecurityPolicy").
		Where("status = ?", model.DomainStatusActive).
		Find(&domains).Error
	if err != nil {
		return nil, err
	}

	privateKeys := make(map[uint]string)
	for domainIndex := range domains {
		for keyIndex := range domains[domainIndex].DNSSECKeys {
			key := &domains[domainIndex].DNSSECKeys[keyIndex]
			if key.PrivateKey != "" {
				privateKeys[key.ID] = key.PrivateKey
			}
		}
	}

	var lines []model.RoutingLine
	_ = database.DB.Where("enabled = ?", true).Find(&lines).Error

	return &ZoneSnapshot{
		Timestamp:         time.Now().Unix(),
		Domains:           domains,
		DNSSECPrivateKeys: privateKeys,
		RoutingLines:      lines,
	}, nil
}

func (m *ClusterManager) BroadcastZoneChange(domainID uint) {
	msg := map[string]interface{}{
		"action":    "invalidate_zone",
		"domain_id": domainID,
		"timestamp": time.Now().Unix(),
	}
	data, _ := json.Marshal(msg)
	if cache.GlobalCache != nil {
		_ = cache.GlobalCache.Publish(context.Background(), ZoneSyncChannel, string(data))
	}
	log.Printf("[Cluster] Broadcasted zone change for domain %d to all edge nodes", domainID)
}

// BroadcastLineChange 通知所有节点重新加载全局智能分线线路定义。
func (m *ClusterManager) BroadcastLineChange() {
	msg := map[string]interface{}{
		"action":    "reload_lines",
		"timestamp": time.Now().Unix(),
	}
	data, _ := json.Marshal(msg)
	if cache.GlobalCache != nil {
		_ = cache.GlobalCache.Publish(context.Background(), ZoneSyncChannel, string(data))
	}
	log.Printf("[Cluster] Broadcasted routing line change to all edge nodes")
}

func (m *ClusterManager) listenPubSub(ctx context.Context) {
	if cache.GlobalCache == nil {
		return
	}
	ch := cache.GlobalCache.Subscribe(ctx, ZoneSyncChannel)
	if ch == nil {
		return
	}
	for msg := range ch {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(msg), &payload); err == nil {
			if action, _ := payload["action"].(string); action == "reload_lines" {
				_ = dnsengine.GlobalLineStore.LoadFromDB()
				continue
			}
			if domainIDFloat, ok := payload["domain_id"].(float64); ok {
				domainID := uint(domainIDFloat)
				dnsengine.GlobalZoneStore.InvalidateZone(domainID)
			}
		}
	}
}

func (m *ClusterManager) checkNodeLiveness() {
	var nodes []model.Node
	database.DB.Find(&nodes)

	now := time.Now()
	threshold := now.Add(-60 * time.Second)
	samples := make([]model.NodeStatusSample, 0, len(nodes))
	for i := range nodes {
		node := nodes[i]
		online := node.IsOnline
		// 心跳超时：把当前在线的节点判定为离线。
		if online && node.LastHeartbeat.Before(threshold) {
			database.DB.Model(&node).Update("is_online", false)
			online = false
			log.Printf("[Cluster] Node %s (%s) marked offline (missed heartbeats)", node.NodeID, node.IP)
		}
		// 为每个节点写入一条状态采样，作为 Uptime 状态条的历史数据来源。
		samples = append(samples, model.NodeStatusSample{
			NodeID:      node.NodeID,
			IsOnline:    online,
			LatencyMs:   node.LatencyMs,
			CPUUsage:    node.CPUUsage,
			MemoryUsage: node.MemoryUsage,
			DiskUsage:   node.DiskUsage,
			NetRxBps:    node.NetRxBps,
			NetTxBps:    node.NetTxBps,
			QPS:         node.QPS,
			SampledAt:   now,
		})
	}

	if len(samples) > 0 {
		if err := database.DB.Create(&samples).Error; err != nil {
			log.Printf("[Cluster] Failed to persist node status samples: %v", err)
		}
	}

	// 周期性清理超过保留期的采样，避免时序表无限增长。
	if now.Sub(m.lastSampleCleanup) >= nodeSampleCleanupInterval {
		m.cleanupOldSamples(now)
		m.lastSampleCleanup = now
	}
}

// cleanupOldSamples 删除早于保留期的节点状态采样。
func (m *ClusterManager) cleanupOldSamples(now time.Time) {
	cutoff := now.Add(-NodeSampleRetention)
	result := database.DB.Where("sampled_at < ?", cutoff).Delete(&model.NodeStatusSample{})
	if result.Error != nil {
		log.Printf("[Cluster] Failed to prune node status samples: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		log.Printf("[Cluster] Pruned %d expired node status samples (older than %s)", result.RowsAffected, NodeSampleRetention)
	}
}
