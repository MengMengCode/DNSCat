package acme

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/miekg/dns"
)

const (
	// challengeLabel 是 RFC 8555 规定的 DNS-01 挑战记录前缀。
	challengeLabel = "_acme-challenge"
	// challengeTTL 取尽可能小的值，避免上一轮挑战的旧值被递归解析器缓存。
	challengeTTL = 30

	// 传播等待参数。边缘节点通过每 10s 一次的快照轮询同步，
	// 因此超时必须显著大于同步周期，否则多节点部署下会误判失败。
	propagationTimeout  = 3 * time.Minute
	propagationInterval = 5 * time.Second
	// dnsQueryTimeout 单次向权威 NS 发查询的超时。
	dnsQueryTimeout = 5 * time.Second
)

// pendingChallenge 是一条已写入、等待 CA 校验的挑战记录。
type pendingChallenge struct {
	fqdn     string // 完整挑战名，如 _acme-challenge.sub.example.com.
	value    string // TXT 值（token 的 SHA-256 摘要，由 ACME 客户端计算）
	recordID uint   // 数据库记录主键，用于收尾清理
}

// dns01Solver 负责在自有权威 DNS 中落地 ACME DNS-01 挑战记录。
//
// 与「写完就认为生效」的天真实现不同，这里必须等到记录在所有活跃权威 NS 上
// 都真实可查之后才能让 CA 开始校验：CA 会随机挑选权威 NS 查询，
// 只要有一台还没同步到，校验就会失败并消耗该订单的重试次数。
type dns01Solver struct {
	domain  *model.Domain
	zoneFQDN string
	// pending 保存本次订单写入的全部挑战。
	// 泛域名与 apex 的授权共用同一个挑战名但 token 不同，两条必须同时存在，
	// 因此这里是切片而非 map[fqdn]value。
	pending []pendingChallenge
}

func newDNS01Solver(domain *model.Domain) *dns01Solver {
	return &dns01Solver{
		domain:   domain,
		zoneFQDN: strings.ToLower(dns.Fqdn(domain.Name)),
	}
}

// challengeFQDNFor 计算某个授权标识对应的挑战记录名。
//
// 传入的 identifier 是 ACME 授权里的域名（泛域名授权已由 CA 剥掉 "*." 前缀）：
//   - example.com      → _acme-challenge.example.com.
//   - sub.example.com  → _acme-challenge.sub.example.com.
//
// 这一步不能简写成「统一挂在 zone apex 下」：子域名 SAN 的挑战必须落在
// 该子域名之下，否则 CA 查不到记录。
func challengeFQDNFor(identifier string) string {
	base := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identifier)), "*.")
	return dns.Fqdn(challengeLabel + "." + strings.TrimSuffix(base, "."))
}

// relativeName 把挑战 FQDN 转换为相对于本 zone 的记录名，
// 以匹配 Record.Name 的存储约定（ConvertRecordToRR 会再拼回 FQDN）。
//
//	_acme-challenge.example.com.     + zone example.com. → _acme-challenge
//	_acme-challenge.sub.example.com. + zone example.com. → _acme-challenge.sub
func (s *dns01Solver) relativeName(fqdn string) (string, error) {
	lowered := strings.ToLower(dns.Fqdn(fqdn))
	if !strings.HasSuffix(lowered, "."+s.zoneFQDN) && lowered != s.zoneFQDN {
		return "", fmt.Errorf("挑战名 %s 不属于托管区域 %s", fqdn, s.zoneFQDN)
	}
	relative := strings.TrimSuffix(lowered, "."+s.zoneFQDN)
	if relative == "" || relative == lowered {
		return "@", nil
	}
	return relative, nil
}

// present 写入一条挑战 TXT 记录。
func (s *dns01Solver) present(challengeFQDN, value string) error {
	name, err := s.relativeName(challengeFQDN)
	if err != nil {
		return err
	}

	rec := model.Record{
		DomainID: s.domain.ID,
		Name:     name,
		Type:     model.RecordTypeTXT,
		Value:    value,
		TTL:      challengeTTL,
		// 必须落在默认线路：CA 的校验源 IP 不可预知，
		// 若绑定到某条地理线路，从其他区域查询将拿不到记录。
		GeoLine: "default",
		Weight:  100,
		Enabled: true,
		Comment: "ACME DNS-01 challenge (auto-managed, safe to ignore)",
	}

	if err := database.DB.Create(&rec).Error; err != nil {
		return fmt.Errorf("写入 DNS-01 挑战记录失败: %w", err)
	}

	s.pending = append(s.pending, pendingChallenge{
		fqdn:     strings.ToLower(dns.Fqdn(challengeFQDN)),
		value:    value,
		recordID: rec.ID,
	})

	s.publishZone()
	return nil
}

// cleanUp 删除本次订单写入的全部临时记录。
// 使用 Unscoped 硬删除：Record 带 gorm.DeletedAt 软删除语义，
// 若只软删除，_acme-challenge 会在库里无限堆积。
func (s *dns01Solver) cleanUp() {
	if len(s.pending) == 0 {
		return
	}
	ids := make([]uint, 0, len(s.pending))
	for _, p := range s.pending {
		ids = append(ids, p.recordID)
	}
	if err := database.DB.Unscoped().
		Where("id IN ?", ids).
		Delete(&model.Record{}).Error; err != nil {
		log.Printf("[ACME] 清理 DNS-01 挑战记录失败 (domain=%s, ids=%v): %v",
			s.domain.Name, ids, err)
	}
	s.pending = nil
	s.publishZone()
}

