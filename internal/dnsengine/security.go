package dnsengine

import (
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"dnscat/internal/geo"
	"dnscat/internal/model"
)

// SecurityAction 是安全引擎对一次查询的处置动作。
type SecurityAction int

const (
	SecActionAllow      SecurityAction = iota // 放行
	SecActionRefuse                           // 返回 REFUSED
	SecActionDrop                             // 直接丢弃，不产生任何应答（抗放大）
	SecActionTruncate                         // 返回 TC=1，迫使正常解析器改走 TCP
	SecActionMinimalANY                       // RFC 8482：ANY 查询回极简 HINFO
)

// 规则标识，同时用于统计计数与事件日志。
const (
	SecRuleRateLimit = "rate_limit"
	SecRuleFlood     = "flood"
	SecRuleRRL       = "rrl"
	SecRuleBlacklist = "blacklist"
	SecRuleACL       = "acl"
	SecRuleAXFR      = "axfr"
	SecRuleQType     = "qtype"
	SecRuleANY       = "any"
	SecRuleZoneQPS   = "zone_qps"
	SecRuleNXAbuse   = "nx_abuse"
)

// SecurityDecision 描述一次安全判定的结果。
type SecurityDecision struct {
	Action SecurityAction
	Rule   string
	Reason string
}

// Blocked 表示该判定是否属于拦截（用于统计与日志）。
func (d SecurityDecision) Blocked() bool {
	return d.Action == SecActionRefuse || d.Action == SecActionDrop || d.Action == SecActionTruncate
}

var secAllow = SecurityDecision{Action: SecActionAllow}

// SecurityEvent 是一条安全事件（被拦截的查询，或开启查询日志后的普通查询）。
type SecurityEvent struct {
	At       time.Time `json:"at"`
	ClientIP string    `json:"client_ip"`
	Country  string    `json:"country"`
	ASN      uint      `json:"asn"`
	QName    string    `json:"qname"`
	QType    string    `json:"qtype"`
	Rule     string    `json:"rule"`
	Action   string    `json:"action"`
	Blocked  bool      `json:"blocked"`
}

// -------------------------------------------------------------------------
// 策略编译：把数据库里的文本配置预编译成可高效匹配的结构
// -------------------------------------------------------------------------

// SecurityRuntime 是某区域策略的编译产物，挂在 CachedZone 上，
// 解析热路径只做 O(1)/O(小 N) 的匹配，不再解析字符串。
type SecurityRuntime struct {
	Policy model.DomainSecurityPolicy

	blockedNets []*net.IPNet
	allowedNets []*net.IPNet
	axfrNets    []*net.IPNet
	aclNets     []*net.IPNet

	blockedASNs      map[uint]struct{}
	blockedCountries map[string]struct{}
	blockedQTypes    map[uint16]struct{}
}

// legacyQTypeCodes 补齐 miekg/dns StringToType 未收录的历史查询类型（RFC 1035 等）。
// 这些类型早已废弃，现实中出现基本意味着扫描或放大攻击，因此要支持按名字拦截。
var legacyQTypeCodes = map[string]uint16{
	"WKS":  11,
	"NXT":  30,
	"GPOS": 27,
	"A6":   38,
}

