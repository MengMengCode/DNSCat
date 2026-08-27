package dnsengine

import (
	"crypto"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"dnscat/internal/database"
	"dnscat/internal/geo"
	"dnscat/internal/model"

	"github.com/miekg/dns"
)

type CachedRecord struct {
	Record     model.Record
	RR         dns.RR
	IsHealthy  bool
	FailoverRR dns.RR // 探测失败时用于容灾切换的备用记录（配置 fallback_ip 时生成），nil 表示无备用
}

// effectiveRR 返回当前实际用于应答的资源记录：
// 健康时返回原始 RR；不健康但配置了备用 IP 时返回备用 RR（容灾切换）；
// 不健康且无备用时返回 nil，表示应从应答中剔除。
func (cr CachedRecord) effectiveRR() dns.RR {
	if cr.IsHealthy {
		return cr.RR
	}
	if cr.FailoverRR != nil {
		return cr.FailoverRR
	}
	return nil
}

type CachedZone struct {
	Domain model.Domain
	SOA    *dns.SOA
	// Security 是本区域安全策略的编译产物；nil 表示未配置策略（不做额外防护）。
	Security      *SecurityRuntime
	Records       []CachedRecord
	DNSSECEnabled bool
	KSK           *dns.DNSKEY
	KSKPriv       crypto.PrivateKey
	ZSK           *dns.DNSKEY
	ZSKPriv       crypto.PrivateKey
}

type ZoneStore struct {
	zones  sync.Map // map[string]*CachedZone (key: lowercase FQDN "example.com.")
	totalQ uint64
	blockQ uint64
}

var GlobalZoneStore = &ZoneStore{}

// LoadFromDB loads all active domains and records into memory
func (zs *ZoneStore) LoadFromDB() error {
	if database.DB == nil {
		return nil
	}

	var domains []model.Domain
	err := database.DB.Preload("Records").Preload("DNSSECKeys").Preload("SecurityPolicy").
		Where("status = ?", model.DomainStatusActive).
		Find(&domains).Error
	if err != nil {
		return err
	}

	zs.ReplaceZones(domains)
	return nil
}

// ReplaceZones applies a full authoritative snapshot. Zones omitted from the
// snapshot are removed so deleted or paused domains cannot remain answerable on
// edge nodes after a synchronization cycle.
func (zs *ZoneStore) ReplaceZones(domains []model.Domain) {
	present := make(map[string]struct{}, len(domains))
	for i := range domains {
		fqdn := strings.ToLower(EnsureFQDN(domains[i].Name))
		present[fqdn] = struct{}{}
		zs.LoadZone(&domains[i])
	}

	zs.zones.Range(func(key, _ interface{}) bool {
		fqdn, ok := key.(string)
		if !ok {
			zs.zones.Delete(key)
			return true
		}
		if _, ok := present[fqdn]; !ok {
			zs.zones.Delete(fqdn)
		}
		return true
	})
}

// LoadZone parses and caches a single domain
func (zs *ZoneStore) LoadZone(domain *model.Domain) {
	fqdn := strings.ToLower(EnsureFQDN(domain.Name))
	cz := &CachedZone{
		Domain:        *domain,
		SOA:           BuildSOA(domain),
		DNSSECEnabled: domain.DNSSECEnabled,
		// 边缘节点通过集群快照拿到同一份策略，因此这里统一编译即可。
		Security: CompileSecurityRuntime(domain.SecurityPolicy),
	}

	// Load DNSSEC keys if enabled
	if domain.DNSSECEnabled {
		for _, k := range domain.DNSSECKeys {
			priv, err := ParsePrivateKeyFromPEM(k.PrivateKey)
			if err == nil {
				keyRR := &dns.DNSKEY{
					Hdr: dns.RR_Header{
						Name:   fqdn,
						Rrtype: dns.TypeDNSKEY,
						Class:  dns.ClassINET,
						Ttl:    3600,
					},
					Flags:     k.Flags,
					Protocol:  3,
					Algorithm: k.Algorithm,
					PublicKey: k.PublicKey,
				}
				if k.KeyType == model.KeyTypeKSK {
					cz.KSK = keyRR
					cz.KSKPriv = priv
				} else {
					cz.ZSK = keyRR
					cz.ZSKPriv = priv
				}
			}
		}
	}

	// Load Records
	var cachedRecords []CachedRecord
	for _, r := range domain.Records {
		if !r.Enabled {
			continue
		}
		rr, err := ConvertRecordToRR(domain.Name, &r)
		if err == nil {
			cachedRecords = append(cachedRecords, CachedRecord{
				Record:    r,
				RR:        rr,
				IsHealthy: true,
			})
		}
	}
	cz.Records = cachedRecords

	// 依据数据库中的健康探测状态初始化容灾态，使主控在 reload/重启后
	// 不丢失当前的故障转移状态。边缘节点无数据库连接（database.DB == nil），
	// 其容灾态改由集群快照 HealthOverrides 与 PubSub 下发。
	zs.applyStoredHealth(cz)

	zs.zones.Store(fqdn, cz)
}

