package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDDNSHelpers(t *testing.T) {
	plain, prefix, hash, err := GenerateDDNSKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, prefix+"_") || len(hash) != 64 || hash != HashDDNSKey(plain) {
		t.Fatalf("unexpected generated key: plain=%q prefix=%q hash=%q", plain, prefix, hash)
	}

	if got := NormalizeDDNSHostnames("Home.Example.COM., @, home", "example.com"); got != "home,@" {
		t.Fatalf("NormalizeDDNSHostnames()=%q, want home,@", got)
	}
	if !hostnameAuthorized("home,@", "HOME") || hostnameAuthorized("home,@", "other") {
		t.Fatal("hostname authorization did not enforce the configured scope")
	}
	if relative, ok := relativeNameFor("HOME.Example.com.", "example.com"); !ok || relative != "home" {
		t.Fatalf("relativeNameFor()=(%q,%v), want (home,true)", relative, ok)
	}

	allowlist, err := ValidateDDNSAllowedIPs("198.51.100.7, 2001:db8::/48")
	if err != nil {
		t.Fatal(err)
	}
	if !ddnsSourceAllowed(allowlist, "198.51.100.7") || !ddnsSourceAllowed(allowlist, "2001:db8::9") {
		t.Fatalf("valid allowlist did not match: %q", allowlist)
	}
	if ddnsSourceAllowed(allowlist, "203.0.113.9") {
		t.Fatal("allowlist accepted an address outside its IP/CIDR entries")
	}
	if _, err := ValidateDDNSAllowedIPs("not-an-ip"); err == nil {
		t.Fatal("invalid allowlist entry was accepted")
	}
}

func TestDDNSUpdateWithIndependentBasicKeyAndSourceAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:ddns-handler-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip("SQLite integration test requires CGO")
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Domain{}, &model.Record{}, &model.DDNSKey{}, &model.AuditLog{}); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip("SQLite integration test requires CGO")
		}
		t.Fatal(err)
	}
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })

	domain := model.Domain{UserID: 42, Name: "example.test", Status: model.DomainStatusActive, SOASerial: 1}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	plain, prefix, hash, err := GenerateDDNSKey()
	if err != nil {
		t.Fatal(err)
	}
	key := model.DDNSKey{
		DomainID: domain.ID, UserID: domain.UserID, Name: "router", KeyPrefix: prefix, KeyHash: hash,
		Hostnames: "home", AllowedIPs: "198.51.100.0/24", RecordTTL: 60,
		AllowIPv4: true, AutoCreate: true, Enabled: true,
	}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	previousLimiter := ddnsLimiter
	ddnsLimiter = &ddnsFailureLimiter{
		attempts: make(map[string]*ddnsFailureRecord), window: 10 * time.Minute, limit: 20,
	}
	t.Cleanup(func() { ddnsLimiter = previousLimiter })

	call := func(sourceIP, address string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"/nic/update?hostname=home.example.test&myip="+address, nil)
		req.RemoteAddr = sourceIP + ":43210"
		req.SetBasicAuth("dnscat", plain)
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = req
		NewDDNSHandler().Update(ctx)
		return w
	}

	first := call("198.51.100.23", "203.0.113.44")
	if first.Code != http.StatusOK || strings.TrimSpace(first.Body.String()) != "good 203.0.113.44" {
		t.Fatalf("first update status=%d body=%q", first.Code, first.Body.String())
	}
	var record model.Record
	if err := db.Where("domain_id = ? AND name = ? AND type = ?", domain.ID, "home", model.RecordTypeA).
		First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.Value != "203.0.113.44" || record.TTL != 60 {
		t.Fatalf("record after DDNS update=%#v", record)
	}

	second := call("198.51.100.23", "203.0.113.44")
	if second.Code != http.StatusOK || strings.TrimSpace(second.Body.String()) != "nochg 203.0.113.44" {
		t.Fatalf("unchanged update status=%d body=%q", second.Code, second.Body.String())
	}

	blocked := call("203.0.113.23", "192.0.2.99")
	if blocked.Code != http.StatusUnauthorized || strings.TrimSpace(blocked.Body.String()) != "badauth" {
		t.Fatalf("allowlist rejection status=%d body=%q", blocked.Code, blocked.Body.String())
	}
	if err := db.First(&record, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Value != "203.0.113.44" {
		t.Fatalf("blocked source changed the record to %q", record.Value)
	}
}
