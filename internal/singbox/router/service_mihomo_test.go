package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/mihomonative"
	"github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
	"github.com/hoaxisr/awg-manager/internal/storage"
	"gopkg.in/yaml.v3"
)

type fakeSubscriptionOutboundSource struct {
	outbounds []map[string]any
}

func (f *fakeSubscriptionOutboundSource) SubscriptionOutbounds() []map[string]any {
	return append([]map[string]any(nil), f.outbounds...)
}

func newMihomoConfigTestService(t *testing.T, routingEngine string) (*ServiceImpl, string) {
	t.Helper()

	singboxDir := t.TempDir()
	mihomoDir := t.TempDir()
	orch := orchestrator.New(singboxDir, nil)
	if err := orch.Register(orchestrator.SlotMeta{Slot: orchestrator.SlotRouter, Filename: "20-router.json"}); err != nil {
		t.Fatalf("register SlotRouter: %v", err)
	}
	if err := orch.SetEnabledSilent(orchestrator.SlotRouter, true); err != nil {
		t.Fatalf("enable SlotRouter: %v", err)
	}

	cfg := NewEmptyConfig()
	cfg.Outbounds = []Outbound{
		{Type: "selector", Tag: "route-group", Outbounds: []string{"sub-a", "direct"}},
		{Type: "direct", Tag: "direct"},
	}
	cfg.Route.Rules = []Rule{
		{DomainSuffix: []string{"example.com"}, Outbound: "route-group"},
	}
	cfg.Route.Final = "direct"
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}
	if err := orch.SaveSilent(orchestrator.SlotRouter, data); err != nil {
		t.Fatalf("SaveSilent SlotRouter: %v", err)
	}

	store := newTestSettingsStore(t, storage.SingboxRouterSettings{
		RoutingMode:   "tproxy",
		RoutingEngine: routingEngine,
		DeviceMode:    "all",
		WANAutoDetect: true,
	})

	svc := &ServiceImpl{deps: Deps{
		Settings: store,
		Engine:   &fakeSingbox{dir: mihomoDir},
		Orch:     orch,
		SubscriptionComposites: NewSubscriptionCompositesAdapter(&fakeSubscriptionOutboundSource{
			outbounds: []map[string]any{
				{
					"type":        "vless",
					"tag":         "sub-a",
					"server":      "sub.example",
					"server_port": 443,
					"uuid":        "11111111-1111-1111-1111-111111111111",
				},
			},
		}),
		DeviceProxyInstances: func() []DeviceProxyInstance {
			return []DeviceProxyInstance{{
				ID:               "dev1",
				Port:             1099,
				SelectedOutbound: "route-group",
				Enabled:          true,
			}}
		},
	}}
	return svc, mihomoDir
}

func attachNativeBridge(t *testing.T, svc *ServiceImpl, port int) *mihomonative.Store {
	t.Helper()
	native, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := native.CreateProxy("vless://11111111-1111-1111-1111-111111111111@native.example:443?type=xhttp#Native", mihomonative.EngineMihomo, mihomonative.EngineMihomo)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.SetBridge("proxy", nodes[0].ID, mihomonative.ProxyBridge{
		ListenPort: port, ProxyIndex: 7, ProxyInterface: "Proxy7", KernelInterface: "t2s7",
	}); err != nil {
		t.Fatal(err)
	}
	svc.deps.MihomoNativeProxies = native
	return native
}