// buildFailoverRR 依据原始记录与配置的备用 IP，构造用于容灾切换的资源记录。
// 备用记录沿用原记录的名称与 TTL，仅替换地址，使解析器透明地切到备用源站。
// 备用 IP 为空或与原记录地址族不匹配时返回 nil，表示无可用备用。
func buildFailoverRR(original dns.RR, fallbackIP string) dns.RR {
	fallbackIP = strings.TrimSpace(fallbackIP)
	if original == nil || fallbackIP == "" {
		return nil
	}

	ip := net.ParseIP(fallbackIP)
	if ip == nil {
		return nil
	}

	hdr := *original.Header()
	if ipv4 := ip.To4(); ipv4 != nil {
		// 仅当原记录本身是 A 记录时才做同族替换，避免把 AAAA 降级成 A。
		if hdr.Rrtype != dns.TypeA {
			return nil
		}
		hdr.Rrtype = dns.TypeA
		return &dns.A{Hdr: hdr, A: ipv4}
	}

	if hdr.Rrtype != dns.TypeAAAA {
		return nil
	}
	hdr.Rrtype = dns.TypeAAAA
	return &dns.AAAA{Hdr: hdr, AAAA: ip.To16()}
}

// applyStoredHealth 依据数据库中的 HealthCheck 记录，将对应 DNS 记录标记为
// 不健康并生成容灾备用记录。仅在具备数据库连接的主控节点上生效。
func (zs *ZoneStore) applyStoredHealth(cz *CachedZone) {
	if database.DB == nil {
		return
	}
	var checks []model.HealthCheck
	if err := database.DB.Where("domain_id = ?", cz.Domain.ID).Find(&checks).Error; err != nil {
		return
	}
	byRecord := make(map[uint]model.HealthCheck, len(checks))
	for _, ck := range checks {
		byRecord[ck.RecordID] = ck
	}
	for i := range cz.Records {
		ck, ok := byRecord[cz.Records[i].Record.ID]
		if !ok {
			continue
		}
		healthy := ck.Status == model.HealthStatusHealthy
		cz.Records[i].IsHealthy = healthy
		if !healthy {
			cz.Records[i].FailoverRR = buildFailoverRR(cz.Records[i].RR, ck.FallbackIP)
		}
	}
}

// InvalidateZone removes or reloads a zone
func (zs *ZoneStore) InvalidateZone(domainID uint) {
	if database.DB == nil {
		return
	}
	var domain model.Domain
	// 必须一并预加载 SecurityPolicy：否则任何一次区域重载都会把已编译的
	// 安全运行时置空，导致该域名的防护规则被静默清除。
	if err := database.DB.Preload("Records").Preload("DNSSECKeys").Preload("SecurityPolicy").First(&domain, domainID).Error; err == nil && domain.Status == model.DomainStatusActive {
		zs.LoadZone(&domain)
		return
	}
	zs.DeleteZoneByID(domainID)
}

// DeleteZone removes a zone from memory
func (zs *ZoneStore) DeleteZone(domainName string) {
	fqdn := strings.ToLower(EnsureFQDN(domainName))
	zs.zones.Delete(fqdn)
}

// DeleteZoneByID removes a zone when its database row is no longer available
// (for example after a soft delete) or is no longer active.
func (zs *ZoneStore) DeleteZoneByID(domainID uint) {
	zs.zones.Range(func(key, value interface{}) bool {
		cz, ok := value.(*CachedZone)
		if ok && cz.Domain.ID == domainID {
			zs.zones.Delete(key)
			return false
		}
		return true
	})
}

