package dnsengine

import (
	"math"
	"net"
	"testing"

	"dnscat/internal/config"

	"github.com/miekg/dns"
)

func TestProcessQueryRecordsCountryInsteadOfRoutingLine(t *testing.T) {
	previous := GlobalTelemetry
	GlobalTelemetry = NewTelemetryCollector()
	t.Cleanup(func() { GlobalTelemetry = previous })

	store := &ZoneStore{}
	domain := testDomain(20, "geo.test", "192.0.2.20")
	store.LoadZone(&domain)
	server := NewServer(config.DefaultConfig(), store)

	// 私有地址：用于验证「无 ECS 且来源不可归属公网地区」这条分支
	localRequest := new(dns.Msg)
	localRequest.SetQuestion("geo.test.", dns.TypeA)
	server.ProcessQuery(localRequest, net.ParseIP("10.0.0.100"))

	ecsRequest := new(dns.Msg)
	ecsRequest.SetQuestion("geo.test.", dns.TypeA)
	opt := &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
	opt.Option = append(opt.Option, &dns.EDNS0_SUBNET{
		Code: dns.EDNS0SUBNET, Family: 1, SourceNetmask: 24,
		Address: net.ParseIP("8.8.8.0").To4(),
	})
	ecsRequest.Extra = append(ecsRequest.Extra, opt)
	server.ProcessQuery(ecsRequest, net.ParseIP("10.0.0.100"))

	counts := GlobalTelemetry.GetGeoCounts()
	if counts["cn"] != 1 {
		t.Fatalf("private request country count=%d, want cn=1", counts["cn"])
	}
	if counts["us"] != 1 {
		t.Fatalf("ECS request country count=%d, want us=1", counts["us"])
	}
	if counts["default"] != 0 {
		t.Fatalf("requests incorrectly recorded in default bucket: %d", counts["default"])
	}
}

func TestBuildGeoBreakdownSortsAndCalculatesPercent(t *testing.T) {
	result := BuildGeoBreakdown(map[string]uint64{"jp": 10, "us": 30, "cn": 60, "de": 0})
	if len(result) != 3 {
		t.Fatalf("entries=%d, want 3 non-zero countries", len(result))
	}
	if result[0]["code"] != "cn" || result[1]["code"] != "us" || result[2]["code"] != "jp" {
		t.Fatalf("unexpected country ordering: %#v", result)
	}
	if math.Abs(result[0]["percent"].(float64)-60) > 0.001 {
		t.Fatalf("cn percent=%v, want 60", result[0]["percent"])
	}
}

func TestMergeEdgeTelemetryUsesHeartbeatDeltaForRangeStats(t *testing.T) {
	tc := NewTelemetryCollector()
	baseline := DomainTelemetry{
		Queries:   100,
		Types:     map[string]uint64{"A": 70, "AAAA": 30},
		Countries: map[string]uint64{"us": 60, "sg": 40},
	}
	tc.MergeEdgeTelemetry("edge-sg", baseline, map[string]DomainTelemetry{
		"example.test": baseline,
	})

	// 首次心跳只建立累计基线，不能把节点过去的 100 次请求塞进当前分钟。
	if got := tc.GetGeoCountsRange("1h")["us"]; got != 0 {
		t.Fatalf("first heartbeat was counted as current traffic: us=%d", got)
	}

	current := DomainTelemetry{
		Queries:   112,
		Blocked:   2,
		Types:     map[string]uint64{"A": 78, "AAAA": 34},
		Countries: map[string]uint64{"us": 67, "sg": 45},
	}
	tc.MergeEdgeTelemetry("edge-sg", current, map[string]DomainTelemetry{
		"example.test": current,
	})

	geo := tc.GetGeoCountsRange("1h")
	if geo["us"] != 7 || geo["sg"] != 5 {
		t.Fatalf("unexpected edge geo delta: %#v", geo)
	}
	types := tc.GetTypeBreakdownRange("1h")
	if len(types) != 2 || types[0]["name"] != "A" || types[0]["value"] != uint64(8) {
		t.Fatalf("unexpected edge type delta: %#v", types)
	}

	points, domainTypes, domainGeo := tc.GetDomainRangeStats("example.test", "1h")
	if len(points) != 60 || points[len(points)-1].Success != 10 || points[len(points)-1].Blocked != 2 {
		t.Fatalf("unexpected domain trend delta: %#v", points[len(points)-1])
	}
	if len(domainTypes) != 2 || domainTypes[0]["value"] != uint64(8) {
		t.Fatalf("unexpected domain type delta: %#v", domainTypes)
	}
	if len(domainGeo) != 2 || domainGeo[0]["queries"] != uint64(7) {
		t.Fatalf("unexpected domain geo delta: %#v", domainGeo)
	}
}
