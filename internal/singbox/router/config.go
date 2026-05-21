package router

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func NewEmptyConfig() *RouterConfig {
	return &RouterConfig{
		Inbounds:  []Inbound{},
		Outbounds: []Outbound{},
		DNS: DNS{
			Servers: []DNSServer{},
			Rules:   []DNSRule{},
		},
		Route: Route{
			RuleSet: []RuleSet{},
			Rules:   []Rule{},
			Final:   "direct",
		},
	}
}

func LoadConfig(path string) (*RouterConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewEmptyConfig(), nil
		}
		return nil, err
	}
	cfg := NewEmptyConfig()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Inbounds == nil {
		cfg.Inbounds = []Inbound{}
	}
	if cfg.Outbounds == nil {
		cfg.Outbounds = []Outbound{}
	}
	if cfg.Route.RuleSet == nil {
		cfg.Route.RuleSet = []RuleSet{}
	}
	if cfg.Route.Rules == nil {
		cfg.Route.Rules = []Rule{}
	}
	if cfg.DNS.Servers == nil {
		cfg.DNS.Servers = []DNSServer{}
	}
	if cfg.DNS.Rules == nil {
		cfg.DNS.Rules = []DNSRule{}
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *RouterConfig) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *RouterConfig) AddRuleSet(rs RuleSet) error {
	if err := validateRuleSet(rs); err != nil {
		return err
	}
	for _, existing := range c.Route.RuleSet {
		if existing.Tag == rs.Tag {
			return fmt.Errorf("%w: %q", ErrRuleSetTagConflict, rs.Tag)
		}
	}
	c.Route.RuleSet = append(c.Route.RuleSet, rs)
	return nil
}

func (c *RouterConfig) UpdateRuleSet(tag string, next RuleSet) error {
	if next.Tag == "" {
		next.Tag = tag
	}
	if next.Tag != tag {
		return fmt.Errorf("rule_set %q: changing tag is not supported", tag)
	}
	if err := validateRuleSet(next); err != nil {
		return err
	}
	for i, existing := range c.Route.RuleSet {
		if existing.Tag == tag {
			c.Route.RuleSet[i] = next
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrRuleSetNotFound, tag)
}

func (c *RouterConfig) DeleteRuleSet(tag string, force bool) error {
	tags := ruleSetTagsWithCompanion(tag)
	refs := c.rulesReferencingRuleSets(tags)
	if len(refs.route) > 0 && !force {
		return fmt.Errorf("%w: %q referenced by route rules %v", ErrRuleSetReferenced, tag, refs.route)
	}
	if len(refs.dns) > 0 && !force {
		return fmt.Errorf("%w: %q referenced by dns rules %v", ErrRuleSetReferenced, tag, refs.dns)
	}
	remove := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		remove[t] = struct{}{}
	}
	filtered := make([]RuleSet, 0, len(c.Route.RuleSet))
	for _, rs := range c.Route.RuleSet {
		if _, drop := remove[rs.Tag]; drop {
			continue
		}
		filtered = append(filtered, rs)
	}
	c.Route.RuleSet = filtered
	return nil
}

type ruleSetRefIndices struct {
	route []int
	dns   []int
}

func (c *RouterConfig) rulesReferencingRuleSets(tags []string) ruleSetRefIndices {
	want := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		want[t] = struct{}{}
	}
	var out ruleSetRefIndices
	for i, r := range c.Route.Rules {
		for _, rsTag := range r.RuleSet {
			if _, ok := want[rsTag]; ok {
				out.route = append(out.route, i)
				break
			}
		}
	}
	for i, r := range c.DNS.Rules {
		for _, rsTag := range r.RuleSet {
			if _, ok := want[rsTag]; ok {
				out.dns = append(out.dns, i)
				break
			}
		}
	}
	return out
}

func (c *RouterConfig) rulesReferencingRuleSet(tag string) []int {
	return c.rulesReferencingRuleSets(ruleSetTagsWithCompanion(tag)).route
}

func (c *RouterConfig) AddRule(r Rule) error {
	if !r.hasAnyMatcher() && r.Action != "sniff" && r.Action != "hijack-dns" {
		return ErrInvalidMatchers
	}
	if err := validateRule(r); err != nil {
		return err
	}
	c.Route.Rules = append(c.Route.Rules, r)
	return nil
}

func (c *RouterConfig) UpdateRule(index int, r Rule) error {
	if index < 0 || index >= len(c.Route.Rules) {
		return ErrRuleIndexOutOfRange
	}
	if !r.hasAnyMatcher() && r.Action != "sniff" && r.Action != "hijack-dns" {
		return ErrInvalidMatchers
	}
	if err := validateRule(r); err != nil {
		return err
	}
	c.Route.Rules[index] = r
	return nil
}