// UpdateRecordHealth 更新内存中指定记录的健康状态。
// 探测失败且配置了 fallbackIP 时同时生成容灾备用记录，使解析立即切到备用源站；
// 恢复健康时清除备用记录，自动切回原源站。
func (zs *ZoneStore) UpdateRecordHealth(domainID uint, recordID uint, healthy bool, fallbackIP string) {
	var targetKey interface{}
	var targetZone *CachedZone
	zs.zones.Range(func(key, value interface{}) bool {
		cz := value.(*CachedZone)
		if cz.Domain.ID == domainID {
			targetKey = key
			targetZone = cz
			return false
		}
		return true
	})

	if targetZone != nil {
		updated := *targetZone
		updated.Records = append([]CachedRecord(nil), targetZone.Records...)
		for i := range updated.Records {
			if updated.Records[i].Record.ID == recordID {
				updated.Records[i].IsHealthy = healthy
				if healthy {
					updated.Records[i].FailoverRR = nil
				} else {
					updated.Records[i].FailoverRR = buildFailoverRR(updated.Records[i].RR, fallbackIP)
				}
				zs.zones.Store(targetKey, &updated)
				return
			}
		}
	}
}

// FindZone finds the matching authoritative zone for a query FQDN
func (zs *ZoneStore) FindZone(qname string) (*CachedZone, string) {
	qname = strings.ToLower(EnsureFQDN(qname))
	labels := dns.SplitDomainName(qname)

	for i := 0; i < len(labels); i++ {
		candidate := strings.Join(labels[i:], ".") + "."
		if val, ok := zs.zones.Load(candidate); ok {
			return val.(*CachedZone), candidate
		}
	}
	return nil, ""
}

