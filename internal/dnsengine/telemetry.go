package dnsengine

import (
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type TelemetryCollector struct {
	mu           sync.RWMutex
	totalQueries uint64
	blockedQ     uint64

	// QPS rolling window (60 seconds)
	qpsWindow    [60]uint64
	windowIndex  int
	lastWindowTs int64

	// 查询处理耗时累计（微秒）与样本数，用原子操作维护，
	// 供节点心跳上报「平均响应延迟」。读取后清零，得到的是相邻两次心跳之间的均值。
	latencySumUs uint64
	latencyCount uint64

	// Type breakdown counts
	typeCounts map[uint16]*uint64

	// Country counts (pure geographic countries and international regions)
	countryCounts map[string]*uint64

	// 24h Hourly buckets (0..23)
	hourlyBuckets [24]uint64
	hourlySuccess [24]uint64
	hourlyBlocked [24]uint64

	// 分钟级环形缓冲，覆盖最近 30 天，服务于可选区间（1h~30d）的趋势查询。
	// 以「分钟纪元」(unix/60) 为逻辑索引，槽位 = epoch % minuteRingSize；
	// 每个槽记录自身代表的 epoch，读取时校验 epoch 以跳过尚未写入或已被覆盖的陈旧槽。
	// 纯内存结构，进程重启后历史趋势清零（当前 QPS 与实时曲线会随新流量迅速重建）。
	minuteRing [minuteRingSize]minuteBucket

	// Per authoritative zone metrics (key: lowercase apex without trailing dot).
	// 只有命中托管区域的查询才会记录，因此基数受托管域名数量约束，
	// 不会被随机域名探测放大。
	domains map[string]*domainMetrics

	// edgeBaselines 保存各边缘节点最近一次上报的累计快照。主控用相邻快照的差值
	// 写入自己的分钟环形缓冲，从而让 1H~30D 的区间统计真正覆盖全部 Anycast
	// 节点，而不是只显示主控进程本地收到的请求。它不参与累计 summary 的计算，
	// 避免与数据库中保存的节点累计快照重复相加。
	edgeBaselines map[string]edgeTelemetryBaseline
}

type edgeTelemetryBaseline struct {
	global  DomainTelemetry
	domains map[string]DomainTelemetry
}

// minuteRingSize 为 30 天的分钟数（60 * 24 * 30），即环形缓冲可回溯的最大跨度。
const minuteRingSize = 60 * 24 * 30

// minuteBucket 是分钟级聚合桶，epoch 记录该桶代表的分钟纪元 (unix/60)。
// types/countries 惰性分配：仅有流量的分钟才建 map，空闲桶保持 nil 以省内存，
// 使区间统计（1h~30d）能按时间桶聚合出查询类型 / 国家分布，而不只是累计总数。
type minuteBucket struct {
	epoch     int64
	queries   uint64
	blocked   uint64
	types     map[uint16]uint64
	countries map[string]uint64
}

// domainMetrics 中的计数器统一由 TelemetryCollector.mu 保护。
type domainMetrics struct {
	queries       uint64
	blocked       uint64
	qpsWindow     [60]uint64
	hourly        [24]uint64
	hourlyBlocked [24]uint64
	types         map[uint16]uint64
	countries     map[string]uint64
	// 域名级分钟环形缓冲，支持按区间(1h~30d)聚合该域名的趋势 / 类型 / 国家分布。
	// 与全局同构；每个有流量的域名约占 1.7MB 基础内存（空闲桶的 map 为 nil，惰性分配）。
	minuteRing [minuteRingSize]minuteBucket
}

// DomainTelemetry 是单个托管区域的遥测快照，同时用作边缘节点心跳的上报结构。
type DomainTelemetry struct {
	Queries       uint64            `json:"queries"`
	Blocked       uint64            `json:"blocked"`
	QPS           int64             `json:"qps"`
	Hourly        [24]uint64        `json:"hourly"`
	HourlyBlocked [24]uint64        `json:"hourly_blocked"`
	Types         map[string]uint64 `json:"types"`
	Countries     map[string]uint64 `json:"countries"`
}

// NormalizeZoneKey 统一区域键：小写并去掉结尾的点。
func NormalizeZoneKey(zone string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
}

var GlobalTelemetry = NewTelemetryCollector()

func NewTelemetryCollector() *TelemetryCollector {
	tc := &TelemetryCollector{
		typeCounts:    make(map[uint16]*uint64),
		countryCounts: make(map[string]*uint64),
		domains:       make(map[string]*domainMetrics),
		edgeBaselines: make(map[string]edgeTelemetryBaseline),
		lastWindowTs:  time.Now().Unix(),
	}

	// Initialize standard types
	stdTypes := []uint16{
		dns.TypeA, dns.TypeAAAA, dns.TypeCNAME, dns.TypeTXT, dns.TypeMX,
		dns.TypeNS, dns.TypeSRV, dns.TypeCAA, dns.TypeHTTPS, dns.TypeDNSKEY,
		dns.TypeDS, dns.TypeSOA, dns.TypePTR,
	}
	for _, t := range stdTypes {
		var c uint64
		tc.typeCounts[t] = &c
	}

	// Initialize pure geographic countries / regions (ISO 3166-1 alpha-2)
	stdCountries := []string{
		"cn", "us", "hk", "jp", "sg", "de", "gb", "kr", "au", "ca", "fr", "ru", "br", "eu", "default",
	}
	for _, c := range stdCountries {
		var cnt uint64
		tc.countryCounts[c] = &cnt
	}

	go tc.windowTicker()
	return tc
}

// RecordLatency 累计一次查询的处理耗时。
func (tc *TelemetryCollector) RecordLatency(d time.Duration) {
	us := d.Microseconds()
	if us < 0 {
		return
	}
	atomic.AddUint64(&tc.latencySumUs, uint64(us))
	atomic.AddUint64(&tc.latencyCount, 1)
}

// GetAvgLatencyMs 返回自上次调用以来的平均查询处理耗时（毫秒，向上取整到 1ms）。
// 读取后清零计数，使每次心跳上报的都是最近一个周期的均值而非全程累计值。
// 无样本时返回 0。
func (tc *TelemetryCollector) GetAvgLatencyMs() int64 {
	sum := atomic.SwapUint64(&tc.latencySumUs, 0)
	count := atomic.SwapUint64(&tc.latencyCount, 0)
	if count == 0 {
		return 0
	}
	avgUs := sum / count
	// 亚毫秒级耗时向上取整为 1ms，避免图表恒为 0 而看不出变化。
	if avgUs > 0 && avgUs < 1000 {
		return 1
	}
	return int64(avgUs / 1000)
}

// RecordQuery 记录一次查询的全局遥测。
func (tc *TelemetryCollector) RecordQuery(qtype uint16, geoLine string, blocked bool) {
	tc.recordQuery("", qtype, geoLine, blocked)
}

// RecordZoneQuery 在记录全局遥测的同时，把查询归属到命中的托管区域。
// zone 为空时行为与 RecordQuery 完全一致。
func (tc *TelemetryCollector) RecordZoneQuery(zone string, qtype uint16, geoLine string, blocked bool) {
	tc.recordQuery(zone, qtype, geoLine, blocked)
}

func (tc *TelemetryCollector) recordQuery(zone string, qtype uint16, geoLine string, blocked bool) {
	atomic.AddUint64(&tc.totalQueries, 1)

	// 先归一化国家/地区码，供分钟桶时序与累计计数共用。
	countryCode := "default"
	cleanLine := strings.ToLower(strings.TrimSpace(geoLine))
	if cleanLine != "" && cleanLine != "default" {
		countryCode = cleanLine
	}

	// Record in current second bucket
	now := time.Now()
	tc.mu.Lock()
	tc.qpsWindow[tc.windowIndex]++
	hr := now.Hour()
	tc.hourlyBuckets[hr]++
	if blocked {
		tc.hourlyBlocked[hr]++
	} else {
		tc.hourlySuccess[hr]++
	}
	// 累加到分钟级环形缓冲（惰性重置：槽位 epoch 不匹配时视为新分钟）。
	// 同时把查询类型与国家分桶写入该分钟，供区间统计按时间聚合。
	epoch := now.Unix() / 60
	mb := &tc.minuteRing[epoch%minuteRingSize]
	if mb.epoch != epoch {
		mb.epoch = epoch
		mb.queries = 0
		mb.blocked = 0
		mb.types = nil
		mb.countries = nil
	}
	mb.queries++
	if blocked {
		mb.blocked++
	}
	if mb.types == nil {
		mb.types = make(map[uint16]uint64, 8)
	}
	mb.types[qtype]++
	if mb.countries == nil {
		mb.countries = make(map[string]uint64, 8)
	}
	mb.countries[countryCode]++
	tc.mu.Unlock()

	if blocked {
		atomic.AddUint64(&tc.blockedQ, 1)
	}

	// Type counter（累计，供"全部时间"视图）
	tc.mu.RLock()
	if ptr, ok := tc.typeCounts[qtype]; ok {
		atomic.AddUint64(ptr, 1)
	} else {
		tc.mu.RUnlock()
		tc.mu.Lock()
		var c uint64 = 1
		tc.typeCounts[qtype] = &c
		tc.mu.Unlock()
		tc.mu.RLock()
	}
	tc.mu.RUnlock()

	// Country counter（累计，供"全部时间"视图）
	tc.mu.RLock()
	if ptr, ok := tc.countryCounts[countryCode]; ok {
		atomic.AddUint64(ptr, 1)
	} else {
		tc.mu.RUnlock()
		tc.mu.Lock()
		var c uint64 = 1
		tc.countryCounts[countryCode] = &c
		tc.mu.Unlock()
		tc.mu.RLock()
	}
	tc.mu.RUnlock()

	tc.recordDomainQuery(zone, qtype, countryCode, blocked)
}

// recordDomainQuery 把查询累加到对应托管区域的计数器上。
func (tc *TelemetryCollector) recordDomainQuery(zone string, qtype uint16, countryCode string, blocked bool) {
	key := NormalizeZoneKey(zone)
	if key == "" {
		return
	}

	now := time.Now()
	hr := now.Hour()

	tc.mu.Lock()
	defer tc.mu.Unlock()

	dm, ok := tc.domains[key]
	if !ok {
		dm = &domainMetrics{
			types:     make(map[uint16]uint64),
			countries: make(map[string]uint64),
		}
		tc.domains[key] = dm
	}

	dm.queries++
	dm.qpsWindow[tc.windowIndex]++
	dm.hourly[hr]++
	if blocked {
		dm.blocked++
		dm.hourlyBlocked[hr]++
	}
	dm.types[qtype]++
	dm.countries[countryCode]++

	// 域名级分钟环形缓冲累加（与全局同构），供该域名的区间统计按时间聚合。
	epoch := now.Unix() / 60
	mb := &dm.minuteRing[epoch%minuteRingSize]
	if mb.epoch != epoch {
		mb.epoch = epoch
		mb.queries = 0
		mb.blocked = 0
		mb.types = nil
		mb.countries = nil
	}
	mb.queries++
	if blocked {
		mb.blocked++
	}
	if mb.types == nil {
		mb.types = make(map[uint16]uint64, 8)
	}
	mb.types[qtype]++
	if mb.countries == nil {
		mb.countries = make(map[string]uint64, 8)
	}
	mb.countries[countryCode]++
}

// snapshotDomain 需在持有 tc.mu 的情况下调用。
func snapshotDomain(dm *domainMetrics) DomainTelemetry {
	snapshot := DomainTelemetry{
		Queries:       dm.queries,
		Blocked:       dm.blocked,
		Hourly:        dm.hourly,
		HourlyBlocked: dm.hourlyBlocked,
		Types:         make(map[string]uint64, len(dm.types)),
		Countries:     make(map[string]uint64, len(dm.countries)),
	}

	var windowSum uint64
	for _, count := range dm.qpsWindow {
		windowSum += count
	}
	snapshot.QPS = int64(windowSum / 60)

	for qtype, count := range dm.types {
		name := dns.TypeToString[qtype]
		if name == "" {
			name = "OTHER"
		}
		snapshot.Types[name] += count
	}
	for code, count := range dm.countries {
		snapshot.Countries[code] += count
	}

	return snapshot
}

// GetDomainTelemetry 返回单个托管区域的本机遥测快照。
func (tc *TelemetryCollector) GetDomainTelemetry(zone string) DomainTelemetry {
	key := NormalizeZoneKey(zone)

	tc.mu.RLock()
	defer tc.mu.RUnlock()

	dm, ok := tc.domains[key]
	if !ok {
		return DomainTelemetry{
			Types:     map[string]uint64{},
			Countries: map[string]uint64{},
		}
	}
	return snapshotDomain(dm)
}

// GetDomainRangeStats 返回某托管区域在选定区间内的趋势点、查询类型分布与国家/地区分布
// （主控本地时序）。区域无流量时返回三个非 nil 空切片，便于 JSON 序列化为空数组。
func (tc *TelemetryCollector) GetDomainRangeStats(zone, rangeKey string) ([]QPSTrendPoint, []map[string]interface{}, []map[string]interface{}) {
	key := NormalizeZoneKey(zone)

	tc.mu.RLock()
	defer tc.mu.RUnlock()

	dm, ok := tc.domains[key]
	if !ok {
		return []QPSTrendPoint{}, []map[string]interface{}{}, []map[string]interface{}{}
	}
	points := buildTrendFromRing(&dm.minuteRing, rangeKey)
	typeCounts, countryCounts := aggregateRing(&dm.minuteRing, rangeKey)
	types := buildTypeBreakdownFromCounts(typeCounts)
	geo := BuildGeoBreakdown(countryCounts)
	return points, types, geo
}

// GetDomainCounts 返回所有有流量的托管区域快照，用于集群心跳传输。
func (tc *TelemetryCollector) GetDomainCounts() map[string]DomainTelemetry {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	res := make(map[string]DomainTelemetry, len(tc.domains))
	for key, dm := range tc.domains {
		if dm.queries == 0 {
			continue
		}
		res[key] = snapshotDomain(dm)
	}
	return res
}

// GetTelemetrySnapshot 返回本节点的全局累计遥测快照，供边缘节点随心跳上报。
// 返回值中的 map 均为独立副本，JSON 编码期间不会与 DNS 查询写入发生竞争。
func (tc *TelemetryCollector) GetTelemetrySnapshot() DomainTelemetry {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	snapshot := DomainTelemetry{
		Queries:       atomic.LoadUint64(&tc.totalQueries),
		Blocked:       atomic.LoadUint64(&tc.blockedQ),
		Hourly:        tc.hourlyBuckets,
		HourlyBlocked: tc.hourlyBlocked,
		Types:         make(map[string]uint64, len(tc.typeCounts)),
		Countries:     make(map[string]uint64, len(tc.countryCounts)),
	}
	for qtype, count := range tc.typeCounts {
		name := dns.TypeToString[qtype]
		if name == "" {
			name = "OTHER"
		}
		snapshot.Types[name] += atomic.LoadUint64(count)
	}
	for code, count := range tc.countryCounts {
		snapshot.Countries[code] = atomic.LoadUint64(count)
	}
	var windowSum uint64
	for _, count := range tc.qpsWindow {
		windowSum += count
	}
	snapshot.QPS = int64(windowSum / 60)
	return snapshot
}

func cloneDomainTelemetry(src DomainTelemetry) DomainTelemetry {
	dst := src
	dst.Types = make(map[string]uint64, len(src.Types))
	for name, count := range src.Types {
		dst.Types[name] = count
	}
	dst.Countries = make(map[string]uint64, len(src.Countries))
	for code, count := range src.Countries {
		dst.Countries[code] = count
	}
	return dst
}

func cloneDomainTelemetryMap(src map[string]DomainTelemetry) map[string]DomainTelemetry {
	dst := make(map[string]DomainTelemetry, len(src))
	for zone, stats := range src {
		dst[NormalizeZoneKey(zone)] = cloneDomainTelemetry(stats)
	}
	return dst
}

func cumulativeDelta(current, previous uint64, reset bool) uint64 {
	if reset || current < previous {
		return current
	}
	return current - previous
}

func telemetryDelta(current, previous DomainTelemetry, reset bool) DomainTelemetry {
	delta := DomainTelemetry{
		Queries:   cumulativeDelta(current.Queries, previous.Queries, reset),
		Blocked:   cumulativeDelta(current.Blocked, previous.Blocked, reset),
		Types:     make(map[string]uint64),
		Countries: make(map[string]uint64),
	}
	for name, count := range current.Types {
		delta.Types[name] = cumulativeDelta(count, previous.Types[name], reset)
	}
	for code, count := range current.Countries {
		delta.Countries[code] = cumulativeDelta(count, previous.Countries[code], reset)
	}
	return delta
}

// mergeTelemetryDeltaIntoRing 把一个边缘节点自上次心跳以来的增量计入当前分钟。
// 心跳间隔仅数秒，因此把差值归入接收时所在分钟可以保留足够准确的区间边界，
// 同时避免每 3 秒传输整整 30 天的明细桶。
func mergeTelemetryDeltaIntoRing(ring *[minuteRingSize]minuteBucket, delta DomainTelemetry, epoch int64) {
	if delta.Queries == 0 && delta.Blocked == 0 && len(delta.Types) == 0 && len(delta.Countries) == 0 {
		return
	}
	slot := ((epoch % minuteRingSize) + minuteRingSize) % minuteRingSize
	mb := &ring[slot]
	if mb.epoch != epoch {
		mb.epoch = epoch
		mb.queries = 0
		mb.blocked = 0
		mb.types = nil
		mb.countries = nil
	}
	mb.queries += delta.Queries
	mb.blocked += delta.Blocked
	for name, count := range delta.Types {
		if count == 0 {
			continue
		}
		cleanName := strings.ToUpper(strings.TrimSpace(name))
		qtype, ok := dns.StringToType[cleanName]
		if cleanName == "OTHER" {
			// 选择一个未注册的类型码；输出时 TypeToString 查不到它，会稳定回到 OTHER。
			qtype, ok = uint16(65535), true
		}
		if !ok {
			continue
		}
		if mb.types == nil {
			mb.types = make(map[uint16]uint64, 8)
		}
		mb.types[qtype] += count
	}
	for code, count := range delta.Countries {
		if count == 0 {
			continue
		}
		if mb.countries == nil {
			mb.countries = make(map[string]uint64, 8)
		}
		mb.countries[strings.ToLower(strings.TrimSpace(code))] += count
	}
}

// MergeEdgeTelemetry 接收一个边缘节点的累计快照并将相邻心跳差值并入区间环形缓冲。
// 首次看到节点时只建立基线，不能把它从进程启动以来的全部累计量伪装成当前分钟流量。
func (tc *TelemetryCollector) MergeEdgeTelemetry(nodeID string, global DomainTelemetry, domains map[string]DomainTelemetry) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	previous, known := tc.edgeBaselines[nodeID]
	if !known {
		tc.edgeBaselines[nodeID] = edgeTelemetryBaseline{
			global:  cloneDomainTelemetry(global),
			domains: cloneDomainTelemetryMap(domains),
		}
		return
	}

	epoch := time.Now().Unix() / 60
	reset := global.Queries < previous.global.Queries
	mergeTelemetryDeltaIntoRing(&tc.minuteRing, telemetryDelta(global, previous.global, reset), epoch)

	for rawZone, current := range domains {
		zone := NormalizeZoneKey(rawZone)
		if zone == "" {
			continue
		}
		before, existed := previous.domains[zone]
		domainReset := reset || (existed && current.Queries < before.Queries)
		// 新增区域出现在一个已有节点上时，其累计量全部发生在相邻两次心跳之间，
		// 应当计入当前分钟；节点首次上报已在上面的基线分支被排除。
		if !existed {
			domainReset = true
		}
		delta := telemetryDelta(current, before, domainReset)
		dm, ok := tc.domains[zone]
		if !ok {
			dm = &domainMetrics{
				types:     make(map[uint16]uint64),
				countries: make(map[string]uint64),
			}
			tc.domains[zone] = dm
		}
		mergeTelemetryDeltaIntoRing(&dm.minuteRing, delta, epoch)
	}

	tc.edgeBaselines[nodeID] = edgeTelemetryBaseline{
		global:  cloneDomainTelemetry(global),
		domains: cloneDomainTelemetryMap(domains),
	}
}

