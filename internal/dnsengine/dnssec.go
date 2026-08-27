package dnsengine

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	"dnscat/internal/model"

	"github.com/miekg/dns"
)

// GenerateDNSSECKeyPair creates a KSK and a ZSK using ECDSA P-256 (Algorithm 13)
func GenerateDNSSECKeyPair(domainID uint, domainName string) ([]model.DNSSECKey, error) {
	domainFQDN := EnsureFQDN(domainName)

	// 1. Generate KSK (Flags: 257)
	kskPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate KSK key: %w", err)
	}
	kskDNSKEY := &dns.DNSKEY{
		Hdr: dns.RR_Header{
			Name:   domainFQDN,
			Rrtype: dns.TypeDNSKEY,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Flags:     257, // KSK
		Protocol:  3,
		Algorithm: dns.ECDSAP256SHA256,
	}
	kskDNSKEY.PublicKey = base64.StdEncoding.EncodeToString(elliptic.Marshal(kskPriv.Curve, kskPriv.PublicKey.X, kskPriv.PublicKey.Y)[1:])
	kskTag := kskDNSKEY.KeyTag()

	// Compute DS record from KSK
	ds := kskDNSKEY.ToDS(dns.SHA256)
	var dsDigest string
	var dsConfig string
	if ds != nil {
		dsDigest = ds.Digest
		dsConfig = fmt.Sprintf("%d %d %d %s", ds.KeyTag, ds.Algorithm, ds.DigestType, ds.Digest)
	}

	kskPrivBytes, _ := x509.MarshalECPrivateKey(kskPriv)
	kskPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kskPrivBytes})

	kskModel := model.DNSSECKey{
		DomainID:   domainID,
		KeyType:    model.KeyTypeKSK,
		Algorithm:  dns.ECDSAP256SHA256,
		Flags:      257,
		KeyTag:     kskTag,
		PublicKey:  kskDNSKEY.PublicKey,
		PrivateKey: string(kskPEM),
		DigestType: dns.SHA256,
		Digest:     dsDigest,
		DSConfig:   dsConfig,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// 2. Generate ZSK (Flags: 256)
	zskPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ZSK key: %w", err)
	}
	zskDNSKEY := &dns.DNSKEY{
		Hdr: dns.RR_Header{
			Name:   domainFQDN,
			Rrtype: dns.TypeDNSKEY,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Flags:     256, // ZSK
		Protocol:  3,
		Algorithm: dns.ECDSAP256SHA256,
	}
	zskDNSKEY.PublicKey = base64.StdEncoding.EncodeToString(elliptic.Marshal(zskPriv.Curve, zskPriv.PublicKey.X, zskPriv.PublicKey.Y)[1:])
	zskTag := zskDNSKEY.KeyTag()

	zskPrivBytes, _ := x509.MarshalECPrivateKey(zskPriv)
	zskPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: zskPrivBytes})

	zskModel := model.DNSSECKey{
		DomainID:   domainID,
		KeyType:    model.KeyTypeZSK,
		Algorithm:  dns.ECDSAP256SHA256,
		Flags:      256,
		KeyTag:     zskTag,
		PublicKey:  zskDNSKEY.PublicKey,
		PrivateKey: string(zskPEM),
		DigestType: dns.SHA256,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	return []model.DNSSECKey{kskModel, zskModel}, nil
}

// ParsePrivateKeyFromPEM parses EC or RSA private key from PEM string
func ParsePrivateKeyFromPEM(pemStr string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("unsupported private key format")
}

// SignRRSet signs an RRSet and returns an RRSIG record
func SignRRSet(rrs []dns.RR, signerKey *dns.DNSKEY, privKey crypto.PrivateKey, signerName string) (*dns.RRSIG, error) {
	if len(rrs) == 0 || signerKey == nil || privKey == nil {
		return nil, fmt.Errorf("invalid arguments for signing")
	}

	signer, ok := privKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("private key does not implement crypto.Signer")
	}

	now := time.Now()
	inception := uint32(now.Add(-1 * time.Hour).Unix())
	expiration := uint32(now.Add(30 * 24 * time.Hour).Unix())

	rrsig := &dns.RRSIG{
		Hdr: dns.RR_Header{
			Name:   rrs[0].Header().Name,
			Rrtype: dns.TypeRRSIG,
			Class:  dns.ClassINET,
			Ttl:    rrs[0].Header().Ttl,
		},
		TypeCovered: rrs[0].Header().Rrtype,
		Algorithm:   signerKey.Algorithm,
		Labels:      uint8(dns.CountLabel(rrs[0].Header().Name)),
		OrigTtl:     rrs[0].Header().Ttl,
		Expiration:  expiration,
		Inception:   inception,
		KeyTag:      signerKey.KeyTag(),
		SignerName:  EnsureFQDN(signerName),
	}

	err := rrsig.Sign(signer, rrs)
	if err != nil {
		return nil, fmt.Errorf("RRSIG signing failed: %w", err)
	}

	return rrsig, nil
}