// Query performs authoritative lookups in the zone
func (zs *ZoneStore) Query(qname string, qtype uint16, clientIP net.IP, ecsIP net.IP, doDNSSEC bool) *dns.Msg {
	atomic.AddUint64(&zs.totalQ, 1)

	resp := new(dns.Msg)
	resp.Authoritative = true

	qname = strings.ToLower(EnsureFQDN(qname))
	cz, zoneName := zs.FindZone(qname)
	if cz == nil {
		resp.Rcode = dns.RcodeRefused
		return resp
	}

	// Effective IP for Line matching: prefer ECS IP if available
	matchIP := clientIP
	if ecsIP != nil {
		matchIP = ecsIP
	}
	clientLoc := geo.MatchLocation(matchIP)

	// Check if querying SOA
	if qtype == dns.TypeSOA && qname == zoneName {
		if cz.SOA != nil {
			resp.Answer = append(resp.Answer, cz.SOA)
			if doDNSSEC && cz.DNSSECEnabled && cz.ZSK != nil {
				if sig, err := SignRRSet([]dns.RR{cz.SOA}, cz.ZSK, cz.ZSKPriv, zoneName); err == nil {
					resp.Answer = append(resp.Answer, sig)
				}
			}
		}
		resp.Rcode = dns.RcodeSuccess
		return resp
	}

	// Check if querying DNSKEY
	if qtype == dns.TypeDNSKEY && qname == zoneName && cz.DNSSECEnabled {
		var keys []dns.RR
		if cz.KSK != nil {
			keys = append(keys, cz.KSK)
		}
		if cz.ZSK != nil {
			keys = append(keys, cz.ZSK)
		}
		if len(keys) > 0 {
			resp.Answer = append(resp.Answer, keys...)
			if doDNSSEC && cz.KSK != nil {
				if sig, err := SignRRSet(keys, cz.KSK, cz.KSKPriv, zoneName); err == nil {
					resp.Answer = append(resp.Answer, sig)
				}
			}
		}
		resp.Rcode = dns.RcodeSuccess
		return resp
	}

	// Match records
	targetMatches, hasAnyRecordForName := collectTargetMatches(cz, qname, zoneName, qtype)

	if len(targetMatches) == 0 {
		if hasAnyRecordForName {
			// NODATA response
			resp.Rcode = dns.RcodeSuccess
			if cz.SOA != nil {
				resp.Ns = append(resp.Ns, cz.SOA)
			}
			return resp
		}

		// NXDOMAIN response
		resp.Rcode = dns.RcodeNameError
		if cz.SOA != nil {
			resp.Ns = append(resp.Ns, cz.SOA)
			if doDNSSEC && cz.DNSSECEnabled && cz.ZSK != nil {
				if sig, err := SignRRSet([]dns.RR{cz.SOA}, cz.ZSK, cz.ZSKPriv, zoneName); err == nil {
					resp.Ns = append(resp.Ns, sig)
				}
				// Attach NSEC for authenticated denial of existence
				nsec := BuildNSEC(qname, "\\000."+zoneName, []uint16{dns.TypeNSEC, dns.TypeRRSIG})
				resp.Ns = append(resp.Ns, nsec)
				if nsecSig, err := SignRRSet([]dns.RR{nsec}, cz.ZSK, cz.ZSKPriv, zoneName); err == nil {
					resp.Ns = append(resp.Ns, nsecSig)
				}
			}
		}
		return resp
	}

	// Line Routing & Failover filtering
	selectedRRs := filterByLineAndHealth(targetMatches, clientLoc)
	if len(selectedRRs) == 0 {
		// 分线筛选无结果时，退回到所有仍可应答的记录（含已切换的备用源站）。
		for _, m := range targetMatches {
			if rr := m.effectiveRR(); rr != nil {
				selectedRRs = append(selectedRRs, rr)
			}
		}
		if len(selectedRRs) == 0 {
			// 全部源站都已失效且无备用：仍返回原记录，避免整个域名解析中断。
			for _, m := range targetMatches {
				selectedRRs = append(selectedRRs, m.RR)
			}
		}
	}

	// Attach Answers
	resp.Rcode = dns.RcodeSuccess
	resp.Answer = append(resp.Answer, selectedRRs...)

	// Attach Authority NS
	for _, cr := range cz.Records {
		if cr.RR.Header().Rrtype == dns.TypeNS && strings.ToLower(cr.RR.Header().Name) == zoneName {
			resp.Ns = append(resp.Ns, cr.RR)
		}
	}

	// DNSSEC RRSIG signing
	if doDNSSEC && cz.DNSSECEnabled && cz.ZSK != nil && len(resp.Answer) > 0 {
		if sig, err := SignRRSet(resp.Answer, cz.ZSK, cz.ZSKPriv, zoneName); err == nil {
			resp.Answer = append(resp.Answer, sig)
		}
	}

	return resp
}

// collectTargetMatches 按 DNS 语义筛选出与查询名称、类型相符的记录集合。
// 优先级：精确名称匹配 > CNAME 跟随 > 通配符匹配。
// 第二个返回值表示该名称下是否存在任意类型的记录，用于区分 NODATA 与 NXDOMAIN。
// 真实解析与路由检测器共用此函数，确保两者对"哪些记录参与竞争"的判定完全一致。
func collectTargetMatches(cz *CachedZone, qname, zoneName string, qtype uint16) ([]CachedRecord, bool) {
	var exactMatches []CachedRecord
	var wildcardMatches []CachedRecord
	var cnameMatches []CachedRecord
	var hasAnyRecordForName bool

	wildcardName := "*." + zoneName
	if qname != zoneName {
		subLabels := dns.SplitDomainName(qname)
		zoneLabels := dns.SplitDomainName(zoneName)
		if len(subLabels) > len(zoneLabels) {
			wildcardName = "*." + strings.Join(subLabels[1:], ".") + "."
		}
	}

	for _, cr := range cz.Records {
		rrName := strings.ToLower(cr.RR.Header().Name)

		isExact := rrName == qname
		isWildcard := rrName == wildcardName

		if isExact {
			hasAnyRecordForName = true
			if cr.RR.Header().Rrtype == dns.TypeCNAME && qtype != dns.TypeCNAME {
				cnameMatches = append(cnameMatches, cr)
			} else if qtype == dns.TypeANY || cr.RR.Header().Rrtype == qtype {
				exactMatches = append(exactMatches, cr)
			}
		} else if isWildcard {
			if qtype == dns.TypeANY || cr.RR.Header().Rrtype == qtype {
				wildcardMatches = append(wildcardMatches, cr)
			}
		}
	}

	targetMatches := exactMatches
	if len(targetMatches) == 0 && len(cnameMatches) > 0 {
		targetMatches = cnameMatches
	}
	if len(targetMatches) == 0 && len(wildcardMatches) > 0 {
		targetMatches = wildcardMatches
	}

	return targetMatches, hasAnyRecordForName
}