// MergeDomainTelemetry 把边缘节点上报的区域遥测合并进累计结果。
func MergeDomainTelemetry(dst *DomainTelemetry, src DomainTelemetry) {
	dst.Queries += src.Queries
	dst.Blocked += src.Blocked
	dst.QPS += src.QPS

	for i := 0; i < 24; i++ {
		dst.Hourly[i] += src.Hourly[i]
		dst.HourlyBlocked[i] += src.HourlyBlocked[i]
	}

	if dst.Types == nil {
		dst.Types = make(map[string]uint64, len(src.Types))
	}
	for name, count := range src.Types {
		dst.Types[name] += count
	}

	if dst.Countries == nil {
		dst.Countries = make(map[string]uint64, len(src.Countries))
	}
	for code, count := range src.Countries {
		dst.Countries[code] += count
	}
}

// BuildTypeBreakdownFromNames 按查询类型名称构建与全局接口一致的分布结构。
func BuildTypeBreakdownFromNames(counts map[string]uint64) []map[string]interface{} {
	var total uint64
	for _, count := range counts {
		total += count
	}

	names := make([]string, 0, len(counts))
	for name, count := range counts {
		if count > 0 {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] == counts[names[j]] {
			return names[i] < names[j]
		}
		return counts[names[i]] > counts[names[j]]
	})

	res := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		pct := 0.0
		if total > 0 {
			pct = float64(counts[name]) / float64(total) * 100
		}
		res = append(res, map[string]interface{}{
			"name":    name,
			"value":   counts[name],
			"percent": pct,
		})
	}
	return res
}

