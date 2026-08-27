package healthcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var icmpSequence atomic.Uint32

type icmpProbeResult struct {
	address  net.IP
	family   string
	mode     string
	sequence int
	latency  time.Duration
}

type icmpProbeOutcome struct {
	result icmpProbeResult
	err    error
}

// probeICMPEcho performs a real ICMP Echo Request/Reply probe. It never falls
// back to TCP: a target is healthy only after a matching ICMP Echo Reply is
// received. For hostnames with both A and AAAA records, IPv4 and IPv6 are
// probed concurrently and the first valid reply wins.
func probeICMPEcho(target string, timeout time.Duration) (time.Duration, string, error) {
	startedAt := time.Now()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	addresses, err := resolveICMPTargets(ctx, target)
	if err != nil {
		return time.Since(startedAt), "", err
	}

	probeCtx, stopProbes := context.WithCancel(ctx)
	defer stopProbes()

	outcomes := make(chan icmpProbeOutcome, len(addresses))
	for _, address := range addresses {
		address := address
		go func() {
			result, probeErr := probeICMPAddress(probeCtx, address)
			outcomes <- icmpProbeOutcome{result: result, err: probeErr}
		}()
	}

	errorsByAddress := make([]string, 0, len(addresses))
	for range addresses {
		select {
		case outcome := <-outcomes:
			if outcome.err == nil {
				stopProbes()
				detail := fmt.Sprintf(
					"ICMP Echo Reply from %s (%s, %s socket, seq=%d)",
					outcome.result.address.String(),
					outcome.result.family,
					outcome.result.mode,
					outcome.result.sequence,
				)
				return outcome.result.latency, detail, nil
			}
			errorsByAddress = append(errorsByAddress, outcome.err.Error())
		case <-ctx.Done():
			return time.Since(startedAt), "", fmt.Errorf("ICMP Echo timeout after %s", timeout)
		}
	}

	if len(errorsByAddress) == 0 {
		return time.Since(startedAt), "", errors.New("ICMP Echo failed without a reply")
	}
	return time.Since(startedAt), "", fmt.Errorf("ICMP Echo failed: %s", strings.Join(errorsByAddress, "; "))
}

// resolveICMPTargets resolves an IP literal or hostname. At most one IPv4 and
// one IPv6 address are returned, avoiding duplicate probes while preserving
// dual-stack availability.
func resolveICMPTargets(ctx context.Context, target string) ([]net.IPAddr, error) {
	host := strings.TrimSpace(target)
	if host == "" {
		return nil, errors.New("empty ICMP target")
	}
	if strings.Contains(host, "://") {
		return nil, fmt.Errorf("ICMP target must be a hostname or IP address, got %q", target)
	}

	// Accept bracketed IPv6 literals while rejecting host:port input: ICMP has
	// no port and silently accepting one would make the configured target
	// ambiguous.
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	} else if parsedHost, port, err := net.SplitHostPort(host); err == nil && port != "" {
		return nil, fmt.Errorf("ICMP target %q must not include a port (host %q)", target, parsedHost)
	}

	zone := ""
	if percent := strings.LastIndex(host, "%"); percent > 0 {
		zone = host[percent+1:]
		host = host[:percent]
	}

	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip, Zone: zone}}, nil
	}
	if zone != "" {
		return nil, fmt.Errorf("invalid scoped IPv6 address %q", target)
	}

	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, strings.TrimSuffix(host, "."))
	if err != nil {
		return nil, fmt.Errorf("resolve ICMP target %q: %w", target, err)
	}

	var ipv4Address *net.IPAddr
	var ipv6Address *net.IPAddr
	for i := range resolved {
		candidate := resolved[i]
		if candidate.IP == nil {
			continue
		}
		if candidate.IP.To4() != nil {
			if ipv4Address == nil {
				copyOfCandidate := candidate
				ipv4Address = &copyOfCandidate
			}
			continue
		}
		if ipv6Address == nil && candidate.IP.To16() != nil {
			copyOfCandidate := candidate
			ipv6Address = &copyOfCandidate
		}
	}

	addresses := make([]net.IPAddr, 0, 2)
	if ipv4Address != nil {
		addresses = append(addresses, *ipv4Address)
	}
	if ipv6Address != nil {
		addresses = append(addresses, *ipv6Address)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("ICMP target %q did not resolve to an IPv4 or IPv6 address", target)
	}
	return addresses, nil
}