// selectLinePool 按智能分线策略挑选候选记录池，并返回命中的分线标签。
// 优先级：精确命中客户端线路的记录 > 默认线路记录。
// 只要存在可用的精确分线记录，默认线路记录就不参与应答，保证分线解析的确定性。
// 记录健康与容灾状态通过 effectiveRR 生效：不健康且无备用的记录不进入候选池。
func selectLinePool(matches []CachedRecord, loc geo.LocationInfo) ([]CachedRecord, string) {
	var specificLine []CachedRecord
	var defaultLine []CachedRecord

	for _, m := range matches {
		// 剔除已探测失败且无备用 IP 的记录（容灾摘除）。
		if m.effectiveRR() == nil {
			continue
		}
		if isDefaultGeoLine(m.Record.GeoLine) {
			defaultLine = append(defaultLine, m)
			continue
		}
		if lineMatchesClient(m.Record.GeoLine, loc) {
			specificLine = append(specificLine, m)
		}
	}

	if len(specificLine) > 0 {
		return specificLine, string(loc.Line)
	}
	return defaultLine, "default"
}

// filterByLineAndHealth selects RRs matching line and healthy state
func filterByLineAndHealth(matches []CachedRecord, loc geo.LocationInfo) []dns.RR {
	pool, _ := selectLinePool(matches, loc)
	if len(pool) == 0 {
		return nil
	}

	// Address pools return one endpoint selected according to the configured
	// weights. Other RRsets (notably NS and MX) remain complete because clients
	// need the full set for protocol-level priority and redundancy semantics.
	if len(pool) > 1 && isWeightedAddressPool(pool) {
		return []dns.RR{pool[weightedIndex(pool, rand.IntN(totalWeight(pool)))].effectiveRR()}
	}

	rrs := make([]dns.RR, 0, len(pool))
	for _, item := range pool {
		rrs = append(rrs, item.effectiveRR())
	}
	return rrs
}

// RoutingCandidate 描述一条参与智能分线竞争的记录及其判定结果。
type RoutingCandidate struct {
	Record         model.Record `json:"record"`
	Selected       bool         `json:"selected"`         // 是否进入最终应答
	EffectiveValue string       `json:"effective_value"`  // 实际应答值，容灾切换时为备用 IP
	FailoverActive bool         `json:"failover_active"`  // 该记录当前是否处于容灾切换状态
	Healthy        bool         `json:"healthy"`          // 源站探测健康状态
	Weight         int          `json:"weight"`           // 生效权重
	SharePercent   float64      `json:"share_percent"`    // 加权轮询下预期承接的流量占比
	Reason         string       `json:"reason"`           // 入选或淘汰原因
}

// RoutingDecision 描述一次完整的智能分线路由决策，供路由检测器透明展示。
type RoutingDecision struct {
	Zone          string             `json:"zone"`
	QueryName     string             `json:"query_name"`
	QueryType     string             `json:"query_type"`
	ClientLine    string             `json:"client_line"`     // 客户端 IP 被判定的线路
	MatchedLine   string             `json:"matched_line"`    // 最终生效的分线
	SelectionMode string             `json:"selection_mode"`  // weighted / all / none
	Candidates    []RoutingCandidate `json:"candidates"`
	TotalMatched  int                `json:"total_matched"`
	Found         bool               `json:"found"`
}

