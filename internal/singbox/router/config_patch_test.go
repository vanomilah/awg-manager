package router

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestRuleSetAddDuplicate(t *testing.T) {
	cfg := NewEmptyConfig()
	rs := RuleSet{Tag: "geosite-youtube", Type: "remote", Format: "binary", URL: "https://example.com/yt.srs", UpdateInterval: "24h"}
	if err := cfg.AddRuleSet(rs); err != nil {
		t.Fatal(err)
	}
	err := cfg.AddRuleSet(rs)
	if !errors.Is(err, ErrRuleSetTagConflict) {
		t.Errorf("expected ErrRuleSetTagConflict, got %v", err)
	}
}

func TestRuleSetUpdate(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.RuleSet = []RuleSet{
		{Tag: "geosite-youtube", Type: "remote", Format: "binary", URL: "https://example.com/yt.srs", UpdateInterval: "24h"},
	}

	// Successful update — replace URL.
	next := RuleSet{Tag: "geosite-youtube", Type: "remote", Format: "binary", URL: "https://example.com/yt-new.srs", UpdateInterval: "24h"}
	if err := cfg.UpdateRuleSet("geosite-youtube", next); err != nil {
		t.Fatalf("update: %v", err)
	}
	if cfg.Route.RuleSet[0].URL != "https://example.com/yt-new.srs" {
		t.Errorf("URL not updated, got %q", cfg.Route.RuleSet[0].URL)
	}

	// Not found.
	missing := RuleSet{Tag: "missing", Type: "remote", Format: "binary", URL: "https://example.com/x.srs", UpdateInterval: "24h"}
	err := cfg.UpdateRuleSet("missing", missing)
	if !errors.Is(err, ErrRuleSetNotFound) {
		t.Errorf("expected ErrRuleSetNotFound, got %v", err)
	}

	// Tag rename rejected.
	renamed := RuleSet{Tag: "geosite-renamed", Type: "remote", Format: "binary", URL: "https://example.com/x.srs", UpdateInterval: "24h"}
	err = cfg.UpdateRuleSet("geosite-youtube", renamed)
	if err == nil {
		t.Error("expected tag-rename to be rejected, got nil")
	}
}

func TestRuleSetRemoteURLValidation(t *testing.T) {
	cfg := NewEmptyConfig()

	// Garbage URL rejected.
	err := cfg.AddRuleSet(RuleSet{Tag: "g1", Type: "remote", URL: "not a url"})
	if err == nil || !contains(err.Error(), "invalid url") {
		t.Errorf("expected 'invalid url' error, got %v", err)
	}

	// Missing host rejected (scheme but no host).
	err = cfg.AddRuleSet(RuleSet{Tag: "g2", Type: "remote", URL: "https://"})
	if err == nil || !contains(err.Error(), "invalid url") {
		t.Errorf("expected 'invalid url' for empty host, got %v", err)
	}

	// Non-http/https scheme rejected.
	err = cfg.AddRuleSet(RuleSet{Tag: "g3", Type: "remote", URL: "ftp://example.com/x.srs"})
	if err == nil || !contains(err.Error(), "scheme must be http or https") {
		t.Errorf("expected 'scheme' error, got %v", err)
	}

	// Valid http URL accepted.
	if err := cfg.AddRuleSet(RuleSet{Tag: "ok-http", Type: "remote", URL: "http://example.com/x.srs"}); err != nil {
		t.Fatalf("valid http URL: %v", err)
	}

	// Valid https URL accepted.
	if err := cfg.AddRuleSet(RuleSet{Tag: "ok-https", Type: "remote", URL: "https://example.com/x.srs"}); err != nil {
		t.Fatalf("valid https URL: %v", err)
	}
}

func TestRuleSetTagReservedSRSSuffix(t *testing.T) {
	cfg := NewEmptyConfig()
	err := cfg.AddRuleSet(RuleSet{Tag: "geosite-samsung-srs", Type: "inline", Rules: []map[string]any{
		{"domain_suffix": []any{".samsung.com"}},
	}})
	if err == nil || !contains(err.Error(), "-srs") {
		t.Fatalf("expected reserved -srs suffix error, got %v", err)
	}
}