// splitList 按逗号/分号/空白/换行切分配置文本。
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseNetList 把 IP / CIDR 列表解析为网段集合；单个 IP 视为 /32 或 /128。
func parseNetList(raw string) []*net.IPNet {
	items := splitList(raw)
	nets := make([]*net.IPNet, 0, len(items))
	for _, item := range items {
		if strings.Contains(item, "/") {
			if _, n, err := net.ParseCIDR(item); err == nil && n != nil {
				nets = append(nets, n)
			}
			continue
		}
		ip := net.ParseIP(item)
		if ip == nil {
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		} else {
			ip = ip.To4()
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	if ip == nil || len(nets) == 0 {
		return false
	}
	for _, n := range nets {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// CompileSecurityRuntime 由策略生成运行时结构；policy 为 nil 时返回 nil（表示不启用）。
func CompileSecurityRuntime(policy *model.DomainSecurityPolicy) *SecurityRuntime {
	if policy == nil {
		return nil
	}
	rt := &SecurityRuntime{
		Policy:           *policy,
		blockedNets:      parseNetList(policy.BlockedIPs),
		allowedNets:      parseNetList(policy.AllowedIPs),
		axfrNets:         parseNetList(policy.AXFRAllowedIPs),
		aclNets:          parseNetList(policy.ACLAllowedIPs),
		blockedASNs:      make(map[uint]struct{}),
		blockedCountries: make(map[string]struct{}),
		blockedQTypes:    make(map[uint16]struct{}),
	}

	for _, a := range splitList(policy.BlockedASNs) {
		a = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(a)), "AS")
		if n, err := strconv.ParseUint(a, 10, 32); err == nil && n > 0 {
			rt.blockedASNs[uint(n)] = struct{}{}
		}
	}
	for _, c := range splitList(policy.BlockedCountries) {
		rt.blockedCountries[strings.ToLower(c)] = struct{}{}
	}
	for _, name := range splitList(policy.BlockedQTypes) {
		upper := strings.ToUpper(name)
		if qt, ok := dns.StringToType[upper]; ok {
			rt.blockedQTypes[qt] = struct{}{}
			continue
		}
		// miekg/dns 的 StringToType 未收录部分历史类型（如 WKS），
		// 用兜底表补上，否则这些名字会被静默忽略、规则形同虚设。
		if qt, ok := legacyQTypeCodes[upper]; ok {
			rt.blockedQTypes[qt] = struct{}{}
			continue
		}
		// 允许直接写数字类型码
		if n, err := strconv.ParseUint(name, 10, 16); err == nil {
			rt.blockedQTypes[uint16(n)] = struct{}{}
		}
	}
	return rt
}

// -------------------------------------------------------------------------
// 限速原语
// -------------------------------------------------------------------------

type tokenBucket struct {
	tokens  float64
	last    time.Time
	dropped uint64
}

// allow 以令牌桶判定是否放行；rate 为每秒补充速率，burst 为桶容量。
func (b *tokenBucket) allow(now time.Time, rate, burst float64) bool {
	if rate <= 0 {
		return true
	}
	if burst < rate {
		burst = rate
	}
	if b.last.IsZero() {
		b.tokens = burst
		b.last = now
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		b.dropped = 0
		return true
	}
	b.dropped++
	return false
}

// windowCounter 是固定窗口计数器，用于 Flood 与 NXDOMAIN 频次统计。
type windowCounter struct {
	start time.Time
	count int
}

// hit 在窗口内累加并返回当前计数；跨窗口自动重置。
func (w *windowCounter) hit(now time.Time, window time.Duration) int {
	if w.start.IsZero() || now.Sub(w.start) >= window {
		w.start = now
		w.count = 0
	}
	w.count++
	return w.count
}

// clientState 是「某区域 + 某客户端 IP」的全部限速与封禁状态。
type clientState struct {
	rate     tokenBucket
	rrl      tokenBucket
	flood    windowCounter
	nx       windowCounter
	banUntil time.Time
	banRule  string
	lastSeen time.Time
}

type zoneState struct {
	qps      tokenBucket
	lastSeen time.Time
}

// -------------------------------------------------------------------------
// 安全引擎
// -------------------------------------------------------------------------

// securityEventCapFallback 是策略未设置保留上限时的兜底值。
// 实际上限由每个域名的 DomainSecurityPolicy.LogRetentionLimit 决定。
const securityEventCapFallback = model.DefaultLogRetentionLimit

// SecurityEnforcer 是全局安全引擎，持有限速状态、封禁表、拦截计数与事件环。
type SecurityEnforcer struct {
	mu      sync.Mutex
	clients map[string]*clientState
	zones   map[string]*zoneState

	stats  map[uint]*model.DomainSecurityStat
	dirty  map[uint]struct{}
	events map[uint][]SecurityEvent
	// pending 是自上次刷盘以来新增的事件，供持久化层批量写入 Redis。
	// 解析热路径只往内存追加，绝不在锁内做网络 IO；上界与保留上限一致，
	// 因此 Redis 长期不可用时这里最多堆积一个上限的量，不会无界增长。
	pending map[uint][]SecurityEvent
	// limits 记录各域名最近一次生效的保留上限，供持久化层做 LTRIM。
	// 上限本身来自各域名的安全策略，这里只是把它带到刷盘时刻。
	limits map[uint]int
}

// GlobalSecurity 是进程内唯一的安全引擎实例。
var GlobalSecurity = NewSecurityEnforcer()

func NewSecurityEnforcer() *SecurityEnforcer {
	e := &SecurityEnforcer{
		clients: make(map[string]*clientState),
		zones:   make(map[string]*zoneState),
		stats:   make(map[uint]*model.DomainSecurityStat),
		dirty:   make(map[uint]struct{}),
		events:  make(map[uint][]SecurityEvent),
		pending: make(map[uint][]SecurityEvent),
		limits:  make(map[uint]int),
	}
	go e.gcWorker()
	return e
}

// gcWorker 定期回收长期无流量的客户端 / 区域状态，避免内存无界增长。
func (e *SecurityEnforcer) gcWorker() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		e.mu.Lock()
		for k, st := range e.clients {
			// 仍在封禁期内的条目必须保留，否则封禁会被 GC 提前解除。
			if now.After(st.banUntil) && now.Sub(st.lastSeen) > 10*time.Minute {
				delete(e.clients, k)
			}
		}
		for k, st := range e.zones {
			if now.Sub(st.lastSeen) > 10*time.Minute {
				delete(e.zones, k)
			}
		}
		e.mu.Unlock()
	}
}