func (c *RouterConfig) DeleteRule(index int) error {
	if index < 0 || index >= len(c.Route.Rules) {
		return ErrRuleIndexOutOfRange
	}
	c.Route.Rules = append(c.Route.Rules[:index], c.Route.Rules[index+1:]...)
	return nil
}

func (c *RouterConfig) MoveRule(from, to int) error {
	n := len(c.Route.Rules)
	if from < 0 || from >= n || to < 0 || to >= n {
		return ErrRuleIndexOutOfRange
	}
	if from == to {
		return nil
	}
	r := c.Route.Rules[from]
	without := append(c.Route.Rules[:from:from], c.Route.Rules[from+1:]...)
	rules := make([]Rule, 0, n)
	rules = append(rules, without[:to]...)
	rules = append(rules, r)
	rules = append(rules, without[to:]...)
	c.Route.Rules = rules
	return nil
}

func (c *RouterConfig) EnsureSystemRules(snifferEnabled bool) {
	if c.Route.Final == "" {
		c.Route.Final = "direct"
	}
	hasSniff := false
	hasHijack := false
	hasPrivateBypass := false
	// Track existing hijack-dns position. ip_is_private MUST be inserted
	// right after hijack-dns; if we prepend it to position 0 instead,
	// LAN-IP DNS matches ip_is_private first and routes `direct`,
	// bypassing the hijack entirely and breaking DNS for in-policy
	// clients.
	hijackIdx := -1
	for i, r := range c.Route.Rules {
		if r.Action == "sniff" && !r.hasAnyMatcher() {
			hasSniff = true
		}
		// Detect both the legacy (`protocol:dns`) and current
		// (`logical(or){protocol:dns, port:53}`) system hijack-dns
		// forms so re-running EnsureSystemRules doesn't stack
		// duplicates on configs migrated from older versions.
		if r.Action == "hijack-dns" {
			if r.Protocol == "dns" || (r.Type == "logical" && r.Mode == "or") {
				hasHijack = true
				if hijackIdx == -1 {
					hijackIdx = i
				}
			}
		}
		// Any user-authored ip_is_private rule wins over the system
		// one — we just have to not duplicate. Outbound is intentionally
		// not checked: a user might point private destinations at a
		// specific direct-LAN outbound and we should respect that.
		if r.IPIsPrivate != nil && *r.IPIsPrivate {
			hasPrivateBypass = true
		}
	}

	if !snifferEnabled && hasSniff {
		filtered := c.Route.Rules[:0]
		for _, r := range c.Route.Rules {
			if r.Action == "sniff" && !r.hasAnyMatcher() {
				continue
			}
			filtered = append(filtered, r)
		}
		c.Route.Rules = filtered
		hasSniff = false
		if hijackIdx >= 0 {
			hijackIdx = -1
			for i, r := range c.Route.Rules {
				if r.Action == "hijack-dns" {
					if r.Protocol == "dns" || (r.Type == "logical" && r.Mode == "or") {
						hijackIdx = i
						break
					}
				}
			}
		}
	}

	// Phase 1: prepend sniff + hijack-dns to front if missing.
	// Predictable order inside the prepend block is [sniff, hijack-dns].
	prepend := make([]Rule, 0, 2)
	if snifferEnabled && !hasSniff {
		prepend = append(prepend, Rule{Action: "sniff"})
	}
	if !hasHijack {
		// Logical-or rule catches BOTH sniffed DNS (`protocol:dns`)
		// and any TCP/UDP traffic to port 53 (`port:53`). The latter
		// matters when sniffing missed the protocol (truncated buffer,
		// non-standard DNS payload) — port-based match guarantees
		// hijack still fires. SKeen ships the same form.
		prepend = append(prepend, Rule{
			Type: "logical",
			Mode: "or",
			Rules: []Rule{
				{Protocol: "dns"},
				{Port: []int{53}},
			},
			Action: "hijack-dns",
		})
		// Newly-prepended hijack ends up at the last slot of the
		// prepend block (after the optional sniff).
		hijackIdx = len(prepend) - 1
	} else {
		// Existing hijack shifts right by len(prepend) once prepend is
		// stitched in front.
		hijackIdx += len(prepend)
	}
	if len(prepend) > 0 {
		c.Route.Rules = append(prepend, c.Route.Rules...)
	}

	// Phase 2: insert ip_is_private at hijackIdx+1 — directly after the
	// hijack-dns rule, whether it was just prepended or already present.
	if !hasPrivateBypass {
		// Defense-in-depth: any packet that slips into sing-box with a
		// private destination (RFC1918, loopback, link-local, CGNAT,
		// multicast) goes `direct` instead of falling through to
		// `final: proxy`. Matters specifically for non-policy DNS that
		// the `hijack-dns` side-effect transparent listener picks up
		// from router LAN IPs — those packets arrive without TPROXY
		// ancillary data and would otherwise be silently dropped (no
		// reply, client sees timeout). Mirrors SKeen example config
		// (`reference/SKeen/examples/config.json:115`).
		truePtr := true
		privateRule := Rule{IPIsPrivate: &truePtr, Outbound: "direct"}
		insertPos := hijackIdx + 1
		newRules := make([]Rule, 0, len(c.Route.Rules)+1)
		newRules = append(newRules, c.Route.Rules[:insertPos]...)
		newRules = append(newRules, privateRule)
		newRules = append(newRules, c.Route.Rules[insertPos:]...)
		c.Route.Rules = newRules
	}
}

