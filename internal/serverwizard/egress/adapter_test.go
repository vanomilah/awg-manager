package egress

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/routing"
)

type mockCatalog struct {
	entries []routing.TunnelEntry
}

func (m *mockCatalog) ListAll(ctx context.Context) []routing.TunnelEntry {
	return m.entries
}

func TestAdapter_ListOptions(t *testing.T) {
	cat := &mockCatalog{
		entries: []routing.TunnelEntry{
			{
				ID:        "awgm0",
				Name:      "AWG-Server",
				Iface:     "awgm0",
				Type:      "managed",
				Status:    "running",
				Available: true,
			},
			{
				ID:        "Wireguard0",
				Name:      "Dacha",
				Iface:     "nwg1",
				Type:      "system",
				Status:    "down",
				Available: false,
			},
		},
	}

	adapter := NewAdapter(cat)
	ctx := context.Background()

	opts := adapter.ListOptions(ctx)
	if len(opts) != 4 { // direct + mihomo:1099 + 2 tunnels
		t.Fatalf("expected 4 options, got %d", len(opts))
	}

	// Direct should be first
	if opts[0].Kind != EgressKindDirect || opts[0].ID != "direct" {
		t.Errorf("expected direct first, got %+v", opts[0])
	}
	// Socks second
	if opts[1].Kind != EgressKindSocks || opts[1].ID != "mihomo:1099" {
		t.Errorf("expected socks second, got %+v", opts[1])
	}

	// FindOption
	opt, ok := adapter.FindOption(ctx, "Wireguard0")
	if !ok {
		t.Fatalf("expected Wireguard0 found")
	}
	if opt.Available {
		t.Errorf("expected Wireguard0 to be unavailable")
	}
	if opt.DegradedMsg == "" {
		t.Errorf("expected DegradedMsg on down tunnel")
	}

	// EgressSummary
	s1 := adapter.EgressSummary(ctx)
	if s1 == "" {
		t.Errorf("expected non-empty summary")
	}

	adapter.BumpGeneration()
	s2 := adapter.EgressSummary(ctx)
	if s1 == s2 {
		t.Errorf("expected summary to change after bump generation")
	}
}

func TestAdapter_Resolve(t *testing.T) {
	cat := &mockCatalog{
		entries: []routing.TunnelEntry{
			{
				ID:        "Wireguard0",
				Name:      "Dacha",
				Iface:     "nwg1",
				Available: true,
				Status:    "running",
			},
			{
				ID:        "Degraded0",
				Name:      "Broken",
				Iface:     "nwg2",
				Available: false,
				Status:    "down",
			},
		},
	}

	adapter := NewAdapter(cat)
	// Mock dialer to simulate :1099 available
	adapter.SetDialTimeout(func(network, address string, timeout time.Duration) (net.Conn, error) {
		return &mockConn{}, nil
	})

	ctx := context.Background()

	// 1. Direct
	res, err := adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "direct"})
	if err != nil || res.Mode != "direct" {
		t.Fatalf("expected direct resolve, got: %+v, %v", res, err)
	}

	// 2. Socks for Xray -> OK
	res, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "mihomo:1099"})
	if err != nil || res.Mode != "socks" || res.SocksPort != 1099 {
		t.Fatalf("expected socks resolve for xray, got: %+v, %v", res, err)
	}

	// 3. Socks for Telegram -> Error!
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "tgwebproxy", EgressID: "mihomo:1099"})
	if err == nil {
		t.Fatalf("expected socks resolve to fail for telegram")
	}

	// 4. Available tunnel -> Interface
	res, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "Wireguard0"})
	if err != nil || res.Mode != "interface" || res.Interface != "nwg1" {
		t.Fatalf("expected interface resolve for Wireguard0, got: %+v, %v", res, err)
	}

	// 5. Degraded tunnel -> Error!
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "Degraded0"})
	if err == nil {
		t.Fatalf("expected degraded tunnel resolve to fail")
	}
}

type mockConn struct {
	net.Conn
}

func (m *mockConn) Close() error {
	return nil
}
