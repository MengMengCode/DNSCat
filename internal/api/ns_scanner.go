package api

import (
	"context"
	"log"
	"net"
	"strings"
	"time"

	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/model"
)

// nsScanInterval 是 NS 指向自动扫描的默认周期。
const nsScanInterval = 30 * time.Minute

// verifyDomainNS 计算该域名期望的权威 NS 集合，查询其公网 NS 记录并比对，
// 命中任一期望 NS 判定为 verified，否则 unverified，仅在状态变化时写回 ns_status。
// 返回是否命中、期望 NS 列表、公网实际查到的 NS 列表。
//
// 手动校验（VerifyNS handler）与后台定时扫描共用此函数，期望 NS 的聚合复用
// authoritativeNSHostnames，与建域名时写入的 NS 保持一致，避免判定规则漂移。
func verifyDomainNS(cfg *config.Config, domain *model.Domain) (matched bool, expected []string, found []string) {
	expectedNSMap := make(map[string]bool)
	var expectedNSList []string

	var defaults []string
	if cfg != nil {
		defaults = cfg.DNS.DefaultNS
	}
	for _, hName := range authoritativeNSHostnames(defaults) {
		if !expectedNSMap[hName] {
			expectedNSMap[hName] = true
			expectedNSList = append(expectedNSList, hName)
		}
	}

	// 叠加域名自身的 PrimaryNS。
	if domain.PrimaryNS != "" {
		hName := strings.TrimSuffix(strings.ToLower(domain.PrimaryNS), ".")
		if hName != "" && !expectedNSMap[hName] {
			expectedNSMap[hName] = true
			expectedNSList = append(expectedNSList, hName)
		}
	}

	// 系统未配置任何权威 NS 时不再假定一组固定主机名——那会拿本项目作者的域名
	// 去和使用者在注册商处的真实 NS 比对，永远判为不匹配。此时无从判断，
	// 保持期望列表为空，由调用方按「未配置」处理。

	var foundNS []string
	foundMap := make(map[string]bool)
	if nss, err := net.LookupNS(domain.Name); err == nil {
		for _, ns := range nss {
			host := strings.TrimSuffix(strings.ToLower(ns.Host), ".")
			if host != "" && !foundMap[host] {
				foundMap[host] = true
				foundNS = append(foundNS, host)
				if expectedNSMap[host] {
					matched = true
				}
			}
		}
	}

	newStatus := "unverified"
	if matched {
		newStatus = "verified"
	}
	if domain.NSStatus != newStatus {
		database.DB.Model(&model.Domain{}).Where("id = ?", domain.ID).Update("ns_status", newStatus)
		domain.NSStatus = newStatus
	}

	return matched, expectedNSList, foundNS
}

// StartNSScanner 启动后台 goroutine，周期性自动校验所有域名的 NS 指向状态。
// 启动后先延迟一段时间再首扫，避开进程刚启动、公网 DNS 尚未稳定的窗口；
// 通过 ctx 取消可优雅退出。interval <= 0 时回退到 nsScanInterval。
func StartNSScanner(ctx context.Context, cfg *config.Config, interval time.Duration) {
	if interval <= 0 {
		interval = nsScanInterval
	}

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		scanAllDomainsNS(cfg)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scanAllDomainsNS(cfg)
			}
		}
	}()

	log.Printf("[NSScan] 已启动 NS 指向自动扫描，周期 %s", interval)
}

// scanAllDomainsNS 遍历所有域名执行一次 NS 校验。串行执行：域名规模通常不大，
// 且运行在低频后台，net.LookupNS 逐个走系统解析器查询即可。
func scanAllDomainsNS(cfg *config.Config) {
	if database.DB == nil {
		return
	}
	var domains []model.Domain
	if err := database.DB.Find(&domains).Error; err != nil {
		log.Printf("[NSScan] 加载域名列表失败: %v", err)
		return
	}
	if len(domains) == 0 {
		return
	}

	start := time.Now()
	changed := 0
	for i := range domains {
		prev := domains[i].NSStatus
		verifyDomainNS(cfg, &domains[i])
		if domains[i].NSStatus != prev {
			changed++
		}
	}
	log.Printf("[NSScan] 已扫描 %d 个域名的 NS 指向，状态变更 %d 个，耗时 %s",
		len(domains), changed, time.Since(start).Round(time.Millisecond))
}
