package monitoring

import (
	"context"
	"net"
	"time"

	"github.com/hoaxisr/awg-manager/internal/icmpprobe"
	"github.com/hoaxisr/awg-manager/internal/sys/httpclient"
)

// Prober probes a single host through a specific interface and returns
// latency in milliseconds + success flag. Implementations must be safe for
// concurrent use.
type Prober interface {
	Probe(ctx context.Context, host, ifaceName string, timeout time.Duration) (latencyMs int, ok bool)
}

// TCPProber probes via a bare TCP connect to host:443 with SO_BINDTODEVICE
// and reports the dial duration. The matrix metric has always been TCP RTT:
// the previous HTTPS HEAD prober measured `time_connect - time_namelookup`
// and discarded the TLS exchange — but on softfloat MIPS each discarded
// TLS handshake costs seconds of CPU, so idle routers burned most of their
// awg-manager CPU on throwaway handshakes every matrix tick.
//
// "Reachable" is defined as: TCP connect succeeded before the timeout
// (was: any HTTP status code). For the bare-IP base targets these are
// equivalent in practice. Hostname targets resolve inside the measured
// window, matching the old time_total fallback behaviour.
type TCPProber struct {
	// port is dialed on every probed host; defaults to 443 (overridable
	// in tests, where no fixed port can be listened on).
	port string
}

// NewTCPProber builds a prober dialing the conventional HTTPS port.
func NewTCPProber() *TCPProber {
	return &TCPProber{port: "443"}
}

// Probe opens and immediately closes one TCP connection through ifaceName.
// ok=false on context cancellation or any dial error.
func (p *TCPProber) Probe(ctx context.Context, host, ifaceName string, timeout time.Duration) (int, bool) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout+1*time.Second)
	defer cancel()

	port := p.port
	if port == "" {
		port = "443"
	}
	start := time.Now()
	conn, err := httpclient.DialTCP(timeoutCtx, ifaceName, net.JoinHostPort(host, port), timeout)
	if err != nil {
		return 0, false
	}
	_ = conn.Close()

	latencyMs := int(time.Since(start).Milliseconds())
	if latencyMs < 1 {
		latencyMs = 1
	}
	return latencyMs, true
}

// ICMPProber sends a single native ICMP echo bound to the tunnel
// interface. Used for matrix cells whose target is the tunnel's
// connectivity-check self host AND the tunnel's method is "ping".
type ICMPProber struct {
	Pinger func(ctx context.Context, ifaceName, target string, dnsServers []string) (icmpprobe.Result, error)
}

// NewICMPProber builds an ICMP prober backed by the native icmpprobe.
func NewICMPProber() *ICMPProber {
	return &ICMPProber{Pinger: icmpprobe.ByInterface}
}

// Probe sends a single ICMP echo. ok=false on resolve/socket/timeout error.
func (p *ICMPProber) Probe(ctx context.Context, host, ifaceName string, timeout time.Duration) (int, bool) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := p.Pinger(timeoutCtx, ifaceName, host, nil)
	if err != nil {
		return 0, false
	}
	return res.LatencyMs, true
}