// BuildHourlyTrend 依据 24 小时桶构建与全局趋势接口一致的时间序列。
func BuildHourlyTrend(hourly [24]uint64, hourlyBlocked [24]uint64) []QPSTrendPoint {
	points := make([]QPSTrendPoint, 0, 24)
	now := time.Now()
	curHour := now.Hour()

	for i := 23; i >= 0; i-- {
		hr := (curHour - i + 24) % 24
		t := now.Add(-time.Duration(i) * time.Hour)
		total := hourly[hr]
		blocked := hourlyBlocked[hr]
		success := total - blocked
		if blocked > total {
			success = 0
		}

		points = append(points, QPSTrendPoint{
			Timestamp: t.Format("15:04"),
			QPS:       avgQPS(total, 3600),
			Success:   int64(success),
			Blocked:   int64(blocked),
		})
	}
	return points
}

func (tc *TelemetryCollector) windowTicker() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tc.mu.Lock()
		tc.windowIndex = (tc.windowIndex + 1) % 60
		tc.qpsWindow[tc.windowIndex] = 0
		for _, dm := range tc.domains {
			dm.qpsWindow[tc.windowIndex] = 0
		}
		tc.lastWindowTs = time.Now().Unix()
		tc.mu.Unlock()
	}
}

func (tc *TelemetryCollector) GetCurrentQPS() int64 {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	var sum uint64
	count := 0
	for i := 0; i < 60; i++ {
		sum += tc.qpsWindow[i]
		if tc.qpsWindow[i] > 0 {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return int64(sum / 60)
}

func (tc *TelemetryCollector) GetTotals() (uint64, uint64) {
	return atomic.LoadUint64(&tc.totalQueries), atomic.LoadUint64(&tc.blockedQ)
}

func (tc *TelemetryCollector) GetTypeBreakdown() []map[string]interface{} {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	var total uint64
	for _, ptr := range tc.typeCounts {
		total += atomic.LoadUint64(ptr)
	}

	var res []map[string]interface{}
	for t, ptr := range tc.typeCounts {
		cnt := atomic.LoadUint64(ptr)
		typeName := dns.TypeToString[t]
		if typeName == "" {
			typeName = "OTHER"
		}
		if cnt > 0 || total == 0 {
			pct := 0.0
			if total > 0 {
				pct = float64(cnt) / float64(total) * 100
			}
			res = append(res, map[string]interface{}{
				"name":    typeName,
				"value":   cnt,
				"percent": pct,
			})
		}
	}
	return res
}

// GetGeoBreakdown returns strictly pure country / geographic region stats
func (tc *TelemetryCollector) GetGeoBreakdown() []map[string]interface{} {
	return BuildGeoBreakdown(tc.GetGeoCounts())
}

// GetGeoCounts returns a stable copy suitable for cluster heartbeat transport.
func (tc *TelemetryCollector) GetGeoCounts() map[string]uint64 {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	counts := make(map[string]uint64, len(tc.countryCounts))
	for code, ptr := range tc.countryCounts {
		count := atomic.LoadUint64(ptr)
		if count > 0 {
			counts[code] = count
		}
	}
	return counts
}

// BuildGeoBreakdown normalizes and sorts merged master/edge country counters.
func BuildGeoBreakdown(counts map[string]uint64) []map[string]interface{} {
	var total uint64
	for _, count := range counts {
		total += count
	}

	type entry struct {
		code  string
		count uint64
	}
	entries := make([]entry, 0, len(counts))
	for code, count := range counts {
		if count > 0 {
			entries = append(entries, entry{code: code, count: count})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].code < entries[j].code
		}
		return entries[i].count > entries[j].count
	})

	res := make([]map[string]interface{}, 0, len(entries))
	for _, item := range entries {
		pct := 0.0
		if total > 0 {
			pct = float64(item.count) / float64(total) * 100
		}
		res = append(res, map[string]interface{}{
			"region":  item.code,
			"code":    item.code,
			"queries": item.count,
			"percent": pct,
		})
	}
	return res
}