// statLocked 取（或建）某域名的计数器，调用方需持锁。
func (e *SecurityEnforcer) statLocked(domainID uint) *model.DomainSecurityStat {
	st, ok := e.stats[domainID]
	if !ok {
		st = &model.DomainSecurityStat{DomainID: domainID}
		e.stats[domainID] = st
	}
	e.dirty[domainID] = struct{}{}
	return st
}

// countBlockLocked 按规则累加拦截计数，调用方需持锁。
func (e *SecurityEnforcer) countBlockLocked(domainID uint, rule string) {
	st := e.statLocked(domainID)
	st.BlockedTotal++
	switch rule {
	case SecRuleRateLimit:
		st.BlockedRateLimit++
	case SecRuleFlood:
		st.BlockedFlood++
	case SecRuleRRL:
		st.BlockedRRL++
	case SecRuleBlacklist:
		st.BlockedBlacklist++
	case SecRuleACL:
		st.BlockedACL++
	case SecRuleAXFR:
		st.BlockedAXFR++
	case SecRuleQType:
		st.BlockedQType++
	case SecRuleANY:
		st.BlockedANY++
	case SecRuleZoneQPS:
		st.BlockedZoneQPS++
	case SecRuleNXAbuse:
		st.BlockedNXAbuse++
	}
}

func actionName(a SecurityAction) string {
	switch a {
	case SecActionRefuse:
		return "refused"
	case SecActionDrop:
		return "dropped"
	case SecActionTruncate:
		return "truncated"
	case SecActionMinimalANY:
		return "minimal"
	default:
		return "allowed"
	}
}

// appendEventLocked 按 FIFO 写入事件环：写满上限后丢弃最旧的，再追加最新的。
// limit 为该域名策略里的保留条数上限；调用方需持锁。
func (e *SecurityEnforcer) appendEventLocked(domainID uint, ev SecurityEvent, limit int) {
	if limit <= 0 {
		limit = securityEventCapFallback
	}
	e.limits[domainID] = limit

	list := e.events[domainID]
	if len(list) >= limit {
		// 前移覆盖而不是重新切片：复用同一底层数组，既不额外分配，
		// 也不会因反复切片让被丢弃的旧元素一直驻留内存。
		drop := len(list) - limit + 1
		list = append(list[:0], list[drop:]...)
	}
	e.events[domainID] = append(list, ev)

	// 同步进待刷盘队列，交由持久化层异步批量写入 Redis。
	// 这里同样按上限做 FIFO 截断，防止 Redis 不可用时无界堆积。
	pend := e.pending[domainID]
	if len(pend) >= limit {
		drop := len(pend) - limit + 1
		pend = append(pend[:0], pend[drop:]...)
	}
	e.pending[domainID] = append(pend, ev)
}