func probeICMPAddress(ctx context.Context, target net.IPAddr) (icmpProbeResult, error) {
	result := icmpProbeResult{address: target.IP}
	isIPv4 := target.IP.To4() != nil

	var (
		rawNetwork          string
		unprivilegedNetwork string
		listenAddress       string
		protocol            int
		requestType         icmp.Type
		replyType           icmp.Type
	)
	if isIPv4 {
		result.family = "IPv4"
		rawNetwork = "ip4:icmp"
		unprivilegedNetwork = "udp4"
		listenAddress = "0.0.0.0"
		protocol = 1
		requestType = ipv4.ICMPTypeEcho
		replyType = ipv4.ICMPTypeEchoReply
	} else {
		result.family = "IPv6"
		rawNetwork = "ip6:ipv6-icmp"
		unprivilegedNetwork = "udp6"
		listenAddress = "::"
		protocol = 58
		requestType = ipv6.ICMPTypeEchoRequest
		replyType = ipv6.ICMPTypeEchoReply
	}

	conn, mode, unprivileged, err := openICMPSocket(rawNetwork, unprivilegedNetwork, listenAddress)
	if err != nil {
		return result, fmt.Errorf("%s (%s): %w", target.String(), result.family, err)
	}
	defer conn.Close()
	result.mode = mode

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(5 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return result, fmt.Errorf("%s (%s): set ICMP deadline: %w", target.String(), result.family, err)
	}

	// Cancel an in-flight ReadFrom immediately after another address succeeds.
	stopDeadlineWatcher := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-stopDeadlineWatcher:
		}
	}()
	defer close(stopDeadlineWatcher)

	sequence := int(icmpSequence.Add(1) & 0xffff)
	if sequence == 0 {
		sequence = 1
	}
	result.sequence = sequence
	identifier := os.Getpid() & 0xffff
	payload := []byte(fmt.Sprintf("dnscat-icmp:%d:%d", time.Now().UnixNano(), sequence))
	request := icmp.Message{
		Type: requestType,
		Code: 0,
		Body: &icmp.Echo{ID: identifier, Seq: sequence, Data: payload},
	}
	wire, err := request.Marshal(nil)
	if err != nil {
		return result, fmt.Errorf("%s (%s): marshal ICMP request: %w", target.String(), result.family, err)
	}

	var destination net.Addr
	if unprivileged {
		destination = &net.UDPAddr{IP: target.IP, Zone: target.Zone}
	} else {
		destination = &net.IPAddr{IP: target.IP, Zone: target.Zone}
	}

	sentAt := time.Now()
	if _, err := conn.WriteTo(wire, destination); err != nil {
		return result, fmt.Errorf("%s (%s): send ICMP Echo Request: %w", target.String(), result.family, err)
	}

	buffer := make([]byte, 1500)
	for {
		n, peer, readErr := conn.ReadFrom(buffer)
		if readErr != nil {
			if ctx.Err() != nil || isTimeoutError(readErr) {
				return result, fmt.Errorf("%s (%s): ICMP Echo Reply timeout", target.String(), result.family)
			}
			return result, fmt.Errorf("%s (%s): read ICMP reply: %w", target.String(), result.family, readErr)
		}

		peerIP := addressIP(peer)
		if peerIP != nil && !peerIP.Equal(target.IP) {
			continue
		}

		reply, parseErr := icmp.ParseMessage(protocol, buffer[:n])
		if parseErr != nil || reply.Type != replyType {
			continue
		}
		echo, ok := reply.Body.(*icmp.Echo)
		if !ok || echo.Seq != sequence || !bytes.Equal(echo.Data, payload) {
			continue
		}

		// Linux ping sockets may rewrite the ICMP identifier, therefore the
		// unique sequence + payload are used for correlation in both modes.
		result.latency = time.Since(sentAt)
		return result, nil
	}
}

// openICMPSocket first uses a raw ICMP socket (requires CAP_NET_RAW/root). If
// the platform rejects it, Linux's unprivileged datagram ICMP ping socket is
// attempted. Both modes send real ICMP packets; there is deliberately no TCP
// connectivity fallback.
func openICMPSocket(rawNetwork, unprivilegedNetwork, listenAddress string) (*icmp.PacketConn, string, bool, error) {
	rawConn, rawErr := icmp.ListenPacket(rawNetwork, listenAddress)
	if rawErr == nil {
		return rawConn, "raw", false, nil
	}

	unprivilegedConn, unprivilegedErr := icmp.ListenPacket(unprivilegedNetwork, listenAddress)
	if unprivilegedErr == nil {
		return unprivilegedConn, "unprivileged", true, nil
	}

	return nil, "", false, fmt.Errorf(
		"cannot open ICMP socket (raw: %v; unprivileged: %v)",
		rawErr,
		unprivilegedErr,
	)
}

func addressIP(address net.Addr) net.IP {
	switch value := address.(type) {
	case *net.IPAddr:
		return value.IP
	case *net.UDPAddr:
		return value.IP
	default:
		return nil
	}
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
