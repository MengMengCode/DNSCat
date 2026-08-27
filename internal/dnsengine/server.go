package dnsengine

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/config"
	"dnscat/internal/geo"

	"github.com/gin-gonic/gin"
	"github.com/miekg/dns"
)

type Server struct {
	cfg       *config.Config
	rrl       *RRL
	udpServer *dns.Server
	tcpServer *dns.Server
	tlsServer *dns.Server
	zoneStore *ZoneStore
}

var GlobalServer *Server

func NewServer(cfg *config.Config, store *ZoneStore) *Server {
	s := &Server{
		cfg:       cfg,
		rrl:       NewRRL(float64(cfg.DNS.RateLimit)),
		zoneStore: store,
	}
	GlobalServer = s
	return s
}

// Start launches the UDP, TCP, and DoT DNS servers
func (s *Server) Start() error {
	// 1. UDP Server
	udpAddr := fmt.Sprintf(":%d", s.cfg.DNS.UDPPort)
	s.udpServer = &dns.Server{
		Addr:    udpAddr,
		Net:     "udp",
		Handler: dns.HandlerFunc(s.ServeDNS),
		UDPSize: 4096,
	}
	go func() {
		log.Printf("[DNS Server] Starting hardened UDP nameserver on %s (Anti-Hijack & Anti-Poisoning Enabled)", udpAddr)
		if err := s.udpServer.ListenAndServe(); err != nil {
			log.Printf("[DNS Server] UDP server error: %v", err)
		}
	}()

	// 2. TCP Server
	tcpAddr := fmt.Sprintf(":%d", s.cfg.DNS.TCPPort)
	s.tcpServer = &dns.Server{
		Addr:    tcpAddr,
		Net:     "tcp",
		Handler: dns.HandlerFunc(s.ServeDNS),
	}
	go func() {
		log.Printf("[DNS Server] Starting TCP nameserver on %s", tcpAddr)
		if err := s.tcpServer.ListenAndServe(); err != nil {
			log.Printf("[DNS Server] TCP server error: %v", err)
		}
	}()

	// 3. DoT (DNS over TLS) Server
	if s.cfg.DNS.EnableTLS && s.cfg.DNS.TLSCertFile != "" && s.cfg.DNS.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.DNS.TLSCertFile, s.cfg.DNS.TLSKeyFile)
		if err == nil {
			tlsAddr := fmt.Sprintf(":%d", s.cfg.DNS.TLSPort)
			s.tlsServer = &dns.Server{
				Addr:      tlsAddr,
				Net:       "tcp-tls",
				Handler:   dns.HandlerFunc(s.ServeDNS),
				TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
			}
			go func() {
				log.Printf("[DNS Server] Starting DoT (TLS) nameserver on %s", tlsAddr)
				if err := s.tlsServer.ListenAndServe(); err != nil {
					log.Printf("[DNS Server] TLS server error: %v", err)
				}
			}()
		} else {
			log.Printf("[DNS Server] Failed to load TLS cert for DoT: %v", err)
		}
	}

	return nil
}

// Stop gracefully terminates DNS listeners
func (s *Server) Stop() {
	if s.udpServer != nil {
		_ = s.udpServer.Shutdown()
	}
	if s.tcpServer != nil {
		_ = s.tcpServer.Shutdown()
	}
	if s.tlsServer != nil {
		_ = s.tlsServer.Shutdown()
	}
}

