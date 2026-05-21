package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20-router.json")

	cfg := &RouterConfig{
		Inbounds: []Inbound{{
			Type: "tproxy", Tag: "tproxy-in", Listen: "127.0.0.1",
			ListenPort: 51271, Network: "tcp,udp", UDPTimeout: "5m", RoutingMark: 1,
		}},
		Outbounds: []Outbound{
			{Type: "direct", Tag: "awg10", BindInterface: "opkgtun10"},
		},
		Route: Route{
			RuleSet: []RuleSet{
				{Tag: "geosite-youtube", Type: "remote", Format: "binary",
					URL: "https://example.com/geosite-youtube.srs", UpdateInterval: "24h"},
			},
			Rules: []Rule{
				{Action: "sniff"},
				{RuleSet: []string{"geosite-youtube"}, Action: "route", Outbound: "awg10"},
			},
			Final: "direct",
		},
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Inbounds) != 1 || loaded.Inbounds[0].Tag != "tproxy-in" {
		t.Errorf("inbounds: %+v", loaded.Inbounds)
	}
	if len(loaded.Outbounds) != 1 || loaded.Outbounds[0].BindInterface != "opkgtun10" {
		t.Errorf("outbounds: %+v", loaded.Outbounds)
	}
	if loaded.Route.Final != "direct" {
		t.Errorf("final: %q", loaded.Route.Final)
	}
	if len(loaded.Route.Rules) != 2 || len(loaded.Route.RuleSet) != 1 {
		t.Errorf("route: %+v", loaded.Route)
	}
}

func TestLoadConfigMissingReturnsEmpty(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Inbounds) != 0 || len(cfg.Outbounds) != 0 {
		t.Error("expected empty config")
	}
}

func TestSaveProducesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20-router.json")
	if err := SaveConfig(path, NewEmptyConfig()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, k := range []string{"inbounds", "outbounds", "route"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

func TestNewEmptyConfig_FinalIsDirect(t *testing.T) {
	cfg := NewEmptyConfig()
	if cfg.Route.Final != "direct" {
		t.Errorf("expected Final='direct', got %q", cfg.Route.Final)
	}
}

func TestEnsureSystemRules_EnforcesFinal(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.Final = ""
	cfg.EnsureSystemRules(true)
	if cfg.Route.Final != "direct" {
		t.Errorf("EnsureSystemRules should set Final='direct' when empty, got %q", cfg.Route.Final)
	}
}

func TestEnsureSystemRules_PreservesCustomFinal(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.Final = "my-vpn"
	cfg.EnsureSystemRules(true)
	if cfg.Route.Final != "my-vpn" {
		t.Errorf("EnsureSystemRules should preserve non-empty Final, got %q", cfg.Route.Final)
	}
}

func TestSetRouteFinal_RejectsEmpty(t *testing.T) {
	cfg := NewEmptyConfig()
	if err := cfg.SetRouteFinal(""); err == nil {
		t.Error("expected SetRouteFinal('') to error")
	}
}

func TestStripLegacyAWGDirect(t *testing.T) {
	in := []Outbound{
		{Type: "direct", Tag: "legacy-a", BindInterface: "t2s0"},
		{Type: "direct", Tag: "direct"}, // no bind_interface — keep
		{Type: "selector", Tag: "comp", Outbounds: []string{"awg-x"}},
		{Type: "direct", Tag: "legacy-b", BindInterface: "nwg0"},
	}
	got := stripLegacyAWGDirect(in)
	if len(got) != 2 {
		t.Fatalf("want 2 outbounds (direct + selector), got %d (%+v)", len(got), got)
	}
	if got[0].Tag != "direct" || got[1].Tag != "comp" {
		t.Errorf("unexpected outbounds: %+v", got)
	}
}

func TestRuleUnmarshalJSON_PortScalar(t *testing.T) {
	// sing-box allows "port": 53 (scalar), our struct expects []int.
	raw := `{"port": 53, "action": "route", "outbound": "direct"}`
	var r Rule
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal scalar port: %v", err)
	}
	if len(r.Port) != 1 || r.Port[0] != 53 {
		t.Errorf("want Port=[53], got %v", r.Port)
	}
}

func TestRuleUnmarshalJSON_PortArray(t *testing.T) {
	raw := `{"port": [80, 443], "action": "route", "outbound": "proxy"}`
	var r Rule
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal array port: %v", err)
	}
	if len(r.Port) != 2 || r.Port[0] != 80 || r.Port[1] != 443 {
		t.Errorf("want Port=[80,443], got %v", r.Port)
	}
}

func TestRuleUnmarshalJSON_LogicalWithScalarPort(t *testing.T) {
	// The exact shape that triggers the original crash: a logical rule
	// with a nested sub-rule containing "port": 53 (scalar).
	raw := `{
		"type": "logical",
		"mode": "or",
		"rules": [
			{"protocol": "dns"},
			{"port": 53}
		],
		"action": "hijack-dns"
	}`
	var r Rule
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal logical rule with scalar port: %v", err)
	}
	if r.Type != "logical" || len(r.Rules) != 2 {
		t.Fatalf("unexpected shape: %+v", r)
	}
	nested := r.Rules[1]
	if len(nested.Port) != 1 || nested.Port[0] != 53 {
		t.Errorf("nested rule: want Port=[53], got %v", nested.Port)
	}
}
