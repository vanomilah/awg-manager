package router

import (
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestEnrichMaterializedConfig_NativeProxyGroupsAndMixedInbound(t *testing.T) {
	store := newTestSettingsStore(t, storage.SingboxRouterSettings{
		RoutingEngine:   "sing-box",
		MihomoMixedPort: 1099,
		ProxyGroups: []storage.ProxyGroup{
			{
				Name:     "fallback-group",
				Type:     "fallback",
				Proxies:  []string{"node1", "node2", "DIRECT"},
				URL:      "https://cp.cloudflare.com",
				Interval: 60,
			},
			{
				Name:     "lb-group",
				Type:     "load-balance",
				Strategy: "consistent-hashing",
				Proxies:  []string{"node1", "node2"},
				URL:      "https://cp.cloudflare.com",
				Interval: 30,
			},
			{
				Name:     "urltest-group",
				Type:     "url-test",
				Proxies:  []string{"node1", "node2"},
				Interval: 120,
			},
			{
				Name:    "select-group",
				Type:    "select",
				Proxies: []string{"node1", "node2", "REJECT"},
			},
		},
	})

	svc := &ServiceImpl{
		deps: Deps{
			Settings: store,
		},
	}

	cfg := &RouterConfig{
		Outbounds: []Outbound{
			{Type: "direct", Tag: "direct"},
			{Type: "block", Tag: "block"},
		},
		Route: Route{
			Rules: []Rule{},
		},
	}

	svc.enrichMaterializedConfig(cfg)

	// Check outbounds
	outboundMap := make(map[string]Outbound)
	for _, o := range cfg.Outbounds {
		outboundMap[o.Tag] = o
	}

	// Fallback
	fb, ok := outboundMap["fallback-group"]
	if !ok || fb.Type != "fallback" {
		t.Fatalf("expected fallback-group of type fallback, got: %+v", fb)
	}
	if len(fb.Outbounds) != 3 || fb.Outbounds[2] != "direct" {
		t.Fatalf("expected DIRECT normalized to direct in outbounds, got: %+v", fb.Outbounds)
	}
	if fb.Interval != "60s" {
		t.Fatalf("expected interval 60s, got %s", fb.Interval)
	}
	if !fb.InterruptExistConnections {
		t.Fatalf("expected InterruptExistConnections true for fallback group")
	}

	// Load-Balance
	lb, ok := outboundMap["lb-group"]
	if !ok || lb.Type != "load-balance" {
		t.Fatalf("expected lb-group of type load-balance, got: %+v", lb)
	}
	if lb.Strategy != "consistent-hashing" {
		t.Fatalf("expected consistent-hashing strategy, got %s", lb.Strategy)
	}
	if lb.Interval != "30s" {
		t.Fatalf("expected interval 30s, got %s", lb.Interval)
	}

	// URLTest
	ut, ok := outboundMap["urltest-group"]
	if !ok || ut.Type != "urltest" {
		t.Fatalf("expected urltest-group of type urltest, got: %+v", ut)
	}
	if ut.Interval != "120s" {
		t.Fatalf("expected interval 120s, got %s", ut.Interval)
	}

	// Selector
	sel, ok := outboundMap["select-group"]
	if !ok || sel.Type != "selector" {
		t.Fatalf("expected select-group of type selector, got: %+v", sel)
	}
	if len(sel.Outbounds) != 3 || sel.Outbounds[2] != "block" {
		t.Fatalf("expected REJECT normalized to block in outbounds, got: %+v", sel.Outbounds)
	}
	if sel.Default != "node1" {
		t.Fatalf("expected default node1, got %s", sel.Default)
	}
}


