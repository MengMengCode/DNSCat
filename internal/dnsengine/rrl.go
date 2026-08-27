package dnsengine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"sync"
	"time"
)

type RateBucket struct {
	tokens     float64
	lastUpdate time.Time
	dropped    uint64
}

type RRL struct {
	mu           sync.Mutex
	buckets      map[string]*RateBucket
	rate         float64 // tokens added per second
	burst        float64 // max bucket capacity
	slip         uint64  // 1 in slip packets gets TC=1 (Truncation) instead of drop
	serverSecret []byte
}

func NewRRL(rate float64) *RRL {
	if rate <= 0 {
		rate = 1000 // Default 1000 QPS per client IP
	}
	secret := make([]byte, 16)
	binary.BigEndian.PutUint64(secret[:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(secret[8:], 0x5a5a5a5a5a5a5a5a)

	rrl := &RRL{
		buckets:      make(map[string]*RateBucket),
		rate:         rate,
		burst:        rate * 2,
		slip:         2, // Every 2nd dropped packet is responded with TC=1 to force TCP fallback
		serverSecret: secret,
	}

	// Periodic cleanup of stale client buckets
	go rrl.cleanupWorker()

	return rrl
}

// Allow returns (allowed, isSlip)
func (r *RRL) Allow(ip net.IP) (bool, bool) {
	if ip == nil {
		return true, false
	}
	ipStr := ip.String()

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	b, exists := r.buckets[ipStr]
	if !exists {
		r.buckets[ipStr] = &RateBucket{
			tokens:     r.burst - 1,
			lastUpdate: now,
			dropped:    0,
		}
		return true, false
	}

	// Refill tokens
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens += elapsed * r.rate
	if b.tokens > r.burst {
		b.tokens = r.burst
	}
	b.lastUpdate = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		b.dropped = 0
		return true, false
	}

	// Rate limit exceeded
	b.dropped++
	// SLIP: every Nth dropped packet returns TC=1 so legitimate clients fallback to TCP
	if r.slip > 0 && b.dropped%r.slip == 0 {
		return false, true
	}

	return false, false
}

// ValidateOrGenerateCookie validates Client Cookie and generates Server Cookie (RFC 7873 / RFC 9018)
func (r *RRL) ValidateOrGenerateCookie(clientCookie []byte, clientIP net.IP) ([]byte, bool) {
	if len(clientCookie) != 8 || clientIP == nil {
		return nil, false
	}

	mac := hmac.New(sha256.New, r.serverSecret)
	mac.Write(clientCookie)
	mac.Write(clientIP)
	fullHash := mac.Sum(nil)

	// Server cookie is 8 bytes
	serverCookie := fullHash[:8]
	return serverCookie, true
}

func (r *RRL) cleanupWorker() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for ip, b := range r.buckets {
			if now.Sub(b.lastUpdate) > 5*time.Minute {
				delete(r.buckets, ip)
			}
		}
		r.mu.Unlock()
	}
}
