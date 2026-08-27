package dnsengine

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"dnscat/internal/model"

	"github.com/miekg/dns"
)

// EnsureFQDN adds trailing dot to domain name if missing
func EnsureFQDN(name string) string {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, ".") {
		return name + "."
	}
	return name
}

// BuildSOA creates a SOA RR from domain metadata
func BuildSOA(domain *model.Domain) *dns.SOA {
	origin := EnsureFQDN(domain.Name)
	primaryNS := EnsureFQDN(domain.PrimaryNS)
	if primaryNS == "." {
		// 兜底用域名自身派生，而不是任何写死的主机名：
		// 写死会让每个部署的 SOA 都指向本项目作者的域名，属于真实的 DNS 卫生问题。
		primaryNS = "ns1." + origin
	}
	adminEmail := EnsureFQDN(domain.AdminEmail)
	if adminEmail == "." {
		adminEmail = "admin." + origin
	}
	// SOA RNAME 用点分格式（例：admin@example.com -> admin.example.com.）
	adminEmail = strings.Replace(adminEmail, "@", ".", 1)

	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   origin,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    domain.SOAMinimum,
		},
		Ns:      primaryNS,
		Mbox:    adminEmail,
		Serial:  domain.SOASerial,
		Refresh: domain.SOARefresh,
		Retry:   domain.SOARetry,
		Expire:  domain.SOAExpire,
		Minttl:  domain.SOAMinimum,
	}
}

// ConvertRecordToRR transforms model.Record to dns.RR
func ConvertRecordToRR(zoneName string, r *model.Record) (dns.RR, error) {
	zoneName = EnsureFQDN(zoneName)
	var fqdn string

	if r.Name == "@" || r.Name == "" {
		fqdn = zoneName
	} else if strings.HasSuffix(r.Name, zoneName) {
		fqdn = EnsureFQDN(r.Name)
	} else {
		fqdn = EnsureFQDN(r.Name + "." + zoneName)
	}

	ttl := r.TTL
	if ttl == 0 || ttl == 1 {
		ttl = 300
	}

	hdr := dns.RR_Header{
		Name:   fqdn,
		Rrtype: dns.StringToType[string(r.Type)],
		Class:  dns.ClassINET,
		Ttl:    ttl,
	}

	switch r.Type {
	case model.RecordTypeA:
		ip := net.ParseIP(strings.TrimSpace(r.Value))
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("invalid IPv4 address: %s", r.Value)
		}
		return &dns.A{
			Hdr: hdr,
			A:   ip.To4(),
		}, nil

	case model.RecordTypeAAAA:
		ip := net.ParseIP(strings.TrimSpace(r.Value))
		if ip == nil || ip.To16() == nil || ip.To4() != nil {
			return nil, fmt.Errorf("invalid IPv6 address: %s", r.Value)
		}
		return &dns.AAAA{
			Hdr:  hdr,
			AAAA: ip.To16(),
		}, nil

	case model.RecordTypeCNAME:
		return &dns.CNAME{
			Hdr:    hdr,
			Target: EnsureFQDN(r.Value),
		}, nil

	case model.RecordTypeTXT:
		val := strings.Trim(r.Value, "\"")
		// Split long TXT values into 255-byte chunks according to RFC 1035
		var txtChunks []string
		for len(val) > 255 {
			txtChunks = append(txtChunks, val[:255])
			val = val[255:]
		}
		txtChunks = append(txtChunks, val)
		return &dns.TXT{
			Hdr: hdr,
			Txt: txtChunks,
		}, nil

	case model.RecordTypeMX:
		return &dns.MX{
			Hdr:        hdr,
			Preference: r.Priority,
			Mx:         EnsureFQDN(r.Value),
		}, nil

	case model.RecordTypeNS:
		return &dns.NS{
			Hdr: hdr,
			Ns:  EnsureFQDN(r.Value),
		}, nil

	case model.RecordTypePTR:
		return &dns.PTR{
			Hdr: hdr,
			Ptr: EnsureFQDN(r.Value),
		}, nil

	case model.RecordTypeSRV:
		target := EnsureFQDN(r.Value)
		return &dns.SRV{
			Hdr:      hdr,
			Priority: r.Priority,
			Weight:   r.Weight,
			Port:     r.Port,
			Target:   target,
		}, nil

	case model.RecordTypeCAA:
		return parseTextRecord(fqdn, ttl, r.Type, r.Value)

	case model.RecordTypeALIAS:
		// ALIAS represents apex CNAME flattening; if queried directly, return as CNAME
		hdr.Rrtype = dns.TypeCNAME
		return &dns.CNAME{
			Hdr:    hdr,
			Target: EnsureFQDN(r.Value),
		}, nil

	case model.RecordTypeHTTPS, model.RecordTypeSVCB:
		// Let miekg/dns parse the complete RFC 9460 parameter set (alpn,
		// port, ipv4hint, ech, ipv6hint, mandatory, and future keys).
		return parseTextRecord(fqdn, ttl, r.Type, r.Value)

	case model.RecordTypeTLSA, model.RecordTypeSSHFP, model.RecordTypeDS, model.RecordTypeDNSKEY:
		return parseTextRecord(fqdn, ttl, r.Type, r.Value)
	}

	// Generic fallback parse using dns.NewRR
	rrStr := fmt.Sprintf("%s %d IN %s %s", fqdn, ttl, r.Type, r.Value)
	rr, err := dns.NewRR(rrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generic RR '%s': %w", rrStr, err)
	}
	return rr, nil
}

func parseTextRecord(fqdn string, ttl uint32, recordType model.RecordType, value string) (dns.RR, error) {
	rrText := fmt.Sprintf("%s %d IN %s %s", fqdn, ttl, recordType, value)
	rr, err := dns.NewRR(rrText)
	if err != nil {
		return nil, fmt.Errorf("invalid %s record value: %w", recordType, err)
	}
	return rr, nil
}

// BuildNSEC creates NSEC record for denial of existence
func BuildNSEC(name, nextDomain string, types []uint16) *dns.NSEC {
	return &dns.NSEC{
		Hdr: dns.RR_Header{
			Name:   EnsureFQDN(name),
			Rrtype: dns.TypeNSEC,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		NextDomain: EnsureFQDN(nextDomain),
		TypeBitMap: types,
	}
}

// HexToBytes helper
func HexToBytes(s string) []byte {
	b, _ := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	return b
}
