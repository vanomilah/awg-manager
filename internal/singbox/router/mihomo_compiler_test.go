package router

import (
	"context"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestCompileMihomoConfigFromInput_PureNoWrites(t *testing.T) {
	input := &MihomoCompileInput{
		RouterSettings: storage.SingboxRouterSettings{
			RoutingEngine:  "mihomo",
			Enabled:        true,
			MihomoMixedPort: 1099,
		},
		TunIface:      "netaid-tun",
		FinalOutbound: "DIRECT",
		Mode:          mihomo.RuntimeEnforced,
	}

	res, err := CompileMihomoConfigFromInput(input)
	if err != nil {
		t.Fatalf("CompileMihomoConfigFromInput failed: %v", err)
	}

	if res == nil {
		t.Fatal("expected non-nil CompileResult")
	}

	if len(res.ConfigYAML) == 0 {
		t.Fatal("expected non-empty ConfigYAML")
	}

	if res.ConfigDigest == "" {
		t.Fatal("expected non-empty ConfigDigest")
	}

	if res.InputDigest == "" {
		t.Fatal("expected non-empty InputDigest")
	}

	// Verify required listeners include 1099
	has1099TCP := false
	has1099UDP := false
	for _, l := range res.RequiredListeners {
		if l.Port == 1099 && l.Protocol == "tcp" {
			has1099TCP = true
		}
		if l.Port == 1099 && l.Protocol == "udp" {
			has1099UDP = true
		}
	}
	if !has1099TCP || !has1099UDP {
		t.Fatalf("expected 1099 TCP and UDP required listeners: %+v", res.RequiredListeners)
	}
}

func TestCompileMihomoConfigFromInput_RuntimeOff(t *testing.T) {
	input := &MihomoCompileInput{
		RouterSettings: storage.SingboxRouterSettings{
			RoutingEngine: "singbox", // Not mihomo
			Enabled:       false,
		},
		Sidecar: true, // Sidecar with 0 bridge listeners -> RuntimeOff (P0-4)
		NativeResources: mihomo.NativeResources{
			Listeners: nil, // 0 listeners
		},
	}

	res, err := CompileMihomoConfigFromInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Mode != mihomo.RuntimeOff {
		t.Fatalf("expected RuntimeOff, got %s", res.Mode)
	}
	if res.ConfigYAML != nil {
		t.Fatalf("expected nil ConfigYAML for RuntimeOff")
	}
}

type fakeNativeStoreMigration struct {
	groupsImported bool
	rulesImported  bool
}

func (f *fakeNativeStoreMigration) ImportLegacyGroups(groups []storage.ProxyGroup) error {
	f.groupsImported = true
	return nil
}

func (f *fakeNativeStoreMigration) ImportLegacyRules(lines []string) error {
	f.rulesImported = true
	return nil
}

func (f *fakeNativeStoreMigration) HasGroups() bool {
	return f.groupsImported
}

func (f *fakeNativeStoreMigration) HasRules() bool {
	return f.rulesImported
}

func TestMigrateLegacyMihomoResources(t *testing.T) {
	store := &fakeNativeStoreMigration{}
	groups := []storage.ProxyGroup{{Name: "Group1"}}
	rules := []mihomo.Rule{{Domain: []string{"example.com"}, Outbound: "DIRECT"}}

	err := MigrateLegacyMihomoResources(context.Background(), store, groups, rules)
	if err != nil {
		t.Fatalf("MigrateLegacyMihomoResources failed: %v", err)
	}

	if !store.groupsImported || !store.rulesImported {
		t.Fatalf("expected legacy groups and rules imported: groups=%v rules=%v", store.groupsImported, store.rulesImported)
	}

	// Re-run migration: should be a clean no-op
	err = MigrateLegacyMihomoResources(context.Background(), store, groups, rules)
	if err != nil {
		t.Fatalf("second migration call failed: %v", err)
	}
}
