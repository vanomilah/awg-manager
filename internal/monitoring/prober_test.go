package monitoring

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/icmpprobe"
)

// TCPProber: ok=true + positive latency on successful connect, ok=false on
// refused/unreachable. No interface binding in tests (empty ifaceName).
func TestTCPProber_ConnectSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p := &TCPProber{port: port}
	ms, ok := p.Probe(context.Background(), "127.0.0.1", "", 5*time.Second)
	if !ok {
		t.Fatal("Probe() ok = false, want true for listening port")
	}
	if ms < 1 {
		t.Errorf("latency = %d, want >= 1", ms)
	}
}

func TestTCPProber_ConnectRefused(t *testing.T) {
	// Grab a free port and close the listener so the connect is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()

	p := &TCPProber{port: port}
	if _, ok := p.Probe(context.Background(), "127.0.0.1", "", 2*time.Second); ok {
		t.Fatal("Probe() ok = true, want false for closed port")
	}
}

// ICMPProber maps icmpprobe results to (latency, ok).
func TestICMPProber_Probe(t *testing.T) {
	cases := []struct {
		name   string
		res    icmpprobe.Result
		err    error
		wantOK bool
		wantMs int
	}{
		{name: "success", res: icmpprobe.Result{LatencyMs: 14}, wantOK: true, wantMs: 14},
		{name: "probe error means failure", err: errors.New("no reply"), wantOK: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &ICMPProber{Pinger: func(context.Context, string, string, []string) (icmpprobe.Result, error) {
				return c.res, c.err
			}}
			ms, ok := p.Probe(context.Background(), "1.1.1.1", "wg0", 5*time.Second)
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v", ok, c.wantOK)
			}
			if c.wantOK && ms != c.wantMs {
				t.Errorf("latency = %d, want %d", ms, c.wantMs)
			}
		})
	}
}
