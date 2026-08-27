package dnsengine

import (
	"net"
	"testing"

	"dnscat/internal/model"

	"github.com/miekg/dns"
)

// geoRoutingDomain 构造一个带多条分线记录的区域：
// cn 线路、us 线路各一条，外加一条全球默认线路。
func geoRoutingDomain() model.Domain {
	return model.Domain{
		ID: 90, Name: "geo.test", Status: model.DomainStatusActive,
		PrimaryNS: "ns1.dnscat.test.", AdminEmail: "admin.dnscat.test.",
		SOARefresh: 3600, SOARetry: 600, SOAExpire: 86400, SOAMinimum: 300,
		Records: []model.Record{
			{
				ID: 901, DomainID: 90, Name: "cdn", Type: model.RecordTypeA,
				Value: "114.114.114.114", TTL: 60, Weight: 100, GeoLine: "cn", Enabled: true,
			},
			{
				ID: 902, DomainID: 90, Name: "cdn", Type: model.RecordTypeA,
				Value: "1.1.1.1", TTL: 60, Weight: 100, GeoLine: "us", Enabled: true,
			},
			{
				ID: 903, DomainID: 90, Name: "cdn", Type: model.RecordTypeA,
				Value: "8.8.8.8", TTL: 60, Weight: 100, GeoLine: "default", Enabled: true,
			},
		},
	}
}

func answerValues(msg *dns.Msg) []string {
	values := make([]string, 0, len(msg.Answer))
	for _, rr := range msg.Answer {
		if a, ok := rr.(*dns.A); ok {
			values = append(values, a.A.String())
		}
	}
	return values
}

// 命中具体分线时，默认线路记录不得出现在应答中，否则分线解析失去确定性。
func TestGeoLineRoutingExcludesDefaultWhenSpecificLineMatches(t *testing.T) {
	store := &ZoneStore{}
	domain := geoRoutingDomain()
	store.ReplaceZones([]model.Domain{domain})

	// 114.114.114.0/24 属于内置库的 cn 线路。
	resp := store.Query("cdn.geo.test.", dns.TypeA, net.ParseIP("114.114.114.114"), nil, false)
	got := answerValues(resp)
	if len(got) != 1 || got[0] != "114.114.114.114" {
		t.Fatalf("cn client should receive only the cn line record, got %v", got)
	}

	// 8.8.8.0/24 属于内置库的 us 线路。
	respUS := store.Query("cdn.geo.test.", dns.TypeA, net.ParseIP("8.8.8.8"), nil, false)
	gotUS := answerValues(respUS)
	if len(gotUS) != 1 || gotUS[0] != "1.1.1.1" {
		t.Fatalf("us client should receive only the us line record, got %v", gotUS)
	}
}

// 未命中任何具体分线的客户端应回退到全球默认线路。
func TestGeoLineRoutingFallsBackToDefaultLine(t *testing.T) {
	store := &ZoneStore{}
	store.ReplaceZones([]model.Domain{geoRoutingDomain()})

	// 198.51.100.0/24 未收录于内置库，判定为 default。
	resp := store.Query("cdn.geo.test.", dns.TypeA, net.ParseIP("198.51.100.7"), nil, false)
	got := answerValues(resp)
	if len(got) != 1 || got[0] != "8.8.8.8" {
		t.Fatalf("unmatched client should fall back to the default line, got %v", got)
	}
}

// ECS 客户端子网应优先于传输层来源 IP 参与分线判定。
func TestGeoLineRoutingPrefersECSSubnet(t *testing.T) {
	store := &ZoneStore{}
	store.ReplaceZones([]model.Domain{geoRoutingDomain()})

	// 传输层来自未收录网段，ECS 指明 cn 网段，应按 cn 线路应答。
	resp := store.Query(
		"cdn.geo.test.", dns.TypeA,
		net.ParseIP("198.51.100.7"),
		net.ParseIP("114.114.114.114"),
		false,
	)
	got := answerValues(resp)
	if len(got) != 1 || got[0] != "114.114.114.114" {
		t.Fatalf("ECS subnet should drive line selection, got %v", got)
	}
}

