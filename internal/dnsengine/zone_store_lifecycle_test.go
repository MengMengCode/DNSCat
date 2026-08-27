package dnsengine

import (
	"net"
	"testing"

	"dnscat/internal/model"

	"github.com/miekg/dns"
)

func testDomain(id uint, name, address string) model.Domain {
	return model.Domain{
		ID: id, Name: name, Status: model.DomainStatusActive,
		PrimaryNS: "ns1.dnscat.test.", AdminEmail: "admin.dnscat.test.",
		SOARefresh: 3600, SOARetry: 600, SOAExpire: 86400, SOAMinimum: 300,
		Records: []model.Record{{
			ID: id, DomainID: id, Name: "@", Type: model.RecordTypeA,
			Value: address, TTL: 300, Weight: 100, GeoLine: "default", Enabled: true,
		}},
	}
}

func TestReplaceZonesRemovesOmittedZones(t *testing.T) {
	store := &ZoneStore{}
	first := testDomain(1, "first.test", "192.0.2.1")
	second := testDomain(2, "second.test", "192.0.2.2")
	store.ReplaceZones([]model.Domain{first, second})

	store.ReplaceZones([]model.Domain{second})

	removed := store.Query("first.test.", dns.TypeA, net.ParseIP("127.0.0.1"), nil, false)
	if removed.Rcode != dns.RcodeRefused {
		t.Fatalf("omitted zone remained authoritative: rcode=%d", removed.Rcode)
	}
	retained := store.Query("second.test.", dns.TypeA, net.ParseIP("127.0.0.1"), nil, false)
	if retained.Rcode != dns.RcodeSuccess || len(retained.Answer) != 1 {
		t.Fatalf("retained zone was not answerable: rcode=%d answers=%d", retained.Rcode, len(retained.Answer))
	}
}

func TestWeightedIndexUsesConfiguredWeights(t *testing.T) {
	pool := []CachedRecord{
		{Record: model.Record{Weight: 80}},
		{Record: model.Record{Weight: 20}},
	}

	if got := totalWeight(pool); got != 100 {
		t.Fatalf("totalWeight()=%d, want 100", got)
	}
	if got := weightedIndex(pool, 79); got != 0 {
		t.Fatalf("weightedIndex(79)=%d, want 0", got)
	}
	if got := weightedIndex(pool, 80); got != 1 {
		t.Fatalf("weightedIndex(80)=%d, want 1", got)
	}
}

func TestHTTPSRecordPreservesServiceParameters(t *testing.T) {
	rr, err := ConvertRecordToRR("example.test", &model.Record{
		Name: "@", Type: model.RecordTypeHTTPS, TTL: 300,
		Value: `1 . alpn="h2,h3" port=443 ipv4hint="192.0.2.10,192.0.2.11"`,
	})
	if err != nil {
		t.Fatalf("ConvertRecordToRR() error: %v", err)
	}
	https, ok := rr.(*dns.HTTPS)
	if !ok {
		t.Fatalf("record type=%T, want *dns.HTTPS", rr)
	}
	if len(https.Value) != 3 {
		t.Fatalf("HTTPS service parameters=%d, want 3: %s", len(https.Value), rr.String())
	}
}