// GetCarrierBreakdown is retained for API backward compatibility and returns GeoBreakdown
func (tc *TelemetryCollector) GetCarrierBreakdown() []map[string]interface{} {
	return tc.GetGeoBreakdown()
}

type QPSTrendPoint struct {
	Ts        int64  `json:"ts"`
	Timestamp string `json:"timestamp"`
	// QPS 是速率量，必须用浮点：早前是 int64 且由整数除法得出，
	// 只要窗口内查询数不足窗口秒数就恒为 0，趋势图会退化成一条贴底的平线。
	QPS     float64 `json:"qps"`
	Success int64   `json:"success"`
	Blocked int64   `json:"blocked"`
}

// avgQPS 计算窗口内的平均每秒查询数，保留两位小数。
// 低流量下 QPS 天然是小数（例如一小时 120 次 = 0.03 QPS），
// 截断为整数会丢掉全部信息。
func avgQPS(queries uint64, windowSeconds int64) float64 {
	if windowSeconds <= 0 {
		return 0
	}
	return math.Round(float64(queries)/float64(windowSeconds)*100) / 100
}

// trendSpec 描述某个区间的聚合方式：总跨度与单点步长（均以分钟计）。
type trendSpec struct {
	durationMin int
	stepMin     int
}

// trendSpecs 是各区间到（跨度, 步长）的映射，步长保证整除跨度，
// 每个区间得到 60~90 个数据点，兼顾曲线平滑与传输体积。
var trendSpecs = map[string]trendSpec{
	"1h":  {durationMin: 60, stepMin: 1},        // 60 点
	"12h": {durationMin: 12 * 60, stepMin: 10},  // 72 点
	"24h": {durationMin: 24 * 60, stepMin: 20},  // 72 点
	"3d":  {durationMin: 3 * 1440, stepMin: 60}, // 72 点
	"7d":  {durationMin: 7 * 1440, stepMin: 120},
	"14d": {durationMin: 14 * 1440, stepMin: 240},
	"30d": {durationMin: 30 * 1440, stepMin: 480},
}

