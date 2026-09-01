package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
)

// GenerateMihomoConfig translates the current router settings, tunnels, and rules
// into a Mihomo YAML configuration and writes it to the Mihomo config directory.
// It is called by DynamicEngine right before reloading/starting Mihomo.
func (s *ServiceImpl) GenerateMihomoConfig() error {
	settings, err := s.deps.Settings.Load()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	sr := settings.SingboxRouter

	tunIface, _ := s.tunModeIface()

	// 1. Get all subscription proxies
	var subProxies []map[string]any
	subscriptionsLoaded := false
	if s.deps.Orch != nil {
		if raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotSubscriptions); loadErr == nil && len(raw) != 0 {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if unmarshalErr := json.Unmarshal(raw, &slot); unmarshalErr != nil {
				return fmt.Errorf("decode subscription outbound slot: %w", unmarshalErr)
			}
			subProxies = slot.Outbounds
			subscriptionsLoaded = true
		}
	}
	if !subscriptionsLoaded && s.deps.SubscriptionComposites != nil && s.deps.SubscriptionComposites.src != nil {
		subProxies = s.deps.SubscriptionComposites.src.SubscriptionOutbounds()
	}

	// 2. Load router config to get custom outbounds and rules
	cfg, err := s.loadRouterConfig()
	if err != nil {
		cfg = NewEmptyConfig()
	}

	// 3. Serialize own outbounds to maps
	var ownOutbounds []map[string]any
	if ownJSON, err := json.Marshal(cfg.Outbounds); err == nil {
		_ = json.Unmarshal(ownJSON, &ownOutbounds)
	}
	// Standalone sing-box tunnels live in 10-tunnels.json. Routing groups and
	// device proxies can reference their tags, so Mihomo must receive the
	// actual proxy definitions as well as router/subscription outbounds.
	if s.deps.Orch != nil {
		if raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotTunnels); loadErr == nil && len(raw) != 0 {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if unmarshalErr := json.Unmarshal(raw, &slot); unmarshalErr != nil {
				return fmt.Errorf("decode tunnel outbound slot: %w", unmarshalErr)
			}
			ownOutbounds = append(ownOutbounds, slot.Outbounds...)
		}
	}
	// AWG-direct outbounds live in their own sing-box slot, so they are not
	// present in cfg.Outbounds. Prefer that persisted slot as the source of
	// truth: the live catalog may be temporarily empty during daemon startup.
	awgLoaded := false
	if s.deps.Orch != nil {
		if raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotAwg); loadErr == nil {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if unmarshalErr := json.Unmarshal(raw, &slot); unmarshalErr != nil {
				return fmt.Errorf("decode AWG outbound slot: %w", unmarshalErr)
			}
			ownOutbounds = append(ownOutbounds, slot.Outbounds...)
			awgLoaded = true
		}
	}
	// Tests and legacy installs may not have an orchestrator slot available;
	// in that case synthesize the same direct outbounds from the live catalog.
	if !awgLoaded && s.deps.AWGTags != nil {
		awgTags, listErr := s.deps.AWGTags.ListTags(context.Background())
		if listErr != nil {
			return fmt.Errorf("list AWG outbounds: %w", listErr)
		}
		for _, awg := range awgTags {
			if awg.Tag == "" || awg.Iface == "" {
				continue
			}
			ownOutbounds = append(ownOutbounds, map[string]any{
				"type":           "direct",
				"tag":            awg.Tag,
				"bind_interface": awg.Iface,
			})
		}
	}

	// 4. Merge subscription proxies and own outbounds
	allProxies := append(subProxies, ownOutbounds...)

	// 5. Map routing rules to mihomo.Rule
	var mihomoRules []mihomo.Rule
	for _, r := range cfg.Route.Rules {
		mihomoRules = append(mihomoRules, mihomo.Rule{
			DomainSuffix: r.DomainSuffix,
			Domain:       r.Domain,
			IPCIDR:       r.IPCIDR,
			SourceIPCIDR: r.SourceIPCIDR,
			RuleSet:      r.RuleSet,
			Action:       r.Action,
			Outbound:     r.Outbound,
		})
	}

	// 6. Device-proxy listeners remain owned by sing-box. That daemon stays
	// alive for standalone tunnels while Mihomo is selected, so duplicating
	// its listener ports here makes Mihomo abort startup with EADDRINUSE.
	// The shared transparent-routing listeners below are owned only by Mihomo.
	var dpListeners []mihomo.DeviceProxyListener

	resources := mihomo.NativeResources{}
	if s.deps.MihomoNativeProxies != nil {
		if sr.RoutingEngine == "mihomo" {
			if err := s.deps.MihomoNativeProxies.ImportLegacyGroups(sr.ProxyGroups); err != nil {
				return fmt.Errorf("migrate legacy Mihomo groups: %w", err)
			}
			var legacyRuleLines []string
			for _, rule := range mihomoRules {
				legacyRuleLines = append(legacyRuleLines, mihomo.ConvertSingboxRuleToMihomo(rule)...)
			}
			if err := s.deps.MihomoNativeProxies.ImportLegacyRules(legacyRuleLines); err != nil {
				return fmt.Errorf("migrate legacy Mihomo rules: %w", err)
			}
		}
		for _, proxy := range s.deps.MihomoNativeProxies.ConfigProxies() {
			resources.Proxies = append(resources.Proxies, mihomo.Proxy(proxy))
		}
		resources.ProxyProviders = s.deps.MihomoNativeProxies.ConfigProviders()
		for _, rawGroup := range s.deps.MihomoNativeProxies.ConfigProviderGroups() {
			group := mihomo.ProxyGroup{}
			group.Name, _ = rawGroup["name"].(string)
			group.Type, _ = rawGroup["type"].(string)
			group.Use, _ = rawGroup["use"].([]string)
			group.Proxies, _ = rawGroup["proxies"].([]string)
			group.URL, _ = rawGroup["url"].(string)
			group.Interval, _ = rawGroup["interval"].(int)
			if lazyVal, lazyOk := rawGroup["lazy"].(bool); lazyOk {
				group.Lazy = &lazyVal
			}
			group.Strategy, _ = rawGroup["strategy"].(string)
			group.Tolerance, _ = rawGroup["tolerance"].(int)
			group.Timeout, _ = rawGroup["timeout"].(int)
			group.MaxFailedTimes, _ = rawGroup["max-failed-times"].(int)
			group.DisableUDP, _ = rawGroup["disable-udp"].(bool)
			group.IncludeAll, _ = rawGroup["include-all"].(bool)
			group.IncludeAllProxies, _ = rawGroup["include-all-proxies"].(bool)
			group.IncludeAllProviders, _ = rawGroup["include-all-providers"].(bool)
			group.Filter, _ = rawGroup["filter"].(string)
			group.ExcludeFilter, _ = rawGroup["exclude-filter"].(string)
			group.InterfaceName, _ = rawGroup["interface-name"].(string)
			group.ExcludeType, _ = rawGroup["exclude-type"].(string)
			group.ExpectedStatus, _ = rawGroup["expected-status"].(string)
			group.Hidden, _ = rawGroup["hidden"].(bool)
			group.Icon, _ = rawGroup["icon"].(string)
			resources.ProxyGroups = append(resources.ProxyGroups, group)
		}
		if settings.CreateNDMSProxyForSingbox {
			for _, bridge := range s.deps.MihomoNativeProxies.ConfigBridgeListeners() {
				resources.Listeners = append(resources.Listeners, mihomo.Listener{
					Name: bridge.Name, Type: "mixed", Port: bridge.Port,
					Listen: "127.0.0.1", Proxy: bridge.Proxy,
				})
			}
		}
		resources.GroupsAuthoritative = s.deps.MihomoNativeProxies.HasGroups()
		resources.Rules = s.deps.MihomoNativeProxies.ConfigRules()
		resources.RulesAuthoritative = s.deps.MihomoNativeProxies.HasRules()
		resources.RuleProviders = s.deps.MihomoNativeProxies.ConfigRuleProviders()
	}

	for _, srv := range cfg.DNS.Servers {
		sni := ""
		if srv.TLS != nil {
			sni = srv.TLS.ServerName
		}
		resources.DNSServers = append(resources.DNSServers, mihomo.DNSServerSpec{
			Tag:        srv.Tag,
			Type:       srv.Type,
			Server:     srv.Server,
			ServerPort: srv.ServerPort,
			Detour:     srv.Detour,
			SNI:        sni,
		})
	}

	for _, r := range cfg.DNS.Rules {
		resources.DNSRules = append(resources.DNSRules, mihomo.DNSRuleSpec{
			Domain:        r.Domain,
			DomainSuffix:  r.DomainSuffix,
			DomainKeyword: r.DomainKeyword,
			RuleSet:       r.RuleSet,
			Server:        r.Server,
		})
	}

	// 7. Generate either the transparent-routing config or a minimal sidecar.
	// Native ProxyN exports must stay backed by a live loopback listener even
	// while sing-box is the routing engine (or global routing is disabled).
	// Preserve full-config generation for an explicitly selected Mihomo engine
	// even when routing is currently disabled (the UI/tests use generation as a
	// preview). Disabled routing needs the exports-only profile only when native
	// bridge listeners actually have to stay alive.
	sidecar := sr.RoutingEngine != "mihomo" || (!sr.Enabled && len(resources.Listeners) > 0)
	if sidecar && len(resources.Listeners) == 0 {
		return nil
	}
	var yamlBytes []byte
	if sidecar {
		yamlBytes, err = mihomo.GenerateSidecarConfig(resources)
	} else {
		yamlBytes, err = mihomo.GenerateConfigWithResources(sr, tunIface, allProxies, resources, mihomoRules, cfg.Route.Final, dpListeners)
	}
	if err != nil {
		return fmt.Errorf("generate mihomo config: %w", err)
	}

	configDir := s.deps.MihomoConfigDir
	if configDir == "" && s.deps.Engine != nil {
		configDir = s.deps.Engine.ConfigDir()
	}
	if configDir == "" {
		return fmt.Errorf("Mihomo config directory is unavailable")
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create mihomo config directory: %w", err)
	}
	cfgPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(cfgPath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("write mihomo config.yaml: %w", err)
	}

	return nil
}
