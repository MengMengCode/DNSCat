package dnsengine

import (
	"net"
	"testing"

	"dnscat/internal/config"
	"dnscat/internal/model"

	"github.com/miekg/dns"
)

func TestProcessQueryPreservesAuthoritativeRcode(t *testing.T) {
	store := &ZoneStore{}
	domain := testDomain(10, "rcode.test", "192.0.2.10")
	store.LoadZone(&domain)
	server := NewServer(config.DefaultConfig(), store)

	tests := []struct {
		name  string
		qname string
		want  int
	}{
		{name: "unknown zone refused", qname: "outside.test.", want: dns.RcodeRefused},
		{name: "unknown name nxdomain", qname: "missing.rcode.test.", want: dns.RcodeNameError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := new(dns.Msg)
			req.SetQuestion(tt.qname, dns.TypeA)
			resp := server.ProcessQuery(req, net.ParseIP("127.0.0.1"))
			if resp.Rcode != tt.want {
				t.Fatalf("rcode=%d, want %d", resp.Rcode, tt.want)
			}
		})
	}
}

func TestInactiveDomainIsNotLoadedBySnapshot(t *testing.T) {
	store := &ZoneStore{}
	active := testDomain(11, "active.test", "192.0.2.11")
	paused := testDomain(12, "paused.test", "192.0.2.12")
	paused.Status = model.DomainStatusPaused

	// Cluster snapshots are active-only; ReplaceZones must also evict any zone
	// that disappears when its status changes.
	store.ReplaceZones([]model.Domain{active, paused})
	store.ReplaceZones([]model.Domain{active})
	if zone, _ := store.FindZone("paused.test."); zone != nil {
		t.Fatal("paused zone remained in the authoritative store")
	}
}
