package dnsengine_test

import (
	"net"
	"testing"

	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/miekg/dns"
)

func TestDNSResolutionAndDNSSEC(t *testing.T) {
	store := &dnsengine.ZoneStore{}

	// Setup demo domain
	domain := &model.Domain{
		ID:            1,
		Name:          "example.com",
		Status:        model.DomainStatusActive,
		PrimaryNS:     "ns1.example.com.",
		AdminEmail:    "admin.example.com.",
		SOARefresh:    10000,
		SOARetry:      2400,
		SOAExpire:     604800,
		SOAMinimum:    300,
		SOASerial:     1,
		DNSSECEnabled: true,
	}

	// Generate DNSSEC Keys
	keys, err := dnsengine.GenerateDNSSECKeyPair(domain.ID, domain.Name)
	if err != nil {
		t.Fatalf("Failed to generate DNSSEC keys: %v", err)
	}
	domain.DNSSECKeys = keys

	// Add sample records
	domain.Records = []model.Record{
		{DomainID: 1, Name: "@", Type: model.RecordTypeA, Value: "93.184.216.34", TTL: 300, GeoLine: "default", Weight: 100, Enabled: true},
		{DomainID: 1, Name: "www", Type: model.RecordTypeCNAME, Value: "example.com.", TTL: 300, GeoLine: "default", Weight: 100, Enabled: true},
		{DomainID: 1, Name: "cdn", Type: model.RecordTypeA, Value: "114.114.114.114", TTL: 60, GeoLine: "cn", Weight: 100, Enabled: true},
		{DomainID: 1, Name: "cdn", Type: model.RecordTypeA, Value: "1.1.1.1", TTL: 60, GeoLine: "us", Weight: 100, Enabled: true},
		{DomainID: 1, Name: "cdn", Type: model.RecordTypeA, Value: "8.8.8.8", TTL: 60, GeoLine: "default", Weight: 100, Enabled: true},
		{DomainID: 1, Name: "@", Type: model.RecordTypeTXT, Value: "v=spf1 -all", TTL: 300, GeoLine: "default", Weight: 100, Enabled: true},
	}

	store.LoadZone(domain)

	// 1. Test Apex A record query
	msg := store.Query("example.com.", dns.TypeA, net.ParseIP("127.0.0.1"), nil, true)
	if msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("Expected RcodeSuccess, got %d", msg.Rcode)
	}
	if len(msg.Answer) == 0 {
		t.Fatalf("Expected answer for example.com A record")
	}
	aRecord, ok := msg.Answer[0].(*dns.A)
	if !ok || aRecord.A.String() != "93.184.216.34" {
		t.Fatalf("Expected IP 93.184.216.34, got %v", msg.Answer[0])
	}

	// Verify DNSSEC RRSIG is present in answer
	var hasRRSIG bool
	for _, ans := range msg.Answer {
		if ans.Header().Rrtype == dns.TypeRRSIG {
			hasRRSIG = true
			break
		}
	}
	if !hasRRSIG {
		t.Errorf("Expected RRSIG signature in DNSSEC answer")
	}

	// 2. Test Geo Line Routing (China client IP)
	cnIP := net.ParseIP("114.114.114.114")
	cnMsg := store.Query("cdn.example.com.", dns.TypeA, cnIP, nil, false)
	if len(cnMsg.Answer) == 0 {
		t.Fatalf("Expected answer for CN line query")
	}
	ansIP := cnMsg.Answer[0].(*dns.A).A.String()
	if ansIP != "114.114.114.114" {
		t.Errorf("Expected CN IP 114.114.114.114, got %s", ansIP)
	}

	// 3. Test Geo Line Routing (US client IP)
	usIP := net.ParseIP("8.8.8.8")
	usMsg := store.Query("cdn.example.com.", dns.TypeA, usIP, nil, false)
	if len(usMsg.Answer) == 0 {
		t.Fatalf("Expected answer for US line query")
	}
	ansUSIP := usMsg.Answer[0].(*dns.A).A.String()
	if ansUSIP != "1.1.1.1" {
		t.Errorf("Expected US IP 1.1.1.1, got %s", ansUSIP)
	}

	// 4. Test DNSKEY Query
	keyMsg := store.Query("example.com.", dns.TypeDNSKEY, net.ParseIP("127.0.0.1"), nil, true)
	if len(keyMsg.Answer) == 0 {
		t.Errorf("Expected DNSKEY answer")
	}

	// 5. Test NXDOMAIN response
	nxMsg := store.Query("notexist.example.com.", dns.TypeA, net.ParseIP("127.0.0.1"), nil, true)
	if nxMsg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN (NameError), got %d", nxMsg.Rcode)
	}
}