// EnsureRouteWAN applies the WAN-binding discriminator to route.
// Exactly one of `auto_detect_interface` / `default_interface` is written
// to the emitted config — never both — so sing-box never sees a
// contradictory state.
//
//   - autoDetect == true  → AutoDetectInterface = &true,
//     DefaultInterface = "".
//     kernelName MUST be empty here (validated upstream by
//     ValidateSingboxRouterSettings); the field is accepted as an
//     argument purely for the symmetric signature.
//   - autoDetect == false → DefaultInterface = kernelName,
//     AutoDetectInterface = nil.
//     kernelName MUST be a non-empty kernel system-name (e.g. "ppp0").
//     Same upstream validator enforces non-emptiness; this method does
//     not second-guess the caller.
//
// Called from Enable() after EnsureSystemRules. Re-running with the same
// arguments is a no-op idempotent update.
func (c *RouterConfig) EnsureRouteWAN(autoDetect bool, kernelName string) {
	if autoDetect {
		t := true
		c.Route.AutoDetectInterface = &t
		c.Route.DefaultInterface = ""
		return
	}
	c.Route.AutoDetectInterface = nil
	c.Route.DefaultInterface = kernelName
}

// SetRouteFinal updates route.final. Caller must validate the tag refers
// to a known outbound (or sing-box built-in: "direct", "block").
// Setting to "" is rejected — use "direct" for default fallback.
func (c *RouterConfig) SetRouteFinal(tag string) error {
	if tag == "" {
		return fmt.Errorf("route final cannot be empty (use 'direct' for default)")
	}
	c.Route.Final = tag
	return nil
}

func (c *RouterConfig) AddCompositeOutbound(o Outbound) error {
	if err := validateCompositeOutbound(o); err != nil {
		return err
	}
	for _, existing := range c.Outbounds {
		if existing.Tag == o.Tag {
			return fmt.Errorf("%w: %q", ErrOutboundTagConflict, o.Tag)
		}
	}
	c.Outbounds = append(c.Outbounds, o)
	return nil
}

func (c *RouterConfig) UpdateCompositeOutbound(tag string, o Outbound) error {
	if err := validateCompositeOutbound(o); err != nil {
		return err
	}
	for i, existing := range c.Outbounds {
		if existing.Tag == tag {
			c.Outbounds[i] = o
			return nil
		}
	}
	return fmt.Errorf("%w: %q not found", ErrOutboundTagConflict, tag)
}

// validateCompositeOutbound rejects shapes that compile but produce
// surprising behavior at runtime. In particular `direct` as a member of
// a selector/urltest/loadbalance group lets traffic bypass the proxy
// silently — almost never what the user wants, and a known footgun in
// sing-box composite groups. Same for `default: "direct"`.
func validateCompositeOutbound(o Outbound) error {
	if strings.TrimSpace(o.Tag) == "" {
		return fmt.Errorf("outbound tag is required")
	}
	if len(o.Outbounds) == 0 {
		return fmt.Errorf("outbound %q: at least one member is required", o.Tag)
	}
	for _, m := range o.Outbounds {
		if strings.EqualFold(strings.TrimSpace(m), "direct") {
			return fmt.Errorf("outbound %q: member %q is not allowed in composite groups (would bypass proxy silently)", o.Tag, m)
		}
	}
	if strings.EqualFold(strings.TrimSpace(o.Default), "direct") {
		return fmt.Errorf("outbound %q: default %q is not allowed in composite groups", o.Tag, o.Default)
	}
	return nil
}

func (c *RouterConfig) DeleteCompositeOutbound(tag string, force bool) error {
	refs := c.rulesReferencingOutbound(tag)
	if len(refs) > 0 && !force {
		return fmt.Errorf("%w: %q referenced by rules %v", ErrOutboundReferenced, tag, refs)
	}
	filtered := make([]Outbound, 0, len(c.Outbounds))
	for _, o := range c.Outbounds {
		if o.Tag != tag {
			filtered = append(filtered, o)
		}
	}
	c.Outbounds = filtered
	return nil
}