// publishZone 让挑战记录的增删立即在本机权威引擎生效，并通知边缘节点。
func (s *dns01Solver) publishZone() {
	dnsengine.GlobalZoneStore.InvalidateZone(s.domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(s.domain.ID)
	}
}

// waitForPropagation 阻塞直到全部挑战记录在所有活跃权威 NS 上均可查到。
//
// 每台 NS 都必须能返回同一挑战名下的全部期望值（而非任意一个），
// 否则 CA 抽到缺失记录的那台就会校验失败。
func (s *dns01Solver) waitForPropagation() error {
	if len(s.pending) == 0 {
		return nil
	}

	// 按挑战名聚合期望值，逐名校验。
	expectedByName := make(map[string][]string)
	for _, p := range s.pending {
		expectedByName[p.fqdn] = append(expectedByName[p.fqdn], p.value)
	}

	servers := resolveAuthoritativeServers()
	if len(servers) == 0 {
		// 拿不到任何可探测的权威 NS 地址时不阻断流程：
		// 记录已写入本机引擎，让 CA 直接校验，其返回的失败信息更具诊断价值。
		log.Printf("[ACME] 未能解析到任何活跃权威 NS 地址，跳过传播等待 (domain=%s)", s.domain.Name)
		return nil
	}

	deadline := time.Now().Add(propagationTimeout)
	var lastIssue string

	for time.Now().Before(deadline) {
		lastIssue = ""

		for _, server := range servers {
			for name, expected := range expectedByName {
				found, err := queryTXT(server.addr, name)
				if err != nil {
					lastIssue = fmt.Sprintf("%s (%s) 查询 %s 失败: %v",
						server.hostname, server.addr, name, err)
					break
				}
				if missing := missingValues(expected, found); len(missing) > 0 {
					lastIssue = fmt.Sprintf("%s (%s) 上 %s 尚缺 %d 条挑战记录",
						server.hostname, server.addr, name, len(missing))
					break
				}
			}
			if lastIssue != "" {
				break
			}
		}

		if lastIssue == "" {
			log.Printf("[ACME] DNS-01 挑战记录已在 %d 台权威 NS 上全部生效 (domain=%s, 共 %d 条)",
				len(servers), s.domain.Name, len(s.pending))
			return nil
		}

		time.Sleep(propagationInterval)
	}

	return fmt.Errorf("等待 DNS-01 挑战记录全网生效超时（%s）：%s", propagationTimeout, lastIssue)
}

// authoritativeServer 是一台待探测的权威 NS。
type authoritativeServer struct {
	hostname string
	addr     string // host:port
}

// resolveAuthoritativeServers 汇总所有需要确认传播状态的权威 NS 地址。
//
// 优先使用 Nameserver 表中配置的 Glue IP 直连——直接问权威服务器可以绕过
// 递归解析器缓存，得到的是记录的真实状态。Glue IP 缺失时退回用系统解析器
// 解析其主机名。
func resolveAuthoritativeServers() []authoritativeServer {
	if database.DB == nil {
		return nil
	}

	var nsList []model.Nameserver
	if err := database.DB.Where("is_active = ?", true).Find(&nsList).Error; err != nil {
		log.Printf("[ACME] 读取权威 NS 列表失败: %v", err)
		return nil
	}

	seen := make(map[string]struct{})
	var servers []authoritativeServer

	add := func(hostname, ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			return
		}
		addr := net.JoinHostPort(ip, "53")
		if _, dup := seen[addr]; dup {
			return
		}
		seen[addr] = struct{}{}
		servers = append(servers, authoritativeServer{hostname: hostname, addr: addr})
	}

	for _, ns := range nsList {
		hostname := strings.TrimSuffix(ns.Hostname, ".")
		hadGlue := false
		if strings.TrimSpace(ns.IPv4) != "" {
			add(hostname, ns.IPv4)
			hadGlue = true
		}
		// IPv6 Glue 仅在没有 IPv4 时作为探测目标：
		// 运行环境未必具备 IPv6 出口，贸然探测会产生无意义的失败。
		if !hadGlue && strings.TrimSpace(ns.IPv6) != "" {
			add(hostname, ns.IPv6)
			hadGlue = true
		}
		if hadGlue || hostname == "" {
			continue
		}
		// 无 Glue 记录时借助系统解析器定位该 NS。
		if ips, err := net.LookupHost(hostname); err == nil {
			for _, ip := range ips {
				if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
					add(hostname, ip)
					break
				}
			}
		}
	}

	return servers
}

// queryTXT 直接向指定权威服务器查询 TXT 记录，返回全部字符串值。
// 不走系统解析器，避免递归缓存导致的假阴性/假阳性。
func queryTXT(serverAddr, fqdn string) ([]string, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(fqdn), dns.TypeTXT)
	msg.RecursionDesired = false

	client := &dns.Client{Timeout: dnsQueryTimeout}
	resp, _, err := client.Exchange(msg, serverAddr)
	if err != nil {
		return nil, err
	}
	if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
		return nil, fmt.Errorf("应答码 %s", dns.RcodeToString[resp.Rcode])
	}

	var values []string
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			// 长 TXT 会被拆成多个 255 字节分片，拼接后才是原始值。
			values = append(values, strings.Join(txt.Txt, ""))
		}
	}
	return values, nil
}

// missingValues 返回 expected 中尚未出现在 found 里的值。
func missingValues(expected, found []string) []string {
	present := make(map[string]struct{}, len(found))
	for _, f := range found {
		present[f] = struct{}{}
	}
	var missing []string
	for _, e := range expected {
		if _, ok := present[e]; !ok {
			missing = append(missing, e)
		}
	}
	return missing
}