// DrainPendingEvents 取出并清空全部待持久化事件，按域名分组、时间升序。
// 由持久化层周期性调用；返回 nil 表示这一轮没有新事件。
func (e *SecurityEnforcer) DrainPendingEvents() map[uint][]SecurityEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.pending) == 0 {
		return nil
	}
	out := make(map[uint][]SecurityEvent, len(e.pending))
	for id, list := range e.pending {
		if len(list) == 0 {
			continue
		}
		cp := make([]SecurityEvent, len(list))
		copy(cp, list)
		out[id] = cp
	}
	e.pending = make(map[uint][]SecurityEvent)
	if len(out) == 0 {
		return nil
	}
	return out
}

// EventLimits 返回各域名当前生效的日志保留上限副本，供持久化层裁剪 Redis 列表。
func (e *SecurityEnforcer) EventLimits() map[uint]int {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make(map[uint]int, len(e.limits))
	for id, v := range e.limits {
		out[id] = v
	}
	return out
}

// RestoreEvents 用持久化后端里的历史事件重建某域名的事件环（按时间升序传入）。
// 仅在进程启动时调用：刻意不写 pending，避免刚恢复的事件又被回写一遍。
func (e *SecurityEnforcer) RestoreEvents(domainID uint, evs []SecurityEvent, limit int) {
	if limit <= 0 {
		limit = securityEventCapFallback
	}
	if len(evs) > limit {
		evs = evs[len(evs)-limit:]
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	cp := make([]SecurityEvent, len(evs))
	copy(cp, evs)
	e.events[domainID] = cp
	// 记下上限：这样在该域名产生第一条新事件之前，刷盘也能拿到正确的裁剪边界。
	e.limits[domainID] = limit
}

// TrimEvents 在保留上限被调小后立即裁剪该域名的事件环，
// 使新上限即时生效，而不必等下一条事件写入。
func (e *SecurityEnforcer) TrimEvents(domainID uint, limit int) {
	if limit <= 0 {
		limit = securityEventCapFallback
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	list := e.events[domainID]
	if len(list) > limit {
		drop := len(list) - limit
		e.events[domainID] = append(list[:0], list[drop:]...)
	}
}

// Evaluate 在解析之前对一次查询做安全判定。
// zone 为 nil（查询未命中任何托管区域）时直接放行，交由上层返回 REFUSED。
func (e *SecurityEnforcer) Evaluate(zone *CachedZone, qname string, qtype uint16, clientIP net.IP, isTCP bool) SecurityDecision {
	if zone == nil || zone.Security == nil {
		return secAllow
	}
	rt := zone.Security
	p := &rt.Policy
	if !p.Enabled {
		return secAllow
	}

	domainID := zone.Domain.ID
	zoneKey := strings.ToLower(zone.Domain.Name)
	ipStr := ""
	if clientIP != nil {
		ipStr = clientIP.String()
	}

	// 白名单最高优先：命中即完全跳过后续限制。
	if ipInNets(clientIP, rt.allowedNets) {
		return secAllow
	}

	now := time.Now()

	// 需要地理/ASN 信息的规则才做定位，避免无谓开销。
	var loc geo.LocationInfo
	locResolved := false
	resolveLoc := func() geo.LocationInfo {
		if !locResolved && clientIP != nil {
			loc = geo.MatchLocation(clientIP)
			locResolved = true
		}
		return loc
	}

	// 统一的拦截出口：累加计数并记录事件。
	block := func(action SecurityAction, rule, reason string) SecurityDecision {
		dec := SecurityDecision{Action: action, Rule: rule, Reason: reason}
		e.mu.Lock()
		e.countBlockLocked(domainID, rule)
		if p.AttackLogEnabled {
			l := resolveLoc()
			e.appendEventLocked(domainID, SecurityEvent{
				At:       now,
				ClientIP: ipStr,
				Country:  l.CountryCode,
				ASN:      l.ASN,
				QName:    qname,
				QType:    qtypeName(qtype),
				Rule:     rule,
				Action:   actionName(action),
				Blocked:  true,
			}, p.LogRetentionLimit)
		}
		e.mu.Unlock()
		return dec
	}

	// 1) IP / ASN / 地区黑名单——整条处理链路里优先级最高的拦截规则。
	// 必须排在所有其他判定之前，且整个 Evaluate 又运行在区域查找与智能分线解析之前，
	// 因此命中黑名单的来源根本不会进入路由选路与应答构造，一律直接拒绝。
	// 唯一能越过它的是显式配置的放行白名单（上面已先行返回），那是管理员的逃生通道。
	if p.BlacklistEnabled {
		if ipInNets(clientIP, rt.blockedNets) {
			return block(SecActionRefuse, SecRuleBlacklist, "client IP blacklisted")
		}
		if len(rt.blockedCountries) > 0 || len(rt.blockedASNs) > 0 {
			l := resolveLoc()
			if len(rt.blockedCountries) > 0 {
				if _, bad := rt.blockedCountries[strings.ToLower(l.CountryCode)]; bad {
					return block(SecActionRefuse, SecRuleBlacklist, "country blacklisted")
				}
				if _, bad := rt.blockedCountries[strings.ToLower(l.Continent)]; bad {
					return block(SecActionRefuse, SecRuleBlacklist, "continent blacklisted")
				}
			}
			if l.ASN != 0 {
				if _, bad := rt.blockedASNs[l.ASN]; bad {
					return block(SecActionRefuse, SecRuleBlacklist, "ASN blacklisted")
				}
			}
		}
	}

	// 2) 仍在封禁期内：直接丢弃。
	e.mu.Lock()
	cs := e.clients[zoneKey+"|"+ipStr]
	banned := false
	banRule := ""
	if cs != nil && now.Before(cs.banUntil) {
		banned = true
		banRule = cs.banRule
	}
	e.mu.Unlock()
	if banned {
		if banRule == "" {
			banRule = SecRuleFlood
		}
		return block(SecActionDrop, banRule, "client temporarily banned")
	}

	// 3) Resolver / IP ACL：只允许白名单地址查询本区域。
	if p.ACLEnabled && len(rt.aclNets) > 0 && !ipInNets(clientIP, rt.aclNets) {
		return block(SecActionRefuse, SecRuleACL, "client not in resolver ACL")
	}

	// 4) AXFR / IXFR 区域传送限制。
	if qtype == dns.TypeAXFR || qtype == dns.TypeIXFR {
		if p.AXFRPolicy == model.AXFRPolicyAllowlist && ipInNets(clientIP, rt.axfrNets) {
			return secAllow
		}
		return block(SecActionRefuse, SecRuleAXFR, "zone transfer denied")
	}

	// 5) 异常 QTYPE 防护。
	if p.QTypeFilterEnabled {
		if _, bad := rt.blockedQTypes[qtype]; bad {
			return block(SecActionRefuse, SecRuleQType, "query type blocked")
		}
	}

	// 6) ANY 限制（RFC 8482 极简应答 / 直接拒绝）。
	if qtype == dns.TypeANY {
		switch p.AnyPolicy {
		case model.AnyPolicyRefuse:
			return block(SecActionRefuse, SecRuleANY, "ANY query refused")
		case model.AnyPolicyMinimal:
			// 极简应答不算拦截流量，但要计数以便观察 ANY 滥用情况。
			e.mu.Lock()
			e.countBlockLocked(domainID, SecRuleANY)
			e.mu.Unlock()
			return SecurityDecision{Action: SecActionMinimalANY, Rule: SecRuleANY, Reason: "RFC 8482 minimal ANY"}
		}
	}

	// 7) 单域名（整区）QPS 上限。
	if p.ZoneQPSLimitEnabled && p.ZoneQPSLimit > 0 {
		e.mu.Lock()
		zs := e.zones[zoneKey]
		if zs == nil {
			zs = &zoneState{}
			e.zones[zoneKey] = zs
		}
		zs.lastSeen = now
		limit := float64(p.ZoneQPSLimit)
		ok := zs.qps.allow(now, limit, limit*2)
		e.mu.Unlock()
		if !ok {
			return block(SecActionDrop, SecRuleZoneQPS, "zone QPS limit exceeded")
		}
	}

	// 以下三项按「客户端 IP + 区域」维护状态。
	e.mu.Lock()
	key := zoneKey + "|" + ipStr
	cs = e.clients[key]
	if cs == nil {
		cs = &clientState{}
		e.clients[key] = cs
	}
	cs.lastSeen = now

	// 8) DNS Flood / DDoS：1 秒窗口内超过阈值即临时封禁。
	floodTripped := false
	if p.FloodProtectionEnabled && p.FloodThresholdQPS > 0 {
		if cs.flood.hit(now, time.Second) > p.FloodThresholdQPS {
			ban := p.FloodBanSeconds
			if ban <= 0 {
				ban = 300
			}
			cs.banUntil = now.Add(time.Duration(ban) * time.Second)
			cs.banRule = SecRuleFlood
			floodTripped = true
		}
	}

	// 9) 查询速率限制（令牌桶）。
	rateExceeded := false
	rateSlip := false
	if !floodTripped && p.RateLimitEnabled && p.RateLimitQPS > 0 {
		burst := float64(p.RateLimitBurst)
		if burst <= 0 {
			burst = float64(p.RateLimitQPS) * 2
		}
		if !cs.rate.allow(now, float64(p.RateLimitQPS), burst) {
			rateExceeded = true
			// SLIP：每 N 个被限的包回一个 TC=1，让正常解析器退回 TCP。
			slip := p.RRLSlipRatio
			if slip > 0 && cs.rate.dropped%uint64(slip) == 0 {
				rateSlip = true
			}
		}
	}

	// 10) RRL：限制同一客户端的应答发送速率（仅对 UDP 有放大意义）。
	rrlExceeded := false
	rrlSlip := false
	if !floodTripped && !rateExceeded && !isTCP && p.RRLEnabled && p.RRLResponsesPerSec > 0 {
		rps := float64(p.RRLResponsesPerSec)
		if !cs.rrl.allow(now, rps, rps*2) {
			rrlExceeded = true
			slip := p.RRLSlipRatio
			if slip > 0 && cs.rrl.dropped%uint64(slip) == 0 {
				rrlSlip = true
			}
		}
	}
	e.mu.Unlock()

	if floodTripped {
		return block(SecActionDrop, SecRuleFlood, "flood threshold exceeded, client banned")
	}
	if rateExceeded {
		if isTCP {
			return block(SecActionRefuse, SecRuleRateLimit, "query rate limit exceeded")
		}
		if rateSlip {
			return block(SecActionTruncate, SecRuleRateLimit, "query rate limit exceeded (slip)")
		}
		return block(SecActionDrop, SecRuleRateLimit, "query rate limit exceeded")
	}
	if rrlExceeded {
		if rrlSlip {
			return block(SecActionTruncate, SecRuleRRL, "response rate limit exceeded (slip)")
		}
		return block(SecActionDrop, SecRuleRRL, "response rate limit exceeded")
	}

	return secAllow
}

// RecordOutcome 在解析完成后记录结果：累计总查询、按需写查询日志，
// 并做 NXDOMAIN / 随机子域攻击的频次判定与封禁。
func (e *SecurityEnforcer) RecordOutcome(zone *CachedZone, qname string, qtype uint16, clientIP net.IP, rcode int) {
	if zone == nil || zone.Security == nil {
		return
	}
	p := &zone.Security.Policy
	if !p.Enabled {
		return
	}

	now := time.Now()
	zoneKey := strings.ToLower(zone.Domain.Name)
	ipStr := ""
	if clientIP != nil {
		ipStr = clientIP.String()
	}
	domainID := zone.Domain.ID
	isNX := rcode == dns.RcodeNameError

	e.mu.Lock()
	st := e.statLocked(domainID)
	st.TotalQueries++
	if isNX {
		st.NXDomainCount++
	}

	banned := false
	if isNX && p.NXProtectionEnabled && p.NXThresholdPerMin > 0 {
		key := zoneKey + "|" + ipStr
		cs := e.clients[key]
		if cs == nil {
			cs = &clientState{}
			e.clients[key] = cs
		}
		cs.lastSeen = now
		if cs.nx.hit(now, time.Minute) > p.NXThresholdPerMin {
			ban := p.NXBanSeconds
			if ban <= 0 {
				ban = 300
			}
			cs.banUntil = now.Add(time.Duration(ban) * time.Second)
			cs.banRule = SecRuleNXAbuse
			banned = true
			st.BlockedTotal++
			st.BlockedNXAbuse++
		}
	}

	// 查询日志：开启后记录全部查询（含放行），高流量场景慎用。
	if p.QueryLogEnabled {
		e.appendEventLocked(domainID, SecurityEvent{
			At:       now,
			ClientIP: ipStr,
			QName:    qname,
			QType:    qtypeName(qtype),
			Rule:     "query",
			Action:   dns.RcodeToString[rcode],
			Blocked:  false,
		}, p.LogRetentionLimit)
	}
	if banned && p.AttackLogEnabled {
		e.appendEventLocked(domainID, SecurityEvent{
			At:       now,
			ClientIP: ipStr,
			QName:    qname,
			QType:    qtypeName(qtype),
			Rule:     SecRuleNXAbuse,
			Action:   "banned",
			Blocked:  true,
		}, p.LogRetentionLimit)
	}
	e.mu.Unlock()
}

// Stat 返回某域名的拦截统计快照。
func (e *SecurityEnforcer) Stat(domainID uint) model.DomainSecurityStat {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok := e.stats[domainID]; ok {
		return *st
	}
	return model.DomainSecurityStat{DomainID: domainID}
}

// EventFilter 描述安全日志的查询条件。保留上限可达上万条，
// 因此筛选与分页都在服务端完成，避免把全量事件塞给浏览器。
type EventFilter struct {
	Rule    string // 规则标识，空表示不限
	Outcome string // "blocked" / "allowed"，空表示不限
	Search  string // 匹配来源 IP / 查询名 / 类型 / 国家码 / ASN
	Offset  int
	Limit   int
}

// matchEvent 判断单条事件是否命中筛选条件。
func matchEvent(ev *SecurityEvent, rule, outcome, kw string) bool {
	if rule != "" && ev.Rule != rule {
		return false
	}
	switch outcome {
	case "blocked":
		if !ev.Blocked {
			return false
		}
	case "allowed":
		if ev.Blocked {
			return false
		}
	}
	if kw == "" {
		return true
	}
	if strings.Contains(strings.ToLower(ev.ClientIP), kw) ||
		strings.Contains(strings.ToLower(ev.QName), kw) ||
		strings.Contains(strings.ToLower(ev.QType), kw) ||
		strings.Contains(strings.ToLower(ev.Country), kw) {
		return true
	}
	if ev.ASN != 0 && strings.Contains("as"+strconv.FormatUint(uint64(ev.ASN), 10), kw) {
		return true
	}
	return false
}

// QueryEvents 按条件倒序（最新在前）分页返回安全日志，
// 同时返回命中总数与该域名当前已保留的事件总条数。
func (e *SecurityEnforcer) QueryEvents(domainID uint, f EventFilter) (items []SecurityEvent, matched int, stored int) {
	rule := strings.TrimSpace(f.Rule)
	outcome := strings.TrimSpace(f.Outcome)
	kw := strings.ToLower(strings.TrimSpace(f.Search))

	e.mu.Lock()
	defer e.mu.Unlock()

	src := e.events[domainID]
	stored = len(src)
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	items = make([]SecurityEvent, 0, f.Limit)
	skipped := 0
	// 从尾部向前遍历即为时间倒序，命中的先跳过 Offset 条再收集 Limit 条。
	for i := len(src) - 1; i >= 0; i-- {
		if !matchEvent(&src[i], rule, outcome, kw) {
			continue
		}
		matched++
		if skipped < f.Offset {
			skipped++
			continue
		}
		if len(items) < f.Limit {
			items = append(items, src[i])
		}
	}
	return items, matched, stored
}

// EventRules 返回该域名事件里实际出现过的规则集合，供前端生成筛选下拉。
func (e *SecurityEnforcer) EventRules(domainID uint) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	seen := make(map[string]struct{})
	for i := range e.events[domainID] {
		if r := e.events[domainID][i].Rule; r != "" {
			seen[r] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// ActiveBans 返回某区域当前仍在封禁期内的客户端数量。
func (e *SecurityEnforcer) ActiveBans(zoneName string) int {
	prefix := strings.ToLower(zoneName) + "|"
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for k, st := range e.clients {
		if strings.HasPrefix(k, prefix) && now.Before(st.banUntil) {
			n++
		}
	}
	return n
}

// ClearBans 解除某区域的全部临时封禁（供控制台「解除封禁」使用）。
func (e *SecurityEnforcer) ClearBans(zoneName string) int {
	prefix := strings.ToLower(zoneName) + "|"
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for k, st := range e.clients {
		if strings.HasPrefix(k, prefix) && !st.banUntil.IsZero() {
			st.banUntil = time.Time{}
			st.banRule = ""
			n++
		}
	}
	return n
}

// ResetStats 清零某域名的统计与事件。
func (e *SecurityEnforcer) ResetStats(domainID uint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stats[domainID] = &model.DomainSecurityStat{DomainID: domainID}
	e.dirty[domainID] = struct{}{}
	delete(e.events, domainID)
	// 一并丢弃待刷盘队列，否则刚清空的事件会被下一轮刷盘重新写回 Redis。
	delete(e.pending, domainID)
}

// LoadStats 用数据库中的历史计数初始化内存统计（进程启动时调用），
// 使攻击统计跨重启累计而不归零。
func (e *SecurityEnforcer) LoadStats(rows []model.DomainSecurityStat) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range rows {
		row := rows[i]
		e.stats[row.DomainID] = &row
	}
}

// DirtyStats 取出自上次调用以来有变化的计数快照，供周期性落库。
func (e *SecurityEnforcer) DirtyStats() []model.DomainSecurityStat {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.dirty) == 0 {
		return nil
	}
	out := make([]model.DomainSecurityStat, 0, len(e.dirty))
	for id := range e.dirty {
		if st, ok := e.stats[id]; ok {
			out = append(out, *st)
		}
		delete(e.dirty, id)
	}
	return out
}

// qtypeName 返回查询类型名，未知类型回退为 TYPExx。
func qtypeName(qtype uint16) string {
	if name, ok := dns.TypeToString[qtype]; ok {
		return name
	}
	return "TYPE" + strconv.Itoa(int(qtype))
}

// BuildMinimalANYResponse 按 RFC 8482 构造 ANY 查询的极简应答：
// 只回一条 HINFO，避免把整个 RRset 集合用于反射放大。
func BuildMinimalANYResponse(req *dns.Msg, zoneName string) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true
	resp.RecursionAvailable = false
	name := req.Question[0].Name
	resp.Answer = []dns.RR{
		&dns.HINFO{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeHINFO,
				Class:  dns.ClassINET,
				Ttl:    3600,
			},
			Cpu: "RFC8482",
			Os:  "",
		},
	}
	return resp
}