func TestGenerateMihomoConfig_WritesConfigForMihomoEngine(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml): %v", err)
	}

	var cfg mihomo.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if cfg.TProxyPort != 51271 || cfg.RedirPort != 51272 {
		t.Fatalf("unexpected ports: tproxy=%d redir=%d", cfg.TProxyPort, cfg.RedirPort)
	}
	if len(cfg.Proxies) != 1 || cfg.Proxies[0]["name"] != "sub-a" {
		t.Fatalf("subscription proxy not materialized correctly: %#v", cfg.Proxies)
	}
	if len(cfg.ProxyGroups) == 0 || cfg.ProxyGroups[0].Name != "route-group" {
		t.Fatalf("route-group not materialized: %#v", cfg.ProxyGroups)
	}
	if len(cfg.Listeners) != 0 {
		t.Fatalf("unexpected Mihomo listeners: %#v", cfg.Listeners)
	}
	rules := strings.Join(cfg.Rules, "\n")
	for _, want := range []string{
		"DOMAIN-SUFFIX,example.com,route-group",
		"MATCH,DIRECT",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("generated config missing rule %q:\n%s", want, rules)
		}
	}
}

func TestGenerateMihomoConfig_MigratesLegacyGroupsAndRulesToNativeStore(t *testing.T) {
	svc, _ := newMihomoConfigTestService(t, "mihomo")
	native, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc.deps.MihomoNativeProxies = native
	err = svc.deps.Settings.Update(func(s *storage.Settings) error {
		s.SingboxRouter.ProxyGroups = []storage.ProxyGroup{{Name: "Block", Type: "select", Proxies: []string{"block", "direct"}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatal(err)
	}
	if !native.HasGroups() || !native.HasRules() {
		t.Fatal("native routing ownership was not activated")
	}
	groups := native.ListGroups()
	if len(groups) != 1 || groups[0].Name != "Block" || groups[0].Proxies[0] != "REJECT" {
		t.Fatalf("groups=%#v", groups)
	}
	rules := native.ListRules()
	if len(rules) != 1 || rules[0].Type != "DOMAIN-SUFFIX" || rules[0].Payload != "example.com" {
		t.Fatalf("rules=%#v", rules)
	}
}

func TestGenerateMihomoConfig_LoadsAWGDirectOutboundsFromSlot(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")
	if err := svc.deps.Orch.Register(orchestrator.SlotMeta{
		Slot: orchestrator.SlotAwg, Filename: "15-awg.json", AlwaysOn: true,
	}); err != nil {
		t.Fatalf("register SlotAwg: %v", err)
	}
	raw := []byte(`{"outbounds":[{"type":"direct","tag":"awg-awg20","bind_interface":"nwg1"}]}`)
	if err := svc.deps.Orch.SaveSilent(orchestrator.SlotAwg, raw); err != nil {
		t.Fatalf("SaveSilent SlotAwg: %v", err)
	}

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml): %v", err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	for _, proxy := range cfg.Proxies {
		if proxy["name"] == "awg-awg20" {
			if proxy["type"] != "direct" || proxy["interface-name"] != "nwg1" {
				t.Fatalf("AWG proxy = %#v", proxy)
			}
			return
		}
	}
	t.Fatalf("AWG proxy missing: %#v", cfg.Proxies)
}

func TestGenerateMihomoConfig_LoadsDisabledSubscriptionSlot(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")
	if err := svc.deps.Orch.Register(orchestrator.SlotMeta{
		Slot: orchestrator.SlotSubscriptions, Filename: "40-subscriptions.json",
	}); err != nil {
		t.Fatalf("register SlotSubscriptions: %v", err)
	}
	raw := []byte(`{"outbounds":[{"type":"vless","tag":"tt-node","server":"vpn.example","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111"},{"type":"selector","tag":"ttUS","outbounds":["tt-node"]}]}`)
	if err := svc.deps.Orch.SaveSilent(orchestrator.SlotSubscriptions, raw); err != nil {
		t.Fatalf("SaveSilent SlotSubscriptions: %v", err)
	}
	if err := svc.deps.Orch.SetEnabledSilent(orchestrator.SlotSubscriptions, false); err != nil {
		t.Fatalf("disable SlotSubscriptions: %v", err)
	}

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml): %v", err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	foundProxy, foundGroup := false, false
	for _, proxy := range cfg.Proxies {
		foundProxy = foundProxy || proxy["name"] == "tt-node"
	}
	for _, group := range cfg.ProxyGroups {
		foundGroup = foundGroup || group.Name == "ttUS"
	}
	if !foundProxy || !foundGroup {
		t.Fatalf("subscription slot not materialized: proxies=%#v groups=%#v", cfg.Proxies, cfg.ProxyGroups)
	}
}

