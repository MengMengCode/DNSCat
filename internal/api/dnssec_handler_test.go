package api

import (
	"sync"
	"testing"

	"dnscat/internal/model"

	"gorm.io/gorm/schema"
)

func TestDNSSECEnabledColumnMapping(t *testing.T) {
	parsed, err := schema.Parse(&model.Domain{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse domain schema: %v", err)
	}
	field := parsed.LookUpField("DNSSECEnabled")
	if field == nil {
		t.Fatal("DNSSECEnabled field not found")
	}
	if field.DBName != "dns_sec_enabled" {
		t.Fatalf("DNSSECEnabled column=%q, want dns_sec_enabled", field.DBName)
	}
}
