package router

import (
	"context"
	"encoding/json"
	"errors"
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
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotSubscriptions)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return fmt.Errorf("load subscription outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
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
		return fmt.Errorf("load router config: %w", err)
	}

	// 3. Serialize own outbounds to maps
	var ownOutbounds []map[string]any
	if len(cfg.Outbounds) > 0 {
		ownJSON, err := json.Marshal(cfg.Outbounds)
		if err != nil {
			return fmt.Errorf("marshal router outbounds: %w", err)
		}
		if err := json.Unmarshal(ownJSON, &ownOutbounds); err != nil {
			return fmt.Errorf("unmarshal router outbounds: %w", err)
		}
	}
	// Standalone sing-box tunnels live in 10-tunnels.json. Routing groups and
	// device proxies can reference their tags, so Mihomo must receive the
	// actual proxy definitions as well as router/subscription outbounds.
	if s.deps.Orch != nil {
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotTunnels)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return fmt.Errorf("load tunnel outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
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
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotAwg)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return fmt.Errorf("load AWG outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
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
		if err := s.deps.MihomoNativeProxies.ValidateRuntimeRules(); err != nil {
			return fmt.Errorf("validate mihomo native rules: %w", err)
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
	if sr.KeeneticCloudTunnel {
		sr.DynamicCloudCIDRs = s.dynamicCloudCIDRs()
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

// MigrateLegacyMihomoResources executes legacy groups and rules migration in an isolated transaction.
func (s *ServiceImpl) MigrateLegacyMihomoResources(ctx context.Context) error {
	if s.deps.MihomoNativeProxies == nil {
		return nil
	}
	settings, err := s.deps.Settings.Get()
	if err != nil {
		return err
	}
	sr := settings.SingboxRouter
	if sr.RoutingEngine != "mihomo" {
		return nil
	}
	cfg, err := s.loadRouterConfig()
	if err != nil {
		return err
	}
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
	return MigrateLegacyMihomoResources(ctx, s.deps.MihomoNativeProxies, sr.ProxyGroups, mihomoRules)
}

// AssembleCompileInput builds the immutable snapshot of all inputs needed by CompileMihomoConfigFromInput.
func (s *ServiceImpl) AssembleCompileInput(ctx context.Context) (*MihomoCompileInput, error) {
	settings, err := s.deps.Settings.Load()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	sr := settings.SingboxRouter

	tunIface, _ := s.tunModeIface()

	var subProxies []map[string]any
	subscriptionsLoaded := false
	if s.deps.Orch != nil {
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotSubscriptions)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return nil, fmt.Errorf("load subscription outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if unmarshalErr := json.Unmarshal(raw, &slot); unmarshalErr != nil {
				return nil, fmt.Errorf("decode subscription outbound slot: %w", unmarshalErr)
			}
			subProxies = slot.Outbounds
			subscriptionsLoaded = true
		}
	}
	if !subscriptionsLoaded && s.deps.SubscriptionComposites != nil && s.deps.SubscriptionComposites.src != nil {
		subProxies = s.deps.SubscriptionComposites.src.SubscriptionOutbounds()
	}

	cfg, err := s.loadRouterConfig()
	if err != nil {
		return nil, fmt.Errorf("load router config: %w", err)
	}

	var ownOutbounds []map[string]any
	if len(cfg.Outbounds) > 0 {
		ownJSON, err := json.Marshal(cfg.Outbounds)
		if err != nil {
			return nil, fmt.Errorf("marshal router outbounds: %w", err)
		}
		if err := json.Unmarshal(ownJSON, &ownOutbounds); err != nil {
			return nil, fmt.Errorf("unmarshal router outbounds: %w", err)
		}
	}
	if s.deps.Orch != nil {
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotTunnels)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return nil, fmt.Errorf("load tunnel outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if err := json.Unmarshal(raw, &slot); err != nil {
				return nil, fmt.Errorf("decode tunnel outbound slot: %w", err)
			}
			ownOutbounds = append(ownOutbounds, slot.Outbounds...)
		}
	}
	awgLoaded := false
	if s.deps.Orch != nil {
		raw, loadErr := s.deps.Orch.LoadEffective(orchestrator.SlotAwg)
		if loadErr != nil && !errors.Is(loadErr, orchestrator.ErrUnknownSlot) {
			return nil, fmt.Errorf("load AWG outbound slot: %w", loadErr)
		}
		if len(raw) != 0 {
			var slot struct {
				Outbounds []map[string]any `json:"outbounds"`
			}
			if err := json.Unmarshal(raw, &slot); err != nil {
				return nil, fmt.Errorf("decode AWG outbound slot: %w", err)
			}
			ownOutbounds = append(ownOutbounds, slot.Outbounds...)
			awgLoaded = true
		}
	}
	if !awgLoaded && s.deps.AWGTags != nil {
		awgTags, listErr := s.deps.AWGTags.ListTags(ctx)
		if listErr != nil {
			return nil, fmt.Errorf("list AWG outbounds: %w", listErr)
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

	allProxies := append(subProxies, ownOutbounds...)

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

	resources := mihomo.NativeResources{}
	var targetBridges []mihomo.BridgeRef
	if s.deps.MihomoNativeProxies != nil {
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
		if err := s.deps.MihomoNativeProxies.ValidateRuntimeRules(); err != nil {
			return nil, fmt.Errorf("validate mihomo native rules: %w", err)
		}
		resources.GroupsAuthoritative = s.deps.MihomoNativeProxies.HasGroups()
		resources.Rules = s.deps.MihomoNativeProxies.ConfigRules()
		resources.RulesAuthoritative = s.deps.MihomoNativeProxies.HasRules()
		resources.RuleProviders = s.deps.MihomoNativeProxies.ConfigRuleProviders()

		for _, nb := range s.deps.MihomoNativeProxies.ListBridges() {
			targetBridges = append(targetBridges, mihomo.BridgeRef{
				ProxyIndex:      nb.Bridge.ProxyIndex,
				ProxyInterface:  nb.Bridge.ProxyInterface,
				KernelInterface: nb.Bridge.KernelInterface,
				LegacyOwner:     nb.LegacyOwner,
			})
		}
	}

	isPrimary := sr.Enabled && sr.RoutingEngine == "mihomo"
	hasNativeListeners := len(resources.Listeners) > 0

	var sidecar bool
	var rMode mihomo.RuntimeMode

	if isPrimary {
		sidecar = false
		rMode = mihomo.RuntimeEnforced
	} else if hasNativeListeners {
		sidecar = true
		rMode = mihomo.RuntimePermissive
	} else {
		sidecar = false
		rMode = mihomo.RuntimeOff
	}

	return &MihomoCompileInput{
		RouterSettings:    sr,
		NativeResources:   resources,
		TunIface:          tunIface,
		SubProxies:        allProxies,
		MihomoRules:       mihomoRules,
		FinalOutbound:     cfg.Route.Final,
		DynamicCloudCIDRs: s.dynamicCloudCIDRs(),
		TargetBridges:     targetBridges,
		Sidecar:           sidecar,
		Mode:              rMode,
	}, nil
}

// CompileMihomoConfig gathers current inputs and executes pure compilation.
func (s *ServiceImpl) CompileMihomoConfig(ctx context.Context) (*mihomo.CompileResult, error) {
	input, err := s.AssembleCompileInput(ctx)
	if err != nil {
		return nil, err
	}
	return CompileMihomoConfigFromInput(input)
}