func TestGenerateMihomoConfig_LoadsStandaloneTunnelSlot(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")
	if err := svc.deps.Orch.Register(orchestrator.SlotMeta{
		Slot: orchestrator.SlotTunnels, Filename: "10-tunnels.json", AlwaysOn: true,
	}); err != nil {
		t.Fatalf("register SlotTunnels: %v", err)
	}
	raw := []byte(`{"outbounds":[{"type":"vless","tag":"ttUS","server":"vpn.example","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111"}]}`)
	if err := svc.deps.Orch.SaveSilent(orchestrator.SlotTunnels, raw); err != nil {
		t.Fatalf("SaveSilent SlotTunnels: %v", err)
	}

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml): %v", err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	for _, proxy := range cfg.Proxies {
		if proxy["name"] == "ttUS" {
			return
		}
	}
	t.Fatalf("standalone tunnel missing: %#v", cfg.Proxies)
}

func TestGenerateMihomoConfig_SkipsWriteWhenEngineIsSingbox(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "sing-box")

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}

	_, err := os.Stat(filepath.Join(mihomoDir, "config.yaml"))
	if !os.IsNotExist(err) {
		t.Fatalf("config.yaml existence error = %v, want not exist", err)
	}
}

func TestGenerateMihomoConfig_SingboxPrimaryWritesExportsOnlySidecar(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "sing-box")
	attachNativeBridge(t, svc, 12007)

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ExternalCtl != "127.0.0.1:9090" || !cfg.DNS.Enable || cfg.DNS.Listen != "" {
		t.Fatalf("sidecar controller/DNS=%q %#v", cfg.ExternalCtl, cfg.DNS)
	}
	if cfg.TProxyPort != 0 || cfg.RedirPort != 0 || cfg.MixedPort != 0 || cfg.Port != 0 || cfg.SocksPort != 0 || cfg.Tun != nil {
		t.Fatalf("sidecar exposes routing/global listeners: %#v", cfg)
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Name == "routing-tproxy" || cfg.Listeners[0].Port != 12007 {
		t.Fatalf("sidecar listeners=%#v", cfg.Listeners)
	}
}

func TestGenerateMihomoConfig_PrimaryWithNDMSExportOffOmitsBridgeListener(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")
	attachNativeBridge(t, svc, 12008)
	err := svc.deps.Settings.Update(func(s *storage.Settings) error {
		s.CreateNDMSProxyForSingbox = false
		s.SingboxRouter.Enabled = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, listener := range cfg.Listeners {
		if strings.HasPrefix(listener.Name, "mihomo-native-") {
			t.Fatalf("NDMS export disabled but bridge listener remains: %#v", cfg.Listeners)
		}
	}
}

func TestGenerateMihomoConfig_PicksTunInterfaceForPolicyTun(t *testing.T) {
	svc, mihomoDir := newMihomoConfigTestService(t, "mihomo")

	err := svc.deps.Settings.Update(func(s *storage.Settings) error {
		s.SingboxRouter.RoutingMode = statePolicyTun
		s.OpkgTun = &storage.OpkgTunState{
			Mode:        statePolicyTun,
			Provisioned: true,
			Index:       3,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Settings.Update: %v", err)
	}

	if err := svc.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(mihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(config.yaml): %v", err)
	}
	var cfg mihomo.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if cfg.Tun == nil || !cfg.Tun.Enable {
		t.Fatal("Tun must be enabled for policy-tun mode")
	}
	if cfg.Tun.Device != tunIfaceName(3) {
		t.Fatalf("Tun.Device = %q, want %q", cfg.Tun.Device, tunIfaceName(3))
	}
}
