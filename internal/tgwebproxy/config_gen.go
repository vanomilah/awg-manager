package tgwebproxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateTelemtDirectConfig produces the TOML configuration for direct MTProxy (port 8443, Fake-TLS).
func GenerateTelemtDirectConfig(cfg Config) string {
	var b strings.Builder
	b.WriteString("[general]\n")
	b.WriteString("use_middle_proxy = false\n")
	b.WriteString("fast_mode = true\n")
	b.WriteString("log_level = \"normal\"\n")
	b.WriteString("upstream_connect_failfast_hard_errors = false\n")
	b.WriteString("direct_relay_copy_buf_c2s_bytes = 262144\n")
	b.WriteString("direct_relay_copy_buf_s2c_bytes = 524288\n")
	b.WriteString("quota_state_path = \"/opt/var/run/telemt.limit.json\"\n")
	b.WriteString("beobachten_file = \"/tmp/cache/beobachten.txt\"\n\n")

	b.WriteString("[general.modes]\n")
	b.WriteString("classic = false\n")
	b.WriteString("secure = false\n")
	b.WriteString("tls = true\n\n")

	b.WriteString("[network]\n")
	b.WriteString("ipv6 = false\n")
	b.WriteString("prefer = 4\n")
	b.WriteString("stun_use = false\n\n")

	port := cfg.DirectPort
	if port <= 0 {
		port = DefaultDirectPort
	}
	b.WriteString("[server]\n")
	fmt.Fprintf(&b, "port = %d\n\n", port)

	b.WriteString("[server.api]\n")
	b.WriteString("enabled = false\n\n")

	b.WriteString("[[server.listeners]]\n")
	b.WriteString("ip = \"0.0.0.0\"\n")
	fmt.Fprintf(&b, "port = %d\n\n", port)

	domain := strings.TrimSpace(cfg.TlsDomain)
	if domain == "" {
		domain = DefaultTlsDomain
	}
	b.WriteString("[censorship]\n")
	fmt.Fprintf(&b, "tls_domain = %q\n", domain)
	b.WriteString("mask = true\n")
	b.WriteString("tls_emulation = true\n")
	b.WriteString("tls_front_dir = \"/opt/etc/telemt/tlsfront\"\n")
	fmt.Fprintf(&b, "mask_host = %q\n", domain)
	b.WriteString("mask_shape_hardening_aggressive_mode = true\n\n")

	b.WriteString("[access.users]\n")
	if len(cfg.Users) > 0 {
		wroteAny := false
		for idx, u := range cfg.Users {
			if !u.Enabled || strings.TrimSpace(u.Secret) == "" {
				continue
			}
			keyName := fmt.Sprintf("user%d", idx+1)
			fmt.Fprintf(&b, "%s = %q\n", keyName, u.Secret)
			wroteAny = true
		}
		if !wroteAny {
			fmt.Fprintf(&b, "user1 = %q\n", "00000000000000000000000000000000")
		}
	} else {
		fmt.Fprintf(&b, "user1 = %q\n", cfg.Secret)
	}
	if cfg.LegacySecret != "" {
		fmt.Fprintf(&b, "user_legacy = %q\n", cfg.LegacySecret)
	}
	b.WriteString("\n")

	b.WriteString("[[upstreams]]\n")
	b.WriteString("type = \"direct\"\n")
	if cfg.UpstreamDevice != "" && cfg.UpstreamDevice != "direct" {
		fmt.Fprintf(&b, "bindtodevice = %q\n", cfg.UpstreamDevice)
	}

	return b.String()
}

