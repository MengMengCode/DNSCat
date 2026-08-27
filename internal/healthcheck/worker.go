package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
)

type HealthChecker struct {
	client    *http.Client
	interval  time.Duration
	timeout   time.Duration
	stopChan  chan struct{}
	isRunning bool
}

var GlobalChecker *HealthChecker

func NewHealthChecker(intervalSec, timeoutSec int) *HealthChecker {
	if intervalSec <= 0 {
		intervalSec = 15
	}
	if timeoutSec <= 0 {
		timeoutSec = 5
	}

	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
	}

	hc := &HealthChecker{
		client: &http.Client{
			Transport: tr,
			Timeout:   time.Duration(timeoutSec) * time.Second,
		},
		interval: time.Duration(intervalSec) * time.Second,
		timeout:  time.Duration(timeoutSec) * time.Second,
		stopChan: make(chan struct{}),
	}
	GlobalChecker = hc
	return hc
}

func (hc *HealthChecker) Start(ctx context.Context) {
	if hc.isRunning {
		return
	}
	hc.isRunning = true

	go func() {
		ticker := time.NewTicker(hc.interval)
		defer ticker.Stop()

		log.Printf("[HealthCheck] Worker started (Interval: %v, Timeout: %v)", hc.interval, hc.timeout)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hc.stopChan:
				return
			case <-ticker.C:
				hc.runChecks()
			}
		}
	}()
}

func (hc *HealthChecker) Stop() {
	if hc.isRunning {
		close(hc.stopChan)
		hc.isRunning = false
	}
}

func (hc *HealthChecker) runChecks() {
	if database.DB == nil {
		return
	}

	var checks []model.HealthCheck
	err := database.DB.Preload("Record").Find(&checks).Error
	if err != nil || len(checks) == 0 {
		return
	}

	now := time.Now()
	for _, check := range checks {
		// 每个探测器有自己的检查间隔，worker 的 tick 只是最小调度粒度。
		// 过去这里对所有探测器一律按全局间隔探测，UI 上的「检查间隔」是个死字段。
		if !hc.isDue(&check, now) {
			continue
		}
		go hc.probeSingle(check)
	}
}

// isDue 判断探测器是否已到下次探测时间。
// 容差取半个 tick：否则当探测器间隔与 tick 相等时，探测本身的耗时会让
// now-last 差一点点不到间隔，被跳过一轮，实际间隔翻倍。
func (hc *HealthChecker) isDue(check *model.HealthCheck, now time.Time) bool {
	if check.LastCheckedAt == nil {
		return true
	}
	interval := hc.interval
	if check.CheckIntervalSec > 0 {
		interval = time.Duration(check.CheckIntervalSec) * time.Second
	}
	return now.Sub(*check.LastCheckedAt) >= interval-hc.interval/2
}

// ProbeNow 立即探测并把结果写回数据库。
// 早期版本只返回瞬时结果而不落库，导致新建探测器后要等下一个 tick 才有真实
// 状态，在此之前 status 一直是建表默认的 healthy——又是一次「误报健康」。
func (hc *HealthChecker) ProbeNow(check *model.HealthCheck) (bool, int64, string) {
	success, latencyMs, msg := hc.executeProbe(check)
	hc.applyProbeResult(check, success, latencyMs, msg)
	return success, latencyMs, msg
}

func (hc *HealthChecker) probeSingle(check model.HealthCheck) {
	success, latencyMs, msg := hc.executeProbe(&check)
	hc.applyProbeResult(&check, success, latencyMs, msg)
}