// NormalizeTrendRange 把外部传入的区间键归一化，非法值回退到 24h。
func NormalizeTrendRange(rangeKey string) string {
	key := strings.ToLower(strings.TrimSpace(rangeKey))
	if _, ok := trendSpecs[key]; ok {
		return key
	}
	return "24h"
}

// formatTrendLabel 依据区间跨度选择合适的横轴标签粒度：
// 一天以内显示 时:分，跨天区间显示 月-日。
func formatTrendLabel(t time.Time, durationMin int) string {
	if durationMin <= 24*60 {
		return t.Format("15:04")
	}
	return t.Format("01-02")
}

// buildTrendFromRing 从给定分钟环形缓冲按区间聚合出趋势序列。
// 每个数据点的 QPS 为该步长窗口内的平均每秒查询数。调用方须持有 tc.mu（读锁即可）。
func buildTrendFromRing(ring *[minuteRingSize]minuteBucket, rangeKey string) []QPSTrendPoint {
	spec := trendSpecs[NormalizeTrendRange(rangeKey)]
	nowMin := time.Now().Unix() / 60
	startMin := nowMin - int64(spec.durationMin) + 1

	points := make([]QPSTrendPoint, 0, spec.durationMin/spec.stepMin)

	for bucketStart := startMin; bucketStart <= nowMin; bucketStart += int64(spec.stepMin) {
		bucketEnd := bucketStart + int64(spec.stepMin) // 不含
		var queries, blocked uint64
		var minutes int64

		for m := bucketStart; m < bucketEnd && m <= nowMin; m++ {
			minutes++
			slot := ((m % minuteRingSize) + minuteRingSize) % minuteRingSize
			mb := &ring[slot]
			if mb.epoch == m {
				queries += mb.queries
				blocked += mb.blocked
			}
		}
		if minutes == 0 {
			continue
		}

		seconds := minutes * 60
		success := int64(queries) - int64(blocked)
		if success < 0 {
			success = 0
		}
		pointTime := time.Unix(bucketStart*60, 0)

		points = append(points, QPSTrendPoint{
			Ts:        pointTime.Unix(),
			Timestamp: formatTrendLabel(pointTime, spec.durationMin),
			QPS:       avgQPS(queries, seconds),
			Success:   success,
			Blocked:   int64(blocked),
		})
	}
	return points
}