// ExplainRouting 复用真实解析的匹配与分线逻辑，还原一次查询的完整决策过程。
// 与 Query 共用 collectTargetMatches / selectLinePool，因此检测结果与线上解析一致。
func (zs *ZoneStore) ExplainRouting(qname string, qtype uint16, clientIP net.IP) *RoutingDecision {
	qname = strings.ToLower(EnsureFQDN(qname))
	cz, zoneName := zs.FindZone(qname)

	qtypeName := dns.TypeToString[qtype]
	if qtype == dns.TypeANY {
		qtypeName = "ANY"
	}

	clientLoc := geo.MatchLocation(clientIP)
	decision := &RoutingDecision{
		Zone:          strings.TrimSuffix(zoneName, "."),
		QueryName:     strings.TrimSuffix(qname, "."),
		QueryType:     qtypeName,
		ClientLine:    string(clientLoc.Line),
		MatchedLine:   string(clientLoc.Line),
		SelectionMode: "none",
		Candidates:    []RoutingCandidate{},
	}

	if cz == nil {
		return decision
	}
	decision.Found = true

	targetMatches, _ := collectTargetMatches(cz, qname, zoneName, qtype)
	decision.TotalMatched = len(targetMatches)
	if len(targetMatches) == 0 {
		return decision
	}

	pool, matchedLine := selectLinePool(targetMatches, clientLoc)
	decision.MatchedLine = matchedLine

	// 复刻 filterByLineAndHealth 的选择模式：地址池按权重单选，其余 RRset 全量返回。
	weighted := len(pool) > 1 && isWeightedAddressPool(pool)
	if len(pool) == 0 {
		decision.SelectionMode = "none"
	} else if weighted {
		decision.SelectionMode = "weighted"
	} else {
		decision.SelectionMode = "all"
	}

	inPool := make(map[uint]bool, len(pool))
	for _, item := range pool {
		inPool[item.Record.ID] = true
	}
	poolTotalWeight := 0
	if weighted {
		poolTotalWeight = totalWeight(pool)
	}

	for _, m := range targetMatches {
		effective := m.effectiveRR()
		candidate := RoutingCandidate{
			Record:  m.Record,
			Healthy: m.IsHealthy,
			Weight:  int(m.Record.Weight),
		}
		if candidate.Weight <= 0 {
			candidate.Weight = 1
		}
		candidate.EffectiveValue = m.Record.Value
		if effective != nil {
			candidate.EffectiveValue = rrValue(effective)
		}
		candidate.FailoverActive = !m.IsHealthy && m.FailoverRR != nil

		switch {
		case effective == nil:
			candidate.Reason = "origin_down_no_fallback"
		case !inPool[m.Record.ID]:
			candidate.Reason = "line_not_matched"
		default:
			candidate.Selected = true
			if candidate.FailoverActive {
				candidate.Reason = "selected_failover"
			} else if weighted {
				candidate.Reason = "selected_weighted"
			} else {
				candidate.Reason = "selected"
			}
			if weighted && poolTotalWeight > 0 {
				candidate.SharePercent = float64(candidate.Weight) / float64(poolTotalWeight) * 100
			} else {
				candidate.SharePercent = 100
			}
		}

		decision.Candidates = append(decision.Candidates, candidate)
	}

	return decision
}

// rrValue 提取资源记录的应答数据部分，用于展示实际生效的解析目标。
func rrValue(rr dns.RR) string {
	switch v := rr.(type) {
	case *dns.A:
		return v.A.String()
	case *dns.AAAA:
		return v.AAAA.String()
	case *dns.CNAME:
		return v.Target
	default:
		fields := strings.Fields(rr.String())
		if len(fields) > 4 {
			return strings.Join(fields[4:], " ")
		}
		return rr.String()
	}
}

func isWeightedAddressPool(pool []CachedRecord) bool {
	for _, item := range pool {
		rr := item.effectiveRR()
		if rr == nil {
			return false
		}
		t := rr.Header().Rrtype
		if t != dns.TypeA && t != dns.TypeAAAA {
			return false
		}
	}
	return true
}

func totalWeight(pool []CachedRecord) int {
	total := 0
	for _, item := range pool {
		weight := int(item.Record.Weight)
		if weight <= 0 {
			weight = 1
		}
		total += weight
	}
	return total
}

func weightedIndex(pool []CachedRecord, pick int) int {
	for i, item := range pool {
		weight := int(item.Record.Weight)
		if weight <= 0 {
			weight = 1
		}
		if pick < weight {
			return i
		}
		pick -= weight
	}
	return len(pool) - 1
}

func (zs *ZoneStore) GetTotalStats() (uint64, uint64) {
	return atomic.LoadUint64(&zs.totalQ), atomic.LoadUint64(&zs.blockQ)
}
