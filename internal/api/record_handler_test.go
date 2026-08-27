package api

import (
	"testing"

	"dnscat/internal/model"
)

func TestValidateRecordValue(t *testing.T) {
	tests := []struct {
		name    string
		typeID  model.RecordType
		value   string
		wantErr bool
	}{
		{name: "A", typeID: model.RecordTypeA, value: "192.0.2.10"},
		{name: "bad A", typeID: model.RecordTypeA, value: "999.0.2.10", wantErr: true},
		{name: "HTTPS parameters", typeID: model.RecordTypeHTTPS, value: `1 . alpn="h2,h3" port=443`},
		{name: "bad TLSA", typeID: model.RecordTypeTLSA, value: "3 1", wantErr: true},
		{name: "unknown type", typeID: model.RecordType("BOGUS"), value: "value", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecordValue(tt.typeID, tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRecordValue() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