// GenerateTelemtRawConfig produces the TOML configuration for loopback raw MTProxy (port 2398).
func GenerateTelemtRawConfig(cfg Config) string {
	var b strings.Builder
	b.WriteString("[general]\n")
	b.WriteString("use_middle_proxy = false\n")
	b.WriteString("log_level = \"normal\"\n")
	b.WriteString("upstream_connect_failfast_hard_errors = false\n")
	b.WriteString("direct_relay_copy_buf_c2s_bytes = 262144\n")
	b.WriteString("quota_state_path = \"/opt/var/run/telemt-raw.limit.json\"\n\n")

	b.WriteString("[general.modes]\n")
	b.WriteString("classic = true\n")
	b.WriteString("secure = true\n")
	b.WriteString("tls = false\n\n")

	b.WriteString("[network]\n")
	b.WriteString("ipv6 = false\n")
	b.WriteString("prefer = 4\n")
	b.WriteString("stun_use = false\n\n")

	b.WriteString("[server]\n")
	fmt.Fprintf(&b, "port = %d\n\n", DefaultRawPort)

	b.WriteString("[server.api]\n")
	b.WriteString("enabled = false\n\n")

	b.WriteString("[[server.listeners]]\n")
	b.WriteString("ip = \"127.0.0.1\"\n")
	fmt.Fprintf(&b, "port = %d\n\n", DefaultRawPort)

	b.WriteString("[censorship]\n")
	b.WriteString("mask = false\n")
	b.WriteString("tls_emulation = false\n\n")

	b.WriteString("[access.users]\n")
	if len(cfg.Users) > 0 {
		wroteAny := false
		for idx, u := range cfg.Users {
			if !u.Enabled || strings.TrimSpace(u.Secret) == "" {
				continue
			}
			keyName := fmt.Sprintf("user%d", idx+1)
			fmt.Fprintf(&b, "%s = %q\n", keyName, u.Secret)
			wroteAny = true
		}
		if !wroteAny {
			fmt.Fprintf(&b, "user1 = %q\n", "00000000000000000000000000000000")
		}
	} else {
		fmt.Fprintf(&b, "user1 = %q\n", cfg.Secret)
	}
	if cfg.LegacySecret != "" {
		fmt.Fprintf(&b, "user_legacy = %q\n", cfg.LegacySecret)
	}
	b.WriteString("\n")

	b.WriteString("[[upstreams]]\n")
	b.WriteString("type = \"direct\"\n")
	if cfg.UpstreamDevice != "" && cfg.UpstreamDevice != "direct" {
		fmt.Fprintf(&b, "bindtodevice = %q\n", cfg.UpstreamDevice)
	}

	return b.String()
}

// GenerateEndpointProfilesJSON produces the JSON profiles for a specific tproxy-server endpoint.
func GenerateEndpointProfilesJSON(ep WebProxyEndpoint, cfg Config) ([]byte, error) {
	backend := cfg.Backend
	if backend == "" {
		backend = DefaultBackend
	}
	mode := EffectiveEndpointCarrierMode(ep, cfg)

	var profiles []map[string]interface{}
	if len(cfg.Users) > 0 {
		for idx, u := range cfg.Users {
			if !u.Enabled || strings.TrimSpace(u.Secret) == "" {
				continue
			}
			profName := u.ID
			if len(profiles) == 0 || profName == "" {
				profName = "default"
			} else if profName == "default" {
				profName = fmt.Sprintf("user_%d", idx+1)
			}
			profiles = append(profiles, map[string]interface{}{
				"name":         profName,
				"secret":       u.Secret,
				"backend":      backend,
				"carrier_mode": mode,
			})
		}
		if len(profiles) == 0 {
			profiles = append(profiles, map[string]interface{}{
				"name":         "default",
				"secret":       "00000000000000000000000000000000",
				"backend":      backend,
				"carrier_mode": mode,
			})
		}
	} else {
		profiles = []map[string]interface{}{
			{
				"name":         "default",
				"secret":       cfg.Secret,
				"backend":      backend,
				"carrier_mode": mode,
			},
		}
	}

	if cfg.LegacySecret != "" {
		profiles = append(profiles, map[string]interface{}{
			"name":         "legacy",
			"secret":       cfg.LegacySecret,
			"backend":      backend,
			"carrier_mode": mode,
		})
	}

	data := map[string]interface{}{
		"profiles": profiles,
	}
	return json.MarshalIndent(data, "", "  ")
}