func (c *RouterConfig) rulesReferencingOutbound(tag string) []int {
	var refs []int
	for i, r := range c.Route.Rules {
		if r.Outbound == tag {
			refs = append(refs, i)
		}
	}
	return refs
}

func (c *RouterConfig) CompositeOutbounds() []Outbound {
	// All non-system outbounds in 20-router.json are composite (urltest,
	// selector, loadbalance, ...). AWG-direct outbounds live in
	// 15-awg.json (owned by awgoutbounds) and are not present here.
	out := make([]Outbound, 0, len(c.Outbounds))
	for _, o := range c.Outbounds {
		out = append(out, o)
	}
	return out
}

// stripLegacyAWGDirect filters out direct outbounds with bind_interface
// set — these used to be written here pre-refactor; now they live in
// 15-awg.json owned by awgoutbounds. Composite outbounds are kept.
func stripLegacyAWGDirect(in []Outbound) []Outbound {
	out := make([]Outbound, 0, len(in))
	for _, o := range in {
		if o.Type == "direct" && o.BindInterface != "" {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (r Rule) hasAnyMatcher() bool {
	return len(r.DomainSuffix) > 0 || len(r.IPCIDR) > 0 || len(r.SourceIPCIDR) > 0 ||
		len(r.Port) > 0 || len(r.RuleSet) > 0 || r.Protocol != "" || len(r.Rules) > 0 ||
		r.IPIsPrivate != nil
}

func validateRule(r Rule) error {
	for _, cidr := range r.IPCIDR {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			if _, err := netip.ParseAddr(cidr); err != nil {
				return fmt.Errorf("ip_cidr %q: %w", cidr, err)
			}
		}
	}
	for _, cidr := range r.SourceIPCIDR {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			if _, err := netip.ParseAddr(cidr); err != nil {
				return fmt.Errorf("source_ip_cidr %q: %w", cidr, err)
			}
		}
	}
	for _, p := range r.Port {
		if p < 1 || p > 65535 {
			return fmt.Errorf("port %d out of range [1,65535]", p)
		}
	}
	return nil
}

func validateRuleSet(rs RuleSet) error {
	if rs.Tag == "" {
		return fmt.Errorf("rule_set tag is required")
	}
	if strings.HasSuffix(rs.Tag, inlineSRSSuffix) {
		return fmt.Errorf("rule_set %q: tag suffix %q is reserved for compiled inline rulesets", rs.Tag, inlineSRSSuffix)
	}
	switch rs.Type {
	case "inline":
		if len(rs.Rules) == 0 {
			return fmt.Errorf("rule_set %q: rules required for type=inline", rs.Tag)
		}
		for i, rule := range rs.Rules {
			if len(rule) == 0 {
				return fmt.Errorf("rule_set %q: inline rule at index %d is empty", rs.Tag, i)
			}
			if !inlineRuleHasKnownField(rule) {
				return fmt.Errorf("rule_set %q: inline rule at index %d has no known matcher/action fields", rs.Tag, i)
			}
		}
	case "remote":
		if rs.URL == "" {
			return fmt.Errorf("rule_set %q: url required for type=remote", rs.Tag)
		}
		u, err := url.Parse(rs.URL)
		if err != nil || u == nil || u.Host == "" {
			return fmt.Errorf("rule_set %q: invalid url %q", rs.Tag, rs.URL)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("rule_set %q: url scheme must be http or https, got %q", rs.Tag, u.Scheme)
		}
	case "local":
		if rs.Path == "" {
			return fmt.Errorf("rule_set %q: path required for type=local", rs.Tag)
		}
		if !filepath.IsAbs(rs.Path) {
			return fmt.Errorf("rule_set %q: path must be absolute", rs.Tag)
		}
	default:
		return fmt.Errorf("rule_set %q: unknown type %q", rs.Tag, rs.Type)
	}
	return nil
}

// inlineRuleHasKnownField reports whether an inline rule_set rule has at
// least one recognised matcher/action key with a non-empty value. Mirrors
// sing-box's headline-rule schema (subset; extend if sing-box adds more).
func inlineRuleHasKnownField(rule map[string]any) bool {
	known := []string{
		"domain", "domain_suffix", "domain_keyword", "domain_regex",
		"ip_cidr", "source_ip_cidr", "port", "source_port",
		"process_name", "process_path", "package_name",
		"protocol", "network", "rule_set",
	}
	for _, k := range known {
		v, ok := rule[k]
		if !ok {
			continue
		}
		if inlineRuleValueNonEmpty(v) {
			return true
		}
	}
	return false
}

func inlineRuleValueNonEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case []any:
		return len(t) > 0
	case []string:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}