// ExplainRouting 必须与真实解析保持一致：入选记录即真实应答记录，
// 落选记录需给出明确原因。
func TestExplainRoutingMatchesResolution(t *testing.T) {
	store := &ZoneStore{}
	store.ReplaceZones([]model.Domain{geoRoutingDomain()})

	decision := store.ExplainRouting("cdn.geo.test.", dns.TypeA, net.ParseIP("114.114.114.114"))
	if !decision.Found {
		t.Fatal("zone should be found")
	}
	if decision.MatchedLine != "cn" {
		t.Fatalf("matched line = %q, want cn", decision.MatchedLine)
	}
	if decision.TotalMatched != 3 {
		t.Fatalf("total candidates = %d, want 3", decision.TotalMatched)
	}

	var selected []string
	for _, cand := range decision.Candidates {
		if cand.Selected {
			selected = append(selected, cand.EffectiveValue)
			continue
		}
		if cand.Reason != "line_not_matched" {
			t.Fatalf("record %s excluded with unexpected reason %q", cand.Record.Value, cand.Reason)
		}
	}
	if len(selected) != 1 || selected[0] != "114.114.114.114" {
		t.Fatalf("explained selection = %v, want [114.114.114.114]", selected)
	}

	// 与真实解析结果交叉校验。
	resp := store.Query("cdn.geo.test.", dns.TypeA, net.ParseIP("114.114.114.114"), nil, false)
	if got := answerValues(resp); len(got) != 1 || got[0] != selected[0] {
		t.Fatalf("explanation %v disagrees with resolution %v", selected, answerValues(resp))
	}
}

// 探测失败且配置备用 IP 时，应答必须切换到备用地址（真实容灾）。
func TestFailoverSwitchesAnswerToFallbackIP(t *testing.T) {
	store := &ZoneStore{}
	domain := model.Domain{
		ID: 91, Name: "failover.test", Status: model.DomainStatusActive,
		PrimaryNS: "ns1.dnscat.test.", AdminEmail: "admin.dnscat.test.",
		SOARefresh: 3600, SOARetry: 600, SOAExpire: 86400, SOAMinimum: 300,
		Records: []model.Record{{
			ID: 911, DomainID: 91, Name: "@", Type: model.RecordTypeA,
			Value: "203.0.113.10", TTL: 60, Weight: 100, GeoLine: "default", Enabled: true,
		}},
	}
	store.ReplaceZones([]model.Domain{domain})

	// 源站探测失败，备用 IP 生效。
	store.UpdateRecordHealth(91, 911, false, "203.0.113.99")
	resp := store.Query("failover.test.", dns.TypeA, net.ParseIP("198.51.100.7"), nil, false)
	if got := answerValues(resp); len(got) != 1 || got[0] != "203.0.113.99" {
		t.Fatalf("failover should answer with the fallback IP, got %v", got)
	}

	// 源站恢复，应自动切回原地址。
	store.UpdateRecordHealth(91, 911, true, "203.0.113.99")
	restored := store.Query("failover.test.", dns.TypeA, net.ParseIP("198.51.100.7"), nil, false)
	if got := answerValues(restored); len(got) != 1 || got[0] != "203.0.113.10" {
		t.Fatalf("recovered origin should be restored, got %v", got)
	}
}

// 探测失败且未配置备用 IP 时，该记录应从应答中摘除。
func TestUnhealthyRecordWithoutFallbackIsRemoved(t *testing.T) {
	store := &ZoneStore{}
	domain := model.Domain{
		ID: 92, Name: "drain.test", Status: model.DomainStatusActive,
		PrimaryNS: "ns1.dnscat.test.", AdminEmail: "admin.dnscat.test.",
		SOARefresh: 3600, SOARetry: 600, SOAExpire: 86400, SOAMinimum: 300,
		Records: []model.Record{
			{
				ID: 921, DomainID: 92, Name: "@", Type: model.RecordTypeA,
				Value: "203.0.113.21", TTL: 60, Weight: 100, GeoLine: "default", Enabled: true,
			},
			{
				ID: 922, DomainID: 92, Name: "@", Type: model.RecordTypeA,
				Value: "203.0.113.22", TTL: 60, Weight: 100, GeoLine: "default", Enabled: true,
			},
		},
	}
	store.ReplaceZones([]model.Domain{domain})

	store.UpdateRecordHealth(92, 921, false, "")
	resp := store.Query("drain.test.", dns.TypeA, net.ParseIP("198.51.100.7"), nil, false)
	got := answerValues(resp)
	if len(got) != 1 || got[0] != "203.0.113.22" {
		t.Fatalf("down origin without fallback should be drained, got %v", got)
	}
}