// ServeDNS processes inbound UDP/TCP/TLS DNS queries with security checks
func (s *Server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	if req == nil || len(req.Question) == 0 {
		return
	}

	clientHost, _, _ := net.SplitHostPort(w.RemoteAddr().String())
	clientIP := net.ParseIP(clientHost)
	isTCP := strings.HasPrefix(w.RemoteAddr().Network(), "tcp")

	// Rate Limiting check (TCP is exempt from UDP amplification rate limit)
	if !isTCP {
		allowed, isSlip := s.rrl.Allow(clientIP)
		if !allowed {
			if isSlip {
				// SLIP: Return Truncated response to force legitimate resolver fallback to TCP
				resp := new(dns.Msg)
				resp.SetReply(req)
				resp.Truncated = true
				_ = w.WriteMsg(resp)
				return
			}
			// Dropped to protect against amplification DDoS
			return
		}
	}

	// 逐域名安全防护：先定位查询命中的托管区域，再按该区域的策略判定。
	// 放在全局 RRL 之后、解析之前，被拦截的查询不会进入区域查找与签名等开销。
	q := req.Question[0]
	zone, _ := s.zoneStore.FindZone(q.Name)
	decision := GlobalSecurity.Evaluate(zone, q.Name, q.Qtype, clientIP, isTCP)
	switch decision.Action {
	case SecActionDrop:
		// 静默丢弃：不产生任何应答，避免成为反射放大的帮凶。
		return
	case SecActionRefuse:
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Rcode = dns.RcodeRefused
		resp.RecursionAvailable = false
		_ = w.WriteMsg(resp)
		return
	case SecActionTruncate:
		// SLIP：回 TC=1 让正常解析器改走 TCP，攻击者的伪造源则拿不到放大收益。
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Truncated = true
		_ = w.WriteMsg(resp)
		return
	case SecActionMinimalANY:
		zoneName := ""
		if zone != nil {
			zoneName = zone.Domain.Name
		}
		resp := BuildMinimalANYResponse(req, zoneName)
		GlobalSecurity.RecordOutcome(zone, q.Name, q.Qtype, clientIP, resp.Rcode)
		_ = w.WriteMsg(resp)
		return
	}

	// 记录真实解析处理耗时，供节点心跳上报平均响应延迟。
	started := time.Now()
	resp := s.ProcessQuery(req, clientIP)
	GlobalTelemetry.RecordLatency(time.Since(started))
	// 解析结果回灌安全引擎：统计总量，并做 NXDOMAIN / 随机子域攻击判定。
	GlobalSecurity.RecordOutcome(zone, q.Name, q.Qtype, clientIP, resp.Rcode)
	_ = w.WriteMsg(resp)
}

// ProcessQuery runs the hardened authoritative lookup pipeline
func (s *Server) ProcessQuery(req *dns.Msg, clientIP net.IP) *dns.Msg {
	q := req.Question[0]
	rawQName := q.Name
	qtype := q.Qtype

	// Extract ECS (RFC 7871)
	ecsIP, ecsMask := ExtractECS(req)

	// Check if DNSSEC DO bit is requested
	doDNSSEC := false
	var clientCookie []byte
	if opt := req.IsEdns0(); opt != nil {
		doDNSSEC = opt.Do()
		for _, o := range opt.Option {
			if cookie, ok := o.(*dns.EDNS0_COOKIE); ok {
				clientCookie = HexToBytes(cookie.Cookie)
			}
		}
	}

	// 1. In-memory Authoritative Zone lookup
	resp := s.zoneStore.Query(rawQName, qtype, clientIP, ecsIP, doDNSSEC)
	// SetReply copies the request ID/opcode/question but resets the response code
	// to NOERROR. Preserve the authoritative lookup result (NXDOMAIN/REFUSED).
	rcode := resp.Rcode
	resp.SetReply(req)
	resp.Rcode = rcode

	// 2. Strict Authoritative Response Flags (Anti-Hijack & Anti-Open-Resolver)
	if resp.Rcode == dns.RcodeSuccess || resp.Rcode == dns.RcodeNameError {
		resp.Authoritative = true // Explicitly Authoritative (AA=1)
	}
	resp.RecursionAvailable = false // Not an open recursive resolver (RA=0)
	resp.RecursionDesired = req.RecursionDesired

	// 3. RFC 5452: 0x20 Bit Case-Preserving Randomization
	// Ensure Question in response exactly matches the query case
	if len(resp.Question) > 0 {
		resp.Question[0].Name = rawQName
	}
	// Also preserve QNAME case on answer RRs matching the query name
	for _, ans := range resp.Answer {
		if strings.EqualFold(ans.Header().Name, rawQName) {
			ans.Header().Name = rawQName
		}
	}

	// 4. EDNS0 & Cookie Response (RFC 7873 / RFC 9018)
	if req.IsEdns0() != nil {
		var opt *dns.OPT
		if resp.IsEdns0() != nil {
			opt = resp.IsEdns0()
		} else {
			resp.SetEdns0(4096, doDNSSEC)
			opt = resp.IsEdns0()
		}

		if ecsIP != nil {
			AttachECSResponse(resp, req, ecsMask)
		}

		// Handle DNS Cookie
		if len(clientCookie) >= 8 {
			if srvCookie, ok := s.rrl.ValidateOrGenerateCookie(clientCookie[:8], clientIP); ok {
				cookieOpt := &dns.EDNS0_COOKIE{
					Code:   dns.EDNS0COOKIE,
					Cookie: fmt.Sprintf("%x%x", clientCookie[:8], srvCookie),
				}
				opt.Option = append(opt.Option, cookieOpt)
			}
		}
	}

	// 5. Record Real Telemetry
	countryCode := "default"
	if ecsIP != nil {
		countryCode = geo.MatchCountry(ecsIP)
	} else if clientIP != nil {
		countryCode = geo.MatchCountry(clientIP)
	}

	// 归属到命中的托管区域，未命中任何区域时只累计全局遥测。
	zoneName := ""
	if cz, _ := s.zoneStore.FindZone(rawQName); cz != nil {
		zoneName = cz.Domain.Name
	}
	GlobalTelemetry.RecordZoneQuery(zoneName, qtype, countryCode, resp.Rcode == dns.RcodeRefused)

	return resp
}