// applyProbeResult 推进状态机：1 次失败降级为 degraded，连续 2 次判定 down，
// 成功则清零失败计数。同时同步内存 ZoneStore 完成容灾切换/回切。
func (hc *HealthChecker) applyProbeResult(check *model.HealthCheck, success bool, latencyMs int64, msg string) {
	now := time.Now()

	newStatus := model.HealthStatusHealthy
	consecutiveFails := check.ConsecutiveFails

	if !success {
		consecutiveFails++
		if consecutiveFails >= 2 {
			newStatus = model.HealthStatusDown
		} else {
			newStatus = model.HealthStatusDegraded
		}
	} else {
		consecutiveFails = 0
		newStatus = model.HealthStatusHealthy
	}

	healthy := (newStatus == model.HealthStatusHealthy)
	// 仅当探测失败且配置了备用 IP 时才算真正进入容灾切换状态。
	failoverActive := !healthy && strings.TrimSpace(check.FallbackIP) != ""

	// Update DB
	_ = database.DB.Model(&model.HealthCheck{}).Where("id = ?", check.ID).Updates(map[string]interface{}{
		"status":            newStatus,
		"consecutive_fails": consecutiveFails,
		"last_latency_ms":   latencyMs,
		"last_checked_at":   &now,
		"failover_active":   failoverActive,
	}).Error

	// 回写内存结构体，避免调用方复用同一实例再次探测时读到过期的失败计数
	// （否则连续手动探测会一直停在 degraded，永远升不到 down）。
	check.Status = newStatus
	check.ConsecutiveFails = consecutiveFails
	check.LastLatencyMs = latencyMs
	check.LastCheckedAt = &now
	check.FailoverActive = failoverActive

	// 同步内存 ZoneStore：失败时切到备用 IP，恢复时切回原源站。
	dnsengine.GlobalZoneStore.UpdateRecordHealth(check.DomainID, check.RecordID, healthy, check.FallbackIP)

	if !healthy {
		if failoverActive {
			log.Printf("[HealthCheck] Monitor '%s' (ID:%d) status %s: %s (Latency: %dms) -> failover to %s",
				check.Name, check.ID, newStatus, msg, latencyMs, check.FallbackIP)
		} else {
			log.Printf("[HealthCheck] Monitor '%s' (ID:%d) status changed to %s: %s (Latency: %dms)",
				check.Name, check.ID, newStatus, msg, latencyMs)
		}
	}
}

func (hc *HealthChecker) executeProbe(check *model.HealthCheck) (bool, int64, string) {
	start := time.Now()
	targetHost := check.Host
	if targetHost == "" && check.Record != nil {
		targetHost = check.Record.Value
	}
	if targetHost == "" {
		return false, 0, "Empty host"
	}

	// 协议统一按大写比较：常量为 HTTP/HTTPS/TCP/PING，
	// 若 API 调用方传了小写，之前会落到 default 分支被判成「健康」。
	switch model.HealthCheckProtocol(strings.ToUpper(strings.TrimSpace(string(check.Protocol)))) {
	case model.ProtocolHTTP, model.ProtocolHTTPS:
		proto := "http"
		port := check.Port
		if check.Protocol == model.ProtocolHTTPS {
			proto = "https"
			if port == 0 {
				port = 443
			}
		} else if port == 0 {
			port = 80
		}

		path := check.Path
		if path == "" {
			path = "/"
		}

		// 同理，URL 里的 IPv6 主机也必须带方括号才是合法 authority。
		url := proto + "://" + net.JoinHostPort(targetHost, strconv.Itoa(port)) + path
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, 0, err.Error()
		}
		req.Header.Set("User-Agent", "DnsCat-HealthProber/1.0")

		resp, err := hc.client.Do(req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			return false, latency, fmt.Sprintf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()

		expected := check.ExpectedCode
		if expected == 0 {
			expected = 200
		}

		if resp.StatusCode == expected || (expected == 200 && resp.StatusCode >= 200 && resp.StatusCode < 400) {
			return true, latency, fmt.Sprintf("HTTP %d OK", resp.StatusCode)
		}
		return false, latency, fmt.Sprintf("Expected status %d but received %d", expected, resp.StatusCode)

	case model.ProtocolTCP:
		port := check.Port
		if port == 0 {
			port = 80
		}
		// 用 JoinHostPort 而不是 "%s:%d"：IPv6 字面量必须加方括号，
		// 否则 2001:db8::1 会被拼成无法解析的 2001:db8::1:80。
		addr := net.JoinHostPort(targetHost, strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, hc.timeout)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			return false, latency, fmt.Sprintf("TCP dial failed: %v", err)
		}
		_ = conn.Close()
		return true, latency, "TCP Connection Established"

	case model.ProtocolPING:
		probeTimeout := hc.timeout
		if check.TimeoutSec > 0 {
			probeTimeout = time.Duration(check.TimeoutSec) * time.Second
		}
		latency, detail, err := probeICMPEcho(targetHost, probeTimeout)
		latencyMs := latency.Milliseconds()
		if err != nil {
			return false, latencyMs, err.Error()
		}
		return true, latencyMs, detail

	default:
		// 必须 fail-closed：容灾探测器如果因协议无法识别而报「健康」，
		// 故障时既不会告警也不会切换备用 IP，等于监控形同虚设。
		return false, 0, fmt.Sprintf("Unsupported protocol %q (expected HTTP/HTTPS/TCP/PING)", check.Protocol)
	}
}
