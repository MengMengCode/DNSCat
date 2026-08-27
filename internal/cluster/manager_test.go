package cluster

import (
	"encoding/json"
	"strings"
	"testing"

	"dnscat/internal/model"
)

func TestZoneSnapshotRestoresProtectedDNSSECPrivateKeys(t *testing.T) {
	snapshot := ZoneSnapshot{
		Domains: []model.Domain{{
			ID: 1,
			DNSSECKeys: []model.DNSSECKey{{
				ID: 42, DomainID: 1, KeyType: model.KeyTypeZSK, PrivateKey: "must-not-use-embedded-field",
			}},
		}},
		DNSSECPrivateKeys: map[uint]string{42: "edge-signing-key"},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(data), "must-not-use-embedded-field") {
		t.Fatal("normal DNSSEC key JSON exposed its private key field")
	}

	var decoded ZoneSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	decoded.RestoreDNSSECPrivateKeys()
	if got := decoded.Domains[0].DNSSECKeys[0].PrivateKey; got != "edge-signing-key" {
		t.Fatalf("restored private key=%q, want edge-signing-key", got)
	}
}