// ProcessQuerySecured 在 ProcessQuery 之上套一层逐域名安全防护，供 DoH 这类
// 无法「静默丢弃」的传输方式复用：命中拦截规则时统一返回 REFUSED，
// ANY 极简策略则返回 RFC 8482 应答。
func (s *Server) ProcessQuerySecured(req *dns.Msg, clientIP net.IP) *dns.Msg {
	if req == nil || len(req.Question) == 0 {
		return s.ProcessQuery(req, clientIP)
	}
	q := req.Question[0]
	zone, _ := s.zoneStore.FindZone(q.Name)
	// DoH 承载在 TCP/TLS 之上，按 TCP 语义评估（不适用抗放大的丢包与 TC=1）。
	decision := GlobalSecurity.Evaluate(zone, q.Name, q.Qtype, clientIP, true)
	switch decision.Action {
	case SecActionDrop, SecActionRefuse, SecActionTruncate:
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Rcode = dns.RcodeRefused
		resp.RecursionAvailable = false
		return resp
	case SecActionMinimalANY:
		zoneName := ""
		if zone != nil {
			zoneName = zone.Domain.Name
		}
		resp := BuildMinimalANYResponse(req, zoneName)
		GlobalSecurity.RecordOutcome(zone, q.Name, q.Qtype, clientIP, resp.Rcode)
		return resp
	}
	resp := s.ProcessQuery(req, clientIP)
	GlobalSecurity.RecordOutcome(zone, q.Name, q.Qtype, clientIP, resp.Rcode)
	return resp
}

