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
	if len(opts) != 4 { // direct + firc:router + 2 tunnels
		t.Fatalf("expected 4 options, got %d", len(opts))
	}

	// Direct should be first
	if opts[0].Kind != EgressKindDirect || opts[0].ID != "direct" {
		t.Errorf("expected direct first, got %+v", opts[0])
	}
	// FIRC router outbound second
	if opts[1].Kind != EgressKindRouterOutbound || opts[1].ID != "firc:router" || opts[1].Owner != "firc" {
		t.Errorf("expected firc:router second, got %+v", opts[1])
	}

	// Never offer mihomo:1099 in ListOptions
	for _, o := range opts {
		if o.ID == "mihomo:1099" || o.Owner == "mihomo" {
			t.Errorf("legacy mihomo:1099 must never be offered in ListOptions: %+v", o)
		}
	}

	// Telegram server kind omits firc:router
	tgOpts := adapter.ListOptionsForServer(ctx, "tgwebproxy")
	for _, o := range tgOpts {
		if o.ID == "firc:router" {
			t.Errorf("expected firc:router omitted for tgwebproxy, got %+v", o)
		}
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

func TestAdapter_ListOptions_FilteringAndCurrentPreservation(t *testing.T) {
	cat := &mockCatalog{
		entries: []routing.TunnelEntry{
			{
				ID:        "Wireguard2",
				Name:      "AWGM Server",
				Iface:     "nwg2",
				Type:      "wg",
				Server:    true, // Server inbound interface - MUST be hidden
				Status:    "up",
				Available: true,
			},
			{
				ID:        "wan:ISP",
				Name:      "ISP WAN",
				Iface:     "eth3",
				Type:      "wan", // WAN duplicate of direct - MUST be hidden
				Status:    "up",
				Available: true,
			},
			{
				ID:        "internal:dns",
				Name:      "DNS Outbound",
				Iface:     "",
				Type:      "dns", // Internal service - MUST be hidden
				Status:    "up",
				Available: true,
			},
			{
				ID:        "sb-user-tun",
				Name:      "Sing-Box VPN",
				Iface:     "opkgtun0",
				Type:      "sing-box", // User tunnel via sing-box - MUST NOT be filtered out by owner string
				Server:    false,
				Status:    "up",
				Available: true,
			},
			{
				ID:        "sb-user-tun-dup",
				Name:      "Sing-Box VPN Dup",
				Iface:     "opkgtun0", // Duplicate interface - MUST be deduplicated
				Type:      "external",
				Server:    false,
				Status:    "up",
				Available: true,
			},
		},
	}

	adapter := NewAdapter(cat)
	ctx := context.Background()

	opts := adapter.ListOptionsForServer(ctx, "xray")
	for _, o := range opts {
		if o.ID == "Wireguard2" || o.Iface == "nwg2" {
			t.Errorf("server inbound interface must be excluded from egress options: %+v", o)
		}
		if o.ID == "wan:ISP" || o.Owner == "wan" {
			t.Errorf("wan entry must be excluded (already represented by direct): %+v", o)
		}
		if o.ID == "internal:dns" || o.Owner == "dns" {
			t.Errorf("internal service entry must be excluded: %+v", o)
		}
		if o.ID == "sb-user-tun-dup" {
			t.Errorf("duplicate interface entry must be deduplicated: %+v", o)
		}
	}

	foundSingboxUserTun := false
	for _, o := range opts {
		if o.ID == "sb-user-tun" {
			foundSingboxUserTun = true
			if o.Name != "Sing-Box VPN" {
				t.Errorf("expected clean user-friendly name 'Sing-Box VPN', got %q", o.Name)
			}
			if o.State != EgressStateAvailable {
				t.Errorf("expected state available, got %q", o.State)
			}
		}
	}
	if !foundSingboxUserTun {
		t.Fatalf("expected working user tunnel with owner 'sing-box' to be included in egress options")
	}

	// Preserve unavailable / missing current egress with warning
	optsWithCurrent := adapter.ListOptionsForServerWithCurrent(ctx, "xray", "missing_tun_99", "mihomo:1099")
	var foundMissing, foundLegacy bool
	for _, o := range optsWithCurrent {
		if o.ID == "missing_tun_99" {
			foundMissing = true
			if o.Available || !o.Current || o.State != EgressStateUnavailable {
				t.Errorf("expected missing current egress to be marked unavailable+current, got %+v", o)
			}
		}
		if o.ID == "mihomo:1099" {
			foundLegacy = true
			if o.Available || !o.Current || o.State != EgressStateUnsupported {
				t.Errorf("expected legacy current egress to be marked unsupported+current, got %+v", o)
			}
		}
	}
	if !foundMissing || !foundLegacy {
		t.Fatalf("expected missing and legacy current egresses to be preserved in options: missing=%v legacy=%v", foundMissing, foundLegacy)
	}

	// Reject resolving server inbound interface
	if _, err := adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "Wireguard2"}); err == nil {
		t.Errorf("expected Resolve to reject server inbound interface Wireguard2")
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

	// 2. FIRC Router Outbound without provider -> Fail-closed!
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "firc:router"})
	if err == nil {
		t.Fatalf("expected firc:router resolve to fail-closed when no provider is configured")
	}

	// 2a. FIRC Router Outbound with active provider -> OK
	adapter.SetFircStateProvider(func(ctx context.Context) (bool, string) {
		return true, ""
	})
	res, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "firc:router"})
	if err != nil || res.Mode != "router" {
		t.Fatalf("expected firc:router resolve for xray when provider is healthy, got: %+v, %v", res, err)
	}

	// 2b. FIRC Router Outbound when FIRC is unavailable (e.g. fircd stopped or settings error) -> Error (never silently fallback)
	adapter.SetFircStateProvider(func(ctx context.Context) (bool, string) {
		return false, "Служба маршрутизации fircd не запущена"
	})
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "firc:router"})
	if err == nil {
		t.Fatalf("expected firc:router resolve to fail when FIRC is unavailable")
	}
	adapter.SetFircStateProvider(nil)

	// 3. FIRC Router Outbound for Telegram -> Error!
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "tgwebproxy", EgressID: "firc:router"})
	if err == nil {
		t.Fatalf("expected firc:router resolve to fail for telegram")
	}

	// 3b. Legacy mihomo:1099 rejected for new configurations
	_, err = adapter.Resolve(ctx, ResolveRequest{ServerKind: "xray", EgressID: "mihomo:1099"})
	if err == nil {
		t.Fatalf("expected legacy mihomo:1099 resolve to fail for new configurations")
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
