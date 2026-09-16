package router

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hoaxisr/awg-manager/internal/mihomo"
	"github.com/hoaxisr/awg-manager/internal/storage"
	"github.com/hoaxisr/awg-manager/internal/strictfs"
)

// MihomoCompileInput is the immutable, deep-copied aggregate snapshot of all input sources
// required by the Mihomo compiler.
type MihomoCompileInput struct {
	RouterSettings    storage.SingboxRouterSettings `json:"router_settings"`
	NativeResources   mihomo.NativeResources        `json:"native_resources"`
	TunIface          string                        `json:"tun_iface"`
	SubProxies        []map[string]any              `json:"sub_proxies"`
	MihomoRules       []mihomo.Rule                 `json:"mihomo_rules"`
	FinalOutbound     string                        `json:"final_outbound"`
	DynamicCloudCIDRs []string                      `json:"dynamic_cloud_cidrs"`
	TargetBridges     []mihomo.BridgeRef            `json:"target_bridges"`
	Sidecar           bool                          `json:"sidecar"`
	Mode              mihomo.RuntimeMode            `json:"mode"`
}

// ComputeDigest computes a canonical SHA-256 digest of the input snapshot.
func (in *MihomoCompileInput) ComputeDigest() string {
	b, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return strictfs.ComputeBytesDigest(b)
}

// CompileMihomoConfigFromInput is a 100% pure compiler function with zero side-effects.
// It performs zero writes to disk, store, or settings.
func CompileMihomoConfigFromInput(input *MihomoCompileInput) (*mihomo.CompileResult, error) {
	if input == nil {
		return nil, fmt.Errorf("mihomo compiler: nil input")
	}

	inputDigest := input.ComputeDigest()

	// If explicit RuntimeOff, or zero bridge demand in inactive sidecar -> RuntimeOff (P0-4)
	if input.Mode == mihomo.RuntimeOff || (input.Sidecar && len(input.NativeResources.Listeners) == 0) {
		return &mihomo.CompileResult{
			ConfigYAML:        nil,
			ConfigDigest:      "",
			InputDigest:       inputDigest,
			RequiredListeners: nil,
			TargetBridges:     nil,
			Mode:              mihomo.RuntimeOff,
		}, nil
	}

	var yamlBytes []byte
	var err error
	var mode mihomo.RuntimeMode

	if input.Sidecar {
		yamlBytes, err = mihomo.GenerateSidecarConfig(input.NativeResources)
		mode = mihomo.RuntimePermissive
	} else {
		// Deep copy router settings and apply dynamic cloud CIDRs if enabled
		sr := input.RouterSettings
		if sr.KeeneticCloudTunnel {
			sr.DynamicCloudCIDRs = input.DynamicCloudCIDRs
		}
		yamlBytes, err = mihomo.GenerateConfigWithResources(
			sr,
			input.TunIface,
			input.SubProxies,
			input.NativeResources,
			input.MihomoRules,
			input.FinalOutbound,
			nil, // Device proxy listeners remain owned by sing-box
		)
		mode = mihomo.RuntimeEnforced
	}

	if err != nil {
		return nil, fmt.Errorf("compile mihomo config: %w", err)
	}

	configDigest := strictfs.ComputeBytesDigest(yamlBytes)

	// Collect required listeners
	var reqListeners []mihomo.ListenerSpec
	mixedPort := input.RouterSettings.MihomoMixedPort
	if mixedPort <= 0 {
		mixedPort = 1099
	}
	reqListeners = append(reqListeners,
		mihomo.ListenerSpec{Network: "tcp", Protocol: "tcp", Family: "ipv4", Address: "0.0.0.0", Port: uint16(mixedPort), Purpose: "mihomo-mixed-port"},
		mihomo.ListenerSpec{Network: "udp", Protocol: "udp", Family: "ipv4", Address: "0.0.0.0", Port: uint16(mixedPort), Purpose: "mihomo-mixed-port"},
	)

	for _, l := range input.NativeResources.Listeners {
		if l.Port > 0 {
			reqListeners = append(reqListeners,
				mihomo.ListenerSpec{Network: "tcp", Protocol: "tcp", Family: "ipv4", Address: "0.0.0.0", Port: uint16(l.Port), Purpose: l.Name},
				mihomo.ListenerSpec{Network: "udp", Protocol: "udp", Family: "ipv4", Address: "0.0.0.0", Port: uint16(l.Port), Purpose: l.Name},
			)
		}
	}

	return &mihomo.CompileResult{
		ConfigYAML:        yamlBytes,
		ConfigDigest:      configDigest,
		InputDigest:       inputDigest,
		RequiredListeners: reqListeners,
		TargetBridges:     input.TargetBridges,
		Mode:              mode,
	}, nil
}

// NativeStoreWithMigration abstracts migration capabilities on mihomonative.Store.
type NativeStoreWithMigration interface {
	ImportLegacyGroups(groups []storage.ProxyGroup) error
	ImportLegacyRules(lines []string) error
	HasGroups() bool
	HasRules() bool
}

// MigrateLegacyMihomoResources executes legacy groups and rules migration as a dedicated,
// isolated transaction boundary before compilation.
func MigrateLegacyMihomoResources(ctx context.Context, store NativeStoreWithMigration, groups []storage.ProxyGroup, rules []mihomo.Rule) error {
	if store == nil {
		return nil
	}

	if !store.HasGroups() && len(groups) > 0 {
		if err := store.ImportLegacyGroups(groups); err != nil {
			return fmt.Errorf("migrate legacy groups: %w", err)
		}
	}

	if !store.HasRules() && len(rules) > 0 {
		var lines []string
		for _, r := range rules {
			lines = append(lines, mihomo.ConvertSingboxRuleToMihomo(r)...)
		}
		if err := store.ImportLegacyRules(lines); err != nil {
			return fmt.Errorf("migrate legacy rules: %w", err)
		}
	}

	return nil
}