// GenerateProfilesJSON produces the JSON profiles for primary tproxy-server.
func GenerateProfilesJSON(cfg Config) ([]byte, error) {
	eps := cfg.GetEndpoints()
	if len(eps) > 0 {
		return GenerateEndpointProfilesJSON(eps[0], cfg)
	}
	return GenerateEndpointProfilesJSON(WebProxyEndpoint{
		Preset:      cfg.NetworkPreset,
		CarrierMode: cfg.CarrierMode,
		IsCDN:       cfg.Scenario == ScenarioCDNHTTP || cfg.Scenario == ScenarioDual,
	}, cfg)
}

// GenerateEndpointTproxyConfigJSON produces config JSON for an individual tproxy-server endpoint.
func GenerateEndpointTproxyConfigJSON(ep WebProxyEndpoint, profilesPath, tokenKeyPath string) ([]byte, error) {
	listenPort := ep.ListenPort
	if listenPort <= 0 {
		listenPort = 8084
	}
	adminPort := ep.AdminPort
	if adminPort <= 0 {
		adminPort = 8086
	}
	publicHost := ep.Hostname
	if publicHost == "" {
		publicHost = "direct-only.invalid"
	}

	timeouts := map[string]interface{}{
		"idle":            "300s",
		"reconnect_grace": "120s",
	}
	var limits map[string]interface{}

	if spec, ok := ResolvePresetSpec(ep.Preset); ok {
		if spec.IdleTimeout != "" {
			timeouts["idle"] = spec.IdleTimeout
		}
		if spec.ReconnectGrace != "" {
			timeouts["reconnect_grace"] = spec.ReconnectGrace
		}
		if spec.LongPollTimeout != "" {
			timeouts["long_poll"] = spec.LongPollTimeout
		}
		limits = map[string]interface{}{
			"carrier_batch_bytes":     spec.CarrierBatchBytes,
			"max_pending_per_session": spec.MaxPendingPerSession,
			"max_streams_per_session": spec.MaxStreamsPerSession,
		}
	}

	configData := map[string]interface{}{
		"public_hostname": publicHost,
		"listen":          fmt.Sprintf("127.0.0.1:%d", listenPort),
		"admin_listen":    fmt.Sprintf("127.0.0.1:%d", adminPort),
		"public_upstream": "http://127.0.0.1:9008",
		"token_key_file":  tokenKeyPath,
		"static_routes":   "exact",
		"profiles_file":   profilesPath,
		"timeouts":        timeouts,
	}
	if len(limits) > 0 {
		configData["limits"] = limits
	}
	return json.MarshalIndent(configData, "", "  ")
}

// GenerateTproxyConfigJSON produces config.json for tproxy-server (primary/legacy).
func GenerateTproxyConfigJSON(cfg Config, profilesPath, tokenKeyPath string) ([]byte, error) {
	eps := cfg.GetEndpoints()
	if len(eps) > 0 {
		ep0 := eps[0]
		if ep0.Preset == "" && cfg.NetworkPreset != "" {
			ep0.Preset = InferEndpointPreset(ep0, cfg)
		}
		return GenerateEndpointTproxyConfigJSON(ep0, profilesPath, tokenKeyPath)
	}

	listenPort := cfg.ListenPort
	if listenPort <= 0 || listenPort == DefaultListenPort {
		listenPort = 8084
	}
	adminPort := cfg.AdminPort
	if adminPort <= 0 {
		adminPort = DefaultAdminPort
	}
	publicHost := cfg.PublicHostname
	if publicHost == "" {
		if !cfg.IsWebEnabled() {
			publicHost = "direct-only.invalid"
		} else {
			publicHost = DefaultPublicHostname
		}
	}

	return GenerateEndpointTproxyConfigJSON(WebProxyEndpoint{
		Hostname:   publicHost,
		ListenPort: listenPort,
		AdminPort:  adminPort,
		Preset:     cfg.NetworkPreset,
	}, profilesPath, tokenKeyPath)
}
