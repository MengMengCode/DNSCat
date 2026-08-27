package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/config"
	"dnscat/internal/dnsengine"
	"dnscat/internal/sysmetrics"
)

func main() {
	masterURL := flag.String("master", "http://127.0.0.1:8080", "Master server URL")
	nodeID := flag.String("node-id", "edge-node-01", "Unique Node ID")
	// 无默认值：集群令牌是拉取全量区域快照的唯一凭据，出厂默认值等于人人可读。
	secretToken := flag.String("token", "", "Cluster auth token (must match master's cluster.secret_token)")
	udpPort := flag.Int("udp", 5353, "Local UDP DNS listening port")
	tcpPort := flag.Int("tcp", 5353, "Local TCP DNS listening port")
	// 对外服务地址：与主控同机（走容器网络回连）或位于 NAT 之后时，
	// 主控看到的源地址是内网地址，需由节点自己声明真实的公网地址。
	publicIP := flag.String("public-ip", "", "Public IP this node serves DNS on (reported to master; optional)")
	flag.Parse()

	// 令牌优先从环境变量读取。命令行传参会出现在 ps 输出里，同机任何本地用户都能看到，
	// 而 systemd 的 EnvironmentFile 可以设成 0640，只有服务账号可读。
	token := *secretToken
	if token == "" {
		token = os.Getenv("DNSCAT_CLUSTER_TOKEN")
	}

	// 快速失败：令牌为空时主控会拒绝每一次同步与心跳，节点看起来在跑却永远拿不到区域，
	// 表现为「服务已启动但所有查询都是 SERVFAIL」，很难排查。不如启动时就说清楚。
	if token == "" {
		log.Fatalf("必须提供集群令牌（--token 或环境变量 DNSCAT_CLUSTER_TOKEN），" +
			"且须与主控 cluster.secret_token 一致，否则无法同步权威区域")
	}

	log.Printf("==================================================")
	log.Printf("  DnsCat Edge Node Daemon (%s)", *nodeID)
	log.Printf("  Master: %s", *masterURL)
	log.Printf("  DNS Listeners: UDP:%d, TCP:%d", *udpPort, *tcpPort)
	if *publicIP != "" {
		log.Printf("  Advertised Public IP: %s", *publicIP)
	}
	log.Printf("==================================================")

	store := &dnsengine.ZoneStore{}

	cfg := &config.Config{
		DNS: config.DNSConfig{
			UDPPort:   *udpPort,
			TCPPort:   *tcpPort,
			RateLimit: 5000,
		},
	}

	dnsServer := dnsengine.NewServer(cfg, store)
	if err := dnsServer.Start(); err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initial Sync
	syncZones(client, *masterURL, token, store)

	// Periodic Heartbeat and Sync loop
	// 心跳更频繁（3s），让控制台的边缘节点性能面板接近实时刷新；
	// zone 快照同步保持较低频率（10s），以控制主控负载与带宽占用。
	go func() {
		heartbeatTicker := time.NewTicker(3 * time.Second)
		syncTicker := time.NewTicker(10 * time.Second)
		defer heartbeatTicker.Stop()
		defer syncTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				sendHeartbeat(client, *masterURL, *nodeID, token, *publicIP, store)
			case <-syncTicker.C:
				syncZones(client, *masterURL, token, store)
			}
		}
	}()

	// Wait for OS interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down Edge Node daemon...")
	dnsServer.Stop()
	log.Printf("Edge Node terminated safely.")
}

func syncZones(client *http.Client, masterURL, token string, store *dnsengine.ZoneStore) {
	url := fmt.Sprintf("%s/api/node/sync", masterURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Node-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Sync Error] Failed to connect to master: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Sync Error] Master returned %d: %s", resp.StatusCode, string(body))
		return
	}

	var snapshot cluster.ZoneSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		log.Printf("[Sync Error] Failed to decode snapshot: %v", err)
		return
	}

	snapshot.RestoreDNSSECPrivateKeys()
	store.ReplaceZones(snapshot.Domains)
	dnsengine.GlobalLineStore.ReplaceLines(snapshot.RoutingLines)
	log.Printf("[Sync] Synchronized %d authoritative zones and %d routing lines from master", len(snapshot.Domains), len(snapshot.RoutingLines))
}

func sendHeartbeat(client *http.Client, masterURL, nodeID, token, publicIP string, store *dnsengine.ZoneStore) {
	totalQ, _ := store.GetTotalStats()
	globalCounts := dnsengine.GlobalTelemetry.GetTelemetrySnapshot()
	// 采集本机真实资源占用；CPU 与网络速率需要两次采样才有数值，
	// 因此节点启动后的第一次心跳这两项为 0，属预期表现。
	sys := sysmetrics.Collect()
	hb := cluster.NodeHeartbeatReq{
		NodeID:      nodeID,
		PublicIP:    publicIP,
		CPUUsage:    sys.CPUPercent,
		MemoryUsage: sys.MemPercent,
		DiskUsage:   sys.DiskPercent,
		NetRxBps:    sys.NetRxBps,
		NetTxBps:    sys.NetTxBps,
		// QPS 上报最近 60s 滚动窗口的真实瞬时速率；TotalQueries 才是自启动以来的
		// 累计查询数。早前 QPS 字段错误地填了累计值，导致控制台把累计数当瞬时 QPS 展示。
		QPS:          dnsengine.GlobalTelemetry.GetCurrentQPS(),
		TotalQueries: int64(totalQ),
		// 上报本节点自身的 DNS 处理耗时（微秒转毫秒），不再使用固定值。
		LatencyMs:    dnsengine.GlobalTelemetry.GetAvgLatencyMs(),
		Version:      "v1.0.0",
		GlobalCounts: &globalCounts,
		GeoCounts:    globalCounts.Countries,
		DomainCounts: dnsengine.GlobalTelemetry.GetDomainCounts(),
	}

	body, _ := json.Marshal(hb)
	url := fmt.Sprintf("%s/api/node/heartbeat", masterURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Token", token)

	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}