// DoHHandler handles DNS over HTTPS queries (RFC 8484 and JSON format)
func (s *Server) DoHHandler(c *gin.Context) {
	clientIP := net.ParseIP(c.ClientIP())

	// 1. JSON format support (e.g. GET /dns-query?name=example.com&type=A)
	if c.Request.Method == http.MethodGet && c.Query("dns") == "" && c.Query("name") != "" {
		name := c.Query("name")
		typeStr := strings.ToUpper(c.DefaultQuery("type", "A"))
		qtype := dns.StringToType[typeStr]
		if qtype == 0 {
			if num, err := strconv.Atoi(typeStr); err == nil {
				qtype = uint16(num)
			} else {
				qtype = dns.TypeA
			}
		}

		req := new(dns.Msg)
		req.SetQuestion(EnsureFQDN(name), qtype)
		req.SetEdns0(4096, c.Query("do") == "1" || c.Query("cd") == "1")

		resp := s.ProcessQuerySecured(req, clientIP)

		// Convert to Cloudflare/Google style JSON response
		type JSONAnswer struct {
			Name string `json:"name"`
			Type uint16 `json:"type"`
			TTL  uint32 `json:"TTL"`
			Data string `json:"data"`
		}
		type JSONResponse struct {
			Status   int  `json:"Status"`
			TC       bool `json:"TC"`
			RD       bool `json:"RD"`
			RA       bool `json:"RA"`
			AD       bool `json:"AD"`
			CD       bool `json:"CD"`
			Question []struct {
				Name string `json:"name"`
				Type uint16 `json:"type"`
			} `json:"Question"`
			Answer    []JSONAnswer `json:"Answer,omitempty"`
			Authority []JSONAnswer `json:"Authority,omitempty"`
		}

		jsonResp := JSONResponse{
			Status: resp.Rcode,
			TC:     resp.Truncated,
			RD:     resp.RecursionDesired,
			RA:     resp.RecursionAvailable,
			AD:     resp.AuthenticatedData,
			CD:     resp.CheckingDisabled,
		}
		jsonResp.Question = append(jsonResp.Question, struct {
			Name string `json:"name"`
			Type uint16 `json:"type"`
		}{Name: qnameToDisplay(qname(name)), Type: qtype})

		for _, ans := range resp.Answer {
			jsonResp.Answer = append(jsonResp.Answer, JSONAnswer{
				Name: ans.Header().Name,
				Type: ans.Header().Rrtype,
				TTL:  ans.Header().Ttl,
				Data: formatRRData(ans),
			})
		}
		for _, auth := range resp.Ns {
			jsonResp.Authority = append(jsonResp.Authority, JSONAnswer{
				Name: auth.Header().Name,
				Type: auth.Header().Rrtype,
				TTL:  auth.Header().Ttl,
				Data: formatRRData(auth),
			})
		}

		c.JSON(http.StatusOK, jsonResp)
		return
	}

	// 2. Wireformat RFC 8484 (GET ?dns=base64url or POST application/dns-message)
	var rawMsg []byte
	if c.Request.Method == http.MethodGet {
		dnsParam := c.Query("dns")
		if dnsParam == "" {
			c.String(http.StatusBadRequest, "Missing dns parameter")
			return
		}
		var err error
		rawMsg, err = base64.RawURLEncoding.DecodeString(dnsParam)
		if err != nil {
			rawMsg, err = base64.URLEncoding.DecodeString(dnsParam)
			if err != nil {
				c.String(http.StatusBadRequest, "Invalid base64 encoding")
				return
			}
		}
	} else if c.Request.Method == http.MethodPost {
		var err error
		rawMsg, err = io.ReadAll(c.Request.Body)
		if err != nil || len(rawMsg) == 0 {
			c.String(http.StatusBadRequest, "Empty or invalid body")
			return
		}
	}

	req := new(dns.Msg)
	if err := req.Unpack(rawMsg); err != nil {
		c.String(http.StatusBadRequest, "Malformed DNS message")
		return
	}

	resp := s.ProcessQuerySecured(req, clientIP)
	respBytes, err := resp.Pack()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to pack response")
		return
	}

	c.Data(http.StatusOK, "application/dns-message", respBytes)
}

func qname(n string) string {
	return EnsureFQDN(n)
}

func qnameToDisplay(n string) string {
	return strings.TrimSuffix(n, ".")
}

func formatRRData(rr dns.RR) string {
	switch v := rr.(type) {
	case *dns.A:
		return v.A.String()
	case *dns.AAAA:
		return v.AAAA.String()
	case *dns.CNAME:
		return v.Target
	case *dns.TXT:
		return strings.Join(v.Txt, " ")
	case *dns.MX:
		return fmt.Sprintf("%d %s", v.Preference, v.Mx)
	case *dns.NS:
		return v.Ns
	case *dns.SOA:
		return fmt.Sprintf("%s %s %d %d %d %d %d", v.Ns, v.Mbox, v.Serial, v.Refresh, v.Retry, v.Expire, v.Minttl)
	case *dns.SRV:
		return fmt.Sprintf("%d %d %d %s", v.Priority, v.Weight, v.Port, v.Target)
	case *dns.CAA:
		return fmt.Sprintf("%d %s \"%s\"", v.Flag, v.Tag, v.Value)
	default:
		return rr.String()
	}
}