// GetTrend 按给定区间从全局分钟级环形缓冲聚合出趋势序列。
func (tc *TelemetryCollector) GetTrend(rangeKey string) []QPSTrendPoint {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return buildTrendFromRing(&tc.minuteRing, rangeKey)
}

// aggregateRing 遍历区间内的分钟桶，累加查询类型与国家计数。
// 调用方须持有 tc.mu 读锁。遍历跨度最多 30 天（43200 分钟），仅在统计查询时触发。
func aggregateRing(ring *[minuteRingSize]minuteBucket, rangeKey string) (map[uint16]uint64, map[string]uint64) {
	spec := trendSpecs[NormalizeTrendRange(rangeKey)]
	nowMin := time.Now().Unix() / 60
	startMin := nowMin - int64(spec.durationMin) + 1

	types := make(map[uint16]uint64)
	countries := make(map[string]uint64)
	for m := startMin; m <= nowMin; m++ {
		slot := ((m % minuteRingSize) + minuteRingSize) % minuteRingSize
		mb := &ring[slot]
		if mb.epoch != m {
			continue
		}
		for t, c := range mb.types {
			types[t] += c
		}
		for code, c := range mb.countries {
			countries[code] += c
		}
	}
	return types, countries
}

// buildTypeBreakdownFromCounts 从类型码计数构建分布结构（与 GetTypeBreakdown 输出一致，按次数降序）。
func buildTypeBreakdownFromCounts(counts map[uint16]uint64) []map[string]interface{} {
	var total uint64
	for _, c := range counts {
		total += c
	}
	type entry struct {
		name  string
		count uint64
	}
	entries := make([]entry, 0, len(counts))
	for t, cnt := range counts {
		if cnt == 0 {
			continue
		}
		name := dns.TypeToString[t]
		if name == "" {
			name = "OTHER"
		}
		entries = append(entries, entry{name: name, count: cnt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].name < entries[j].name
		}
		return entries[i].count > entries[j].count
	})
	res := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		pct := 0.0
		if total > 0 {
			pct = float64(e.count) / float64(total) * 100
		}
		res = append(res, map[string]interface{}{
			"name":    e.name,
			"value":   e.count,
			"percent": pct,
		})
	}
	return res
}