func TestRuleSetInlineValidation(t *testing.T) {
	cfg := NewEmptyConfig()

	// Empty rules rejected.
	err := cfg.AddRuleSet(RuleSet{Tag: "in-empty", Type: "inline"})
	if err == nil || !contains(err.Error(), "rules required") {
		t.Errorf("expected 'rules required' error, got %v", err)
	}

	// Rule with no known matcher rejected.
	err = cfg.AddRuleSet(RuleSet{Tag: "in-bad", Type: "inline", Rules: []map[string]any{{"unknown_field": "x"}}})
	if err == nil || !contains(err.Error(), "no known matcher") {
		t.Errorf("expected 'no known matcher' error, got %v", err)
	}

	// Rule with empty domain_suffix array rejected.
	err = cfg.AddRuleSet(RuleSet{Tag: "in-empty-arr", Type: "inline", Rules: []map[string]any{{"domain_suffix": []any{}}}})
	if err == nil {
		t.Error("expected error for empty domain_suffix array, got nil")
	}

	// Valid inline rule_set.
	err = cfg.AddRuleSet(RuleSet{Tag: "in-ok", Type: "inline", Rules: []map[string]any{
		{"domain_suffix": []any{".example.com"}},
	}})
	if err != nil {
		t.Fatalf("valid inline: %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestRuleSetDeleteWithReferences(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.RuleSet = []RuleSet{{Tag: "geosite-youtube"}}
	cfg.Route.Rules = []Rule{{RuleSet: []string{"geosite-youtube"}, Action: "route", Outbound: "awg10"}}

	err := cfg.DeleteRuleSet("geosite-youtube", false)
	if !errors.Is(err, ErrRuleSetReferenced) {
		t.Errorf("expected ErrRuleSetReferenced, got %v", err)
	}

	if err := cfg.DeleteRuleSet("geosite-youtube", true); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.RuleSet) != 0 {
		t.Error("rule_set should be empty after force delete")
	}
}

func TestRuleAddValidatesMatchers(t *testing.T) {
	cfg := NewEmptyConfig()
	err := cfg.AddRule(Rule{Action: "route", Outbound: "awg10"})
	if !errors.Is(err, ErrInvalidMatchers) {
		t.Errorf("expected ErrInvalidMatchers, got %v", err)
	}

	if err := cfg.AddRule(Rule{DomainSuffix: []string{"youtube.com"}, Action: "route", Outbound: "awg10"}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.Rules) != 1 {
		t.Error("rule not added")
	}
}

func TestRuleMove(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.Rules = []Rule{
		{Action: "sniff"},
		{DomainSuffix: []string{"a.com"}, Action: "route", Outbound: "x"},
		{DomainSuffix: []string{"b.com"}, Action: "route", Outbound: "y"},
	}
	if err := cfg.MoveRule(2, 0); err != nil {
		t.Fatal(err)
	}
	if cfg.Route.Rules[0].DomainSuffix[0] != "b.com" {
		t.Errorf("expected b.com first, got %+v", cfg.Route.Rules[0])
	}
}

func TestEnsureSystemRules(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.EnsureSystemRules(true)
	if len(cfg.Route.Rules) < 3 {
		t.Fatalf("expected >=3 rules, got %d", len(cfg.Route.Rules))
	}
	if cfg.Route.Rules[0].Action != "sniff" {
		t.Errorf("first rule should be sniff, got %+v", cfg.Route.Rules[0])
	}

	hijack := cfg.Route.Rules[1]
	if hijack.Action != "hijack-dns" {
		t.Errorf("second rule should be hijack-dns, got %+v", hijack)
	}
	// System hijack-dns must be logical-or(protocol:dns, port:53) so it
	// catches direct DNS to the router LAN IP that sniffing missed.
	if hijack.Type != "logical" || hijack.Mode != "or" {
		t.Errorf("hijack rule must be logical-or, got type=%q mode=%q", hijack.Type, hijack.Mode)
	}
	if len(hijack.Rules) != 2 {
		t.Fatalf("hijack rule should have 2 nested rules, got %d", len(hijack.Rules))
	}
	if hijack.Rules[0].Protocol != "dns" {
		t.Errorf("nested[0] should be protocol:dns, got %+v", hijack.Rules[0])
	}
	if len(hijack.Rules[1].Port) != 1 || hijack.Rules[1].Port[0] != 53 {
		t.Errorf("nested[1] should be port:53, got %+v", hijack.Rules[1])
	}

	privateBypass := cfg.Route.Rules[2]
	if privateBypass.IPIsPrivate == nil || !*privateBypass.IPIsPrivate {
		t.Errorf("third rule should have ip_is_private:true, got %+v", privateBypass)
	}
	if privateBypass.Outbound != "direct" {
		t.Errorf("ip_is_private rule must outbound to 'direct', got %q", privateBypass.Outbound)
	}

	// Idempotency: re-running should NOT add duplicates of any system rule.
	cfg.EnsureSystemRules(true)
	var sniffCount, hijackCount, privateCount int
	for _, r := range cfg.Route.Rules {
		if r.Action == "sniff" && !r.hasAnyMatcher() {
			sniffCount++
		}
		if r.Action == "hijack-dns" {
			hijackCount++
		}
		if r.IPIsPrivate != nil && *r.IPIsPrivate {
			privateCount++
		}
	}
	if sniffCount != 1 || hijackCount != 1 || privateCount != 1 {
		t.Errorf("system rules duplicated: sniff=%d hijack=%d private=%d",
			sniffCount, hijackCount, privateCount)
	}
}

func TestEnsureSystemRules_SnifferDisabled(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Route.Rules = []Rule{
		{Action: "sniff"},
		{DomainSuffix: []string{"example.com"}, Action: "sniff"},
	}
	cfg.EnsureSystemRules(false)

	for _, r := range cfg.Route.Rules {
		if r.Action == "sniff" && !r.hasAnyMatcher() {
			t.Fatalf("system sniff rule must be removed when sniffer is disabled: %+v", cfg.Route.Rules)
		}
	}
	var userSniff, hijackCount int
	for _, r := range cfg.Route.Rules {
		if r.Action == "sniff" && len(r.DomainSuffix) == 1 && r.DomainSuffix[0] == "example.com" {
			userSniff++
		}
		if r.Action == "hijack-dns" {
			hijackCount++
		}
	}
	if userSniff != 1 {
		t.Fatalf("user-authored sniff rule should be preserved, got %d in %+v", userSniff, cfg.Route.Rules)
	}
	if hijackCount != 1 {
		t.Fatalf("hijack-dns should remain enabled, got %d in %+v", hijackCount, cfg.Route.Rules)
	}
}

func TestEnsureSystemRules_PrivateBypassMustComeAfterHijack(t *testing.T) {
	// Critical ordering invariant: ip_is_private MUST come AFTER hijack-dns,
	// not before. If it's prepended in front, DNS to router LAN IP matches
	// ip_is_private first and routes `direct` — bypassing hijack-dns
	// entirely. DNS hijacking for in-policy clients breaks silently.
	//
	// This case is the common-on-upgrade one: an EXISTING config that
	// already has sniff and hijack-dns from a prior version, plus our
	// new code now adding ip_is_private. The naive "prepend all missing
	// system rules" approach puts ip_is_private at position 0, ahead of
	// the existing hijack-dns. Test pins the correct order.
	cfg := NewEmptyConfig()
	cfg.Route.Rules = []Rule{
		{Action: "sniff"},
		{
			Type: "logical", Mode: "or",
			Rules:  []Rule{{Protocol: "dns"}, {Port: []int{53}}},
			Action: "hijack-dns",
		},
	}
	cfg.EnsureSystemRules(true)

	hijackPos, privatePos := -1, -1
	for i, r := range cfg.Route.Rules {
		if r.Action == "hijack-dns" && hijackPos == -1 {
			hijackPos = i
		}
		if r.IPIsPrivate != nil && *r.IPIsPrivate && privatePos == -1 {
			privatePos = i
		}
	}
	if hijackPos < 0 {
		t.Fatal("hijack-dns missing after EnsureSystemRules")
	}
	if privatePos < 0 {
		t.Fatal("ip_is_private missing after EnsureSystemRules")
	}
	if privatePos <= hijackPos {
		t.Errorf("ip_is_private (pos=%d) MUST come after hijack-dns (pos=%d) — DNS hijack breaks otherwise. Rules: %+v",
			privatePos, hijackPos, cfg.Route.Rules)
	}
	if privatePos != hijackPos+1 {
		t.Errorf("ip_is_private should be inserted immediately after hijack-dns (expected pos %d, got %d)",
			hijackPos+1, privatePos)
	}
}

func TestEnsureSystemRules_PreservesCustomPrivateBypass(t *testing.T) {
	// If the user has authored their own ip_is_private rule (e.g. they
	// want private destinations to go through a specific direct-LAN
	// outbound rather than sing-box's built-in `direct`), EnsureSystemRules
	// must NOT prepend a competing system rule that would shadow it.
	cfg := NewEmptyConfig()
	truePtr := true
	cfg.Route.Rules = []Rule{
		{IPIsPrivate: &truePtr, Outbound: "lan-direct"},
	}
	cfg.EnsureSystemRules(true)

	var privateCount int
	var firstPrivateOutbound string
	for _, r := range cfg.Route.Rules {
		if r.IPIsPrivate != nil && *r.IPIsPrivate {
			privateCount++
			if firstPrivateOutbound == "" {
				firstPrivateOutbound = r.Outbound
			}
		}
	}
	if privateCount != 1 {
		t.Errorf("expected 1 ip_is_private rule (user's), got %d", privateCount)
	}
	if firstPrivateOutbound != "lan-direct" {
		t.Errorf("user's custom outbound was overridden: got %q want %q",
			firstPrivateOutbound, "lan-direct")
	}
}

func TestEnsureSystemRules_JSONOmitsUnsetIPIsPrivate(t *testing.T) {
	// `ip_is_private` is `*bool` specifically so an unset value does NOT
	// serialize as `"ip_is_private": false` — that would change sing-box
	// semantics (false explicitly means "match non-private"). Verify
	// that a typical user rule (no ip_is_private set) round-trips clean.
	cfg := NewEmptyConfig()
	cfg.Route.Rules = []Rule{
		{DomainSuffix: []string{"example.com"}, Outbound: "proxy"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if contains(string(data), "ip_is_private") {
		t.Errorf("unset ip_is_private leaked into JSON: %s", string(data))
	}
}

func TestEnsureSystemRules_LegacyHijackRecognized(t *testing.T) {
	// Configs migrated from pre-v2.10.3 carry the legacy form
	// `{protocol:"dns", action:"hijack-dns"}`. EnsureSystemRules must
	// recognize it as system hijack and NOT prepend a duplicate
	// logical-or rule on every reload.
	cfg := NewEmptyConfig()
	cfg.Route.Rules = []Rule{
		{Action: "sniff"},
		{Protocol: "dns", Action: "hijack-dns"},
	}
	cfg.EnsureSystemRules(true)
	var hijackCount int
	for _, r := range cfg.Route.Rules {
		if r.Action == "hijack-dns" {
			hijackCount++
		}
	}
	if hijackCount != 1 {
		t.Errorf("legacy hijack rule should not be duplicated, got %d hijack rules", hijackCount)
	}
}

func TestCompositeOutboundRejectsDirectMember(t *testing.T) {
	cfg := NewEmptyConfig()

	// Add: direct as member rejected.
	err := cfg.AddCompositeOutbound(Outbound{Tag: "auto", Type: "urltest", Outbounds: []string{"awg10", "direct"}})
	if err == nil || !contains(err.Error(), "not allowed in composite groups") {
		t.Errorf("expected 'not allowed' error for direct member, got %v", err)
	}

	// Add: case-insensitive direct also rejected.
	err = cfg.AddCompositeOutbound(Outbound{Tag: "auto", Type: "urltest", Outbounds: []string{"awg10", "Direct"}})
	if err == nil {
		t.Error("expected case-insensitive direct to be rejected")
	}

	// Add: direct as default rejected.
	err = cfg.AddCompositeOutbound(Outbound{Tag: "auto", Type: "selector", Outbounds: []string{"awg10"}, Default: "direct"})
	if err == nil || !contains(err.Error(), "default") {
		t.Errorf("expected 'default' error for direct default, got %v", err)
	}

	// Empty members rejected.
	err = cfg.AddCompositeOutbound(Outbound{Tag: "auto", Type: "urltest"})
	if err == nil || !contains(err.Error(), "at least one member") {
		t.Errorf("expected 'at least one member' error, got %v", err)
	}

	// Empty tag rejected.
	err = cfg.AddCompositeOutbound(Outbound{Tag: "", Type: "selector", Outbounds: []string{"awg10"}})
	if err == nil || !contains(err.Error(), "tag is required") {
		t.Errorf("expected 'tag is required' error, got %v", err)
	}

	// Valid composite accepted.
	if err := cfg.AddCompositeOutbound(Outbound{Tag: "auto", Type: "urltest", Outbounds: []string{"awg10", "awg20"}}); err != nil {
		t.Fatalf("valid composite rejected: %v", err)
	}

	// Update: same validation applies.
	err = cfg.UpdateCompositeOutbound("auto", Outbound{Tag: "auto", Type: "urltest", Outbounds: []string{"direct"}})
	if err == nil {
		t.Error("expected Update with direct member to be rejected")
	}
}

func TestCompositeOutboundTagConflict(t *testing.T) {
	cfg := NewEmptyConfig()
	o := Outbound{Type: "urltest", Tag: "fast", Outbounds: []string{"awg10", "awg20"}}
	if err := cfg.AddCompositeOutbound(o); err != nil {
		t.Fatal(err)
	}
	err := cfg.AddCompositeOutbound(o)
	if !errors.Is(err, ErrOutboundTagConflict) {
		t.Errorf("expected ErrOutboundTagConflict, got %v", err)
	}
}

func TestCompositeOutboundDeleteReferenced(t *testing.T) {
	cfg := NewEmptyConfig()
	cfg.Outbounds = []Outbound{{Type: "urltest", Tag: "fast"}}
	cfg.Route.Rules = []Rule{{DomainSuffix: []string{"x.com"}, Action: "route", Outbound: "fast"}}

	err := cfg.DeleteCompositeOutbound("fast", false)
	if !errors.Is(err, ErrOutboundReferenced) {
		t.Errorf("expected ErrOutboundReferenced, got %v", err)
	}
	if err := cfg.DeleteCompositeOutbound("fast", true); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRouteWAN_AutoDetectMode(t *testing.T) {
	cfg := NewEmptyConfig()
	// Pre-populate stale state to confirm EnsureRouteWAN clears it.
	cfg.Route.DefaultInterface = "ppp_stale"
	cfg.EnsureRouteWAN(true, "")

	if cfg.Route.AutoDetectInterface == nil || !*cfg.Route.AutoDetectInterface {
		t.Errorf("expected AutoDetectInterface=&true, got %+v", cfg.Route.AutoDetectInterface)
	}
	if cfg.Route.DefaultInterface != "" {
		t.Errorf("expected DefaultInterface cleared, got %q", cfg.Route.DefaultInterface)
	}
}

func TestEnsureRouteWAN_PinnedMode(t *testing.T) {
	cfg := NewEmptyConfig()
	// Pre-populate stale state to confirm EnsureRouteWAN clears it.
	truePtr := true
	cfg.Route.AutoDetectInterface = &truePtr
	cfg.EnsureRouteWAN(false, "ppp0")

	if cfg.Route.AutoDetectInterface != nil {
		t.Errorf("expected AutoDetectInterface cleared, got %+v", *cfg.Route.AutoDetectInterface)
	}
	if cfg.Route.DefaultInterface != "ppp0" {
		t.Errorf("expected DefaultInterface=ppp0, got %q", cfg.Route.DefaultInterface)
	}
}

func TestEnsureRouteWAN_JSONShape(t *testing.T) {
	// Verify emitted JSON has exactly one of `auto_detect_interface` /
	// `default_interface` — never both, never neither.
	autoCfg := NewEmptyConfig()
	autoCfg.EnsureRouteWAN(true, "")
	autoBytes, _ := json.Marshal(autoCfg.Route)
	autoStr := string(autoBytes)
	if !contains(autoStr, `"auto_detect_interface":true`) {
		t.Errorf("auto mode: expected auto_detect_interface:true in JSON, got %s", autoStr)
	}
	if contains(autoStr, "default_interface") {
		t.Errorf("auto mode: default_interface must not appear in JSON, got %s", autoStr)
	}

	pinnedCfg := NewEmptyConfig()
	pinnedCfg.EnsureRouteWAN(false, "eth3")
	pinnedBytes, _ := json.Marshal(pinnedCfg.Route)
	pinnedStr := string(pinnedBytes)
	if !contains(pinnedStr, `"default_interface":"eth3"`) {
		t.Errorf("pinned mode: expected default_interface:eth3 in JSON, got %s", pinnedStr)
	}
	if contains(pinnedStr, "auto_detect_interface") {
		t.Errorf("pinned mode: auto_detect_interface must not appear in JSON, got %s", pinnedStr)
	}
}

func TestValidateSingboxRouterSettings(t *testing.T) {
	cases := []struct {
		name      string
		auto      bool
		iface     string
		wantError bool
	}{
		{"auto + empty   = OK", true, "", false},
		{"pinned + ppp0  = OK", false, "ppp0", false},
		{"auto + ppp0    = ERROR (contradictory)", true, "ppp0", true},
		{"pinned + empty = ERROR (no target)", false, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSingboxRouterSettings(storage.SingboxRouterSettings{
				WANAutoDetect: c.auto,
				WANInterface:  c.iface,
			})
			if c.wantError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestNormalizeSingboxRouterSettings_DeviceMode(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		want      string
		wantError bool
	}{
		{"empty defaults to policy", "", "policy", false},
		{"policy accepted", "policy", "policy", false},
		{"all accepted", "all", "all", false},
		{"invalid rejected", "everything", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSingboxRouterSettings(storage.SingboxRouterSettings{
				DeviceMode:    c.mode,
				WANAutoDetect: true,
			})
			if c.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSingboxRouterSettings: %v", err)
			}
			if got.DeviceMode != c.want {
				t.Fatalf("DeviceMode = %q, want %q", got.DeviceMode, c.want)
			}
		})
	}
}