// GetTypeBreakdownRange 返回选定区间内的查询类型分布（主控本地时序）。
func (tc *TelemetryCollector) GetTypeBreakdownRange(rangeKey string) []map[string]interface{} {
	tc.mu.RLock()
	types, _ := aggregateRing(&tc.minuteRing, rangeKey)
	tc.mu.RUnlock()
	return buildTypeBreakdownFromCounts(types)
}

// GetGeoCountsRange 返回选定区间内的国家/地区计数（主控本地时序），供 BuildGeoBreakdown 组装。
func (tc *TelemetryCollector) GetGeoCountsRange(rangeKey string) map[string]uint64 {
	tc.mu.RLock()
	_, countries := aggregateRing(&tc.minuteRing, rangeKey)
	tc.mu.RUnlock()
	return countries
}

func (tc *TelemetryCollector) GetHourlyTrend() []QPSTrendPoint {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	var points []QPSTrendPoint
	now := time.Now()
	curHour := now.Hour()

	for i := 23; i >= 0; i-- {
		hr := (curHour - i + 24) % 24
		t := now.Add(-time.Duration(i) * time.Hour)
		total := tc.hourlyBuckets[hr]
		succ := tc.hourlySuccess[hr]
		blk := tc.hourlyBlocked[hr]

		points = append(points, QPSTrendPoint{
			Timestamp: t.Format("15:04"),
			QPS:       avgQPS(total, 3600),
			Success:   int64(succ),
			Blocked:   int64(blk),
		})
	}
	return points
}
