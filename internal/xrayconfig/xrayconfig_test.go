package xrayconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentLosslessRoundTrip(t *testing.T) {
	rawJSON := `{
		"log": {"loglevel": "warning"},
		"inbounds": [{"tag": "vless-in", "port": 9008, "protocol": "vless"}],
		"customEnterpriseField": {"policy": "strict", "version": 42},
		"anotherUnknown": "custom-value"
	}`

	var doc Document
	if err := json.Unmarshal([]byte(rawJSON), &doc); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(doc.Extra) != 2 {
		t.Fatalf("expected 2 extra fields, got %d", len(doc.Extra))
	}

	marshaled, err := json.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	resStr := string(marshaled)
	if !strings.Contains(resStr, "customEnterpriseField") {
		t.Errorf("lost customEnterpriseField in roundtrip: %s", resStr)
	}
	if !strings.Contains(resStr, "anotherUnknown") {
		t.Errorf("lost anotherUnknown in roundtrip: %s", resStr)
	}
}

func TestCompiler_CompileWithBase(t *testing.T) {
	baseJSON := `{
		"dns": {"servers": ["8.8.8.8", "1.1.1.1"]},
		"observatory": {"probeInterval": "10m"},
		"reverse": {"port": 1234},
		"fakedns": [{"ipPool": "198.18.0.0/15"}],
		"customHeader": "keep-this-val"
	}`

	var base Document
	if err := json.Unmarshal([]byte(baseJSON), &base); err != nil {
		t.Fatalf("unmarshal base doc error: %v", err)
	}

	cfg := &ManagedConfig{
		LogLevel: "debug",
		Inbounds: []Inbound{
			{Tag: "in-1", Port: 8080, Protocol: "vless", Clients: []Client{
				{UUID: "27c69d32-f887-479b-84aa-cb997437844e", Enabled: true},
			}},
		},
		Outbounds: []Outbound{
			{Tag: "out-direct", Protocol: "freedom"},
		},
		Extra: json.RawMessage(`{"mergedManagedKey": 999}`),
	}

	compiler := NewCompiler()
	compiled, err := compiler.CompileWithBase(&base, cfg)
	if err != nil {
		t.Fatalf("compile with base error: %v", err)
	}

	marshaled, err := json.Marshal(compiled)
	if err != nil {
		t.Fatalf("marshal compiled doc error: %v", err)
	}

	str := string(marshaled)
	if !strings.Contains(str, "probeInterval") {
		t.Errorf("expected base observatory to be preserved: %s", str)
	}
	if !strings.Contains(str, "198.18.0.0/15") {
		t.Errorf("expected base fakedns to be preserved: %s", str)
	}
	if !strings.Contains(str, "customHeader") {
		t.Errorf("expected base extra customHeader to be preserved: %s", str)
	}
	if !strings.Contains(str, "mergedManagedKey") {
		t.Errorf("expected cfg.Extra to be merged: %s", str)
	}
	if !strings.Contains(str, "debug") {
		t.Errorf("expected updated loglevel debug: %s", str)
	}
}

func TestOutboundCompilation_AllProtocols(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		outbound      Outbound
		expectedJSON  []string
		unexpectedStr []string
	}{
		{
			name: "vless with reality",
			outbound: Outbound{
				Tag:       "vless-out",
				Protocol:  "vless",
				Server:    "vless.example.com",
				Port:      443,
				UUID:      "27c69d32-f887-479b-84aa-cb997437844e",
				Transport: "xhttp",
				Security:  "reality",
				Path:      "/xhttp-path",
				Host:      "vless.example.com",
				Reality: &RealityConfig{
					Show:        false,
					PublicKey:   "pubkey1234567890",
					ServerNames: []string{"vless.example.com"},
					ShortIDs:    []string{"1234abcd"},
					Fingerprint: "chrome",
				},
			},
			expectedJSON: []string{
				`"vless"`,
				`"vnext"`,
				`"address":"vless.example.com"`,
				`"port":443`,
				`"27c69d32-f887-479b-84aa-cb997437844e"`,
				`"streamSettings"`,
				`"network":"xhttp"`,
				`"security":"reality"`,
				`"realitySettings"`,
				`"publicKey":"pubkey1234567890"`,
				`"shortId":"1234abcd"`,
			},
		},
		{
			name: "vmess with websocket and tls",
			outbound: Outbound{
				Tag:       "vmess-out",
				Protocol:  "vmess",
				Server:    "vmess.example.com",
				Port:      8443,
				UUID:      "ca293bc6-771b-4443-9f0e-ca22fa600f8b",
				Transport: "ws",
				Security:  "tls",
				Path:      "/ws-path",
				Host:      "vmess.example.com",
				TLS: &TLSConfig{
					ServerName: "vmess.example.com",
					ALPN:       []string{"h2", "http/1.1"},
				},
			},
			expectedJSON: []string{
				`"vmess"`,
				`"vnext"`,
				`"security":"auto"`,
				`"ca293bc6-771b-4443-9f0e-ca22fa600f8b"`,
				`"streamSettings"`,
				`"network":"ws"`,
				`"security":"tls"`,
				`"wsSettings"`,
				`"path":"/ws-path"`,
				`"tlsSettings"`,
				`"serverName":"vmess.example.com"`,
			},
		},
		{
			name: "trojan with tcp and tls",
			outbound: Outbound{
				Tag:       "trojan-out",
				Protocol:  "trojan",
				Server:    "trojan.example.com",
				Port:      443,
				Password:  "trojan-pass-secret",
				Transport: "tcp",
				Security:  "tls",
				TLS: &TLSConfig{
					ServerName: "trojan.example.com",
				},
			},
			expectedJSON: []string{
				`"trojan"`,
				`"servers"`,
				`"trojan-pass-secret"`,
				`"security":"tls"`,
			},
		},
		{
			name: "shadowsocks",
			outbound: Outbound{
				Tag:      "ss-out",
				Protocol: "shadowsocks",
				Server:   "ss.example.com",
				Port:     8388,
				Password: "ss-secret-password",
			},
			expectedJSON: []string{
				`"shadowsocks"`,
				`"servers"`,
				`"ss-secret-password"`,
				`"2022-blake3-aes-128-gcm"`,
			},
		},
		{
			name: "socks",
			outbound: Outbound{
				Tag:      "socks-out",
				Protocol: "socks",
				Server:   "127.0.0.1",
				Port:     1080,
			},
			expectedJSON: []string{
				`"socks"`,
				`"servers"`,
				`"127.0.0.1"`,
				`"port":1080`,
			},
		},
		{
			name: "http",
			outbound: Outbound{
				Tag:      "http-out",
				Protocol: "http",
				Server:   "proxy.corp",
				Port:     3128,
			},
			expectedJSON: []string{
				`"http"`,
				`"servers"`,
				`"proxy.corp"`,
				`"port":3128`,
			},
		},
		{
			name: "freedom direct",
			outbound: Outbound{
				Tag:      "direct",
				Protocol: "freedom",
			},
			expectedJSON: []string{
				`"tag":"direct"`,
				`"protocol":"freedom"`,
			},
			unexpectedStr: []string{`"streamSettings"`},
		},
		{
			name: "blackhole block",
			outbound: Outbound{
				Tag:      "block",
				Protocol: "blackhole",
			},
			expectedJSON: []string{
				`"tag":"block"`,
				`"protocol":"blackhole"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawMsg, err := compiler.compileOutbound(tt.outbound)
			if err != nil {
				t.Fatalf("compileOutbound failed: %v", err)
			}
			s := string(rawMsg)
			for _, exp := range tt.expectedJSON {
				if !strings.Contains(s, exp) {
					t.Errorf("missing expected substring %q in compiled outbound:\n%s", exp, s)
				}
			}
			for _, unexp := range tt.unexpectedStr {
				if strings.Contains(s, unexp) {
					t.Errorf("found unexpected substring %q in compiled outbound:\n%s", unexp, s)
				}
			}
		})
	}
}

func TestOutboundCompilation_Unsupported(t *testing.T) {
	compiler := NewCompiler()
	out := Outbound{
		Tag:      "bad-out",
		Protocol: "unsupported-proto-xyz",
	}
	_, err := compiler.compileOutbound(out)
	if err == nil {
		t.Fatal("expected error for unsupported outbound protocol without raw_settings, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported outbound protocol") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRawSettings_StrictValidation(t *testing.T) {
	compiler := NewCompiler()

	// 1. Invalid JSON in inbound
	inBadJSON := Inbound{
		Tag:         "in-bad",
		Protocol:    "socks",
		Port:        1080,
		RawSettings: json.RawMessage(`{not a valid json}`),
	}
	_, err := compiler.compileInbound(inBadJSON, false)
	if err == nil {
		t.Fatal("expected error for invalid inbound RawSettings, got nil")
	}
	if !strings.Contains(err.Error(), "invalid raw_settings for inbound") {
		t.Errorf("unexpected error message: %v", err)
	}

	// 2. Non-object JSON in inbound (e.g. array)
	inArrayJSON := Inbound{
		Tag:         "in-array",
		Protocol:    "socks",
		Port:        1080,
		RawSettings: json.RawMessage(`[1, 2, 3]`),
	}
	_, err = compiler.compileInbound(inArrayJSON, false)
	if err == nil {
		t.Fatal("expected error for non-object inbound RawSettings, got nil")
	}

	// 3. Invalid JSON in outbound
	outBadJSON := Outbound{
		Tag:         "out-bad",
		Protocol:    "freedom",
		RawSettings: json.RawMessage(`{"incomplete`),
	}
	_, err = compiler.compileOutbound(outBadJSON)
	if err == nil {
		t.Fatal("expected error for invalid outbound RawSettings, got nil")
	}
	if !strings.Contains(err.Error(), "invalid raw_settings for outbound") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidator_PortOverlapMatrix(t *testing.T) {
	tests := []struct {
		addr1    string
		port1    int
		addr2    string
		port2    int
		overlaps bool
	}{
		// Same port, wildcards
		{"0.0.0.0", 443, "192.168.90.1", 443, true},
		{"192.168.90.1", 443, "0.0.0.0", 443, true},
		{"", 443, "127.0.0.1", 443, true},
		{"::", 443, "0.0.0.0", 443, true},
		{"[::]", 443, "192.168.90.1", 443, true},
		// Loopback normalization
		{"localhost", 8080, "127.0.0.1", 8080, true},
		{"127.0.0.1", 8080, "localhost", 8080, true},
		// Identical addresses
		{"192.168.90.1", 8080, "192.168.90.1", 8080, true},
		// Different addresses (no overlap)
		{"127.0.0.1", 8080, "192.168.90.1", 8080, false},
		{"192.168.1.1", 8080, "192.168.90.1", 8080, false},
		// Different ports (never overlap)
		{"0.0.0.0", 80, "0.0.0.0", 443, false},
		{"127.0.0.1", 8080, "127.0.0.1", 9090, false},
		{"0.0.0.0", 8080, "192.168.90.1", 9090, false},
	}

	for _, tt := range tests {
		got := AddressesOverlap(tt.addr1, tt.port1, tt.addr2, tt.port2)
		if got != tt.overlaps {
			t.Errorf("AddressesOverlap(%q, %d, %q, %d) = %v; expected %v",
				tt.addr1, tt.port1, tt.addr2, tt.port2, got, tt.overlaps)
		}
	}
}

func TestValidator_CompatibilityMatrixAndFormats(t *testing.T) {
	validator := NewValidator()

	// 1. Invalid UUID format in client
	cfgBadUUID := &ManagedConfig{
		Inbounds: []Inbound{
			{
				Tag:      "vless-in",
				Port:     443,
				Protocol: "vless",
				Clients: []Client{
					{UUID: "not-a-valid-uuid", Enabled: true},
				},
			},
		},
	}
	errs := validator.Validate(cfgBadUUID)
	foundUUIDErr := false
	for _, e := range errs {
		if strings.Contains(e.Message, "недопустимый формат UUID") {
			foundUUIDErr = true
		}
	}
	if !foundUUIDErr {
		t.Errorf("expected UUID format error, got: %v", errs)
	}

	// 2. Server TLS without certificates
	cfgTLSNoCerts := &ManagedConfig{
		Inbounds: []Inbound{
			{
				Tag:      "tls-in",
				Port:     443,
				Protocol: "trojan",
				Security: "tls",
				TLS:      &TLSConfig{}, // no certificates
			},
		},
	}
	errs = validator.Validate(cfgTLSNoCerts)
	foundCertErr := false
	for _, e := range errs {
		if strings.Contains(e.Message, "требуется как минимум один сертификат") {
			foundCertErr = true
		}
	}
	if !foundCertErr {
		t.Errorf("expected TLS certs error, got: %v", errs)
	}

	// 3. Both OutboundTag and BalancerTag specified
	cfgBothTags := &ManagedConfig{
		Inbounds: []Inbound{
			{Tag: "in-1", Port: 8080, Protocol: "socks"},
		},
		Outbounds: []Outbound{
			{Tag: "out-1", Protocol: "freedom"},
		},
		RoutingRules: []RoutingRule{
			{
				Type:        "field",
				OutboundTag: "out-1",
				BalancerTag: "balancer-1",
			},
		},
	}
	errs = validator.Validate(cfgBothTags)
	foundMutualExcl := false
	for _, e := range errs {
		if strings.Contains(e.Message, "не может одновременно указывать outbound_tag и balancer_tag") {
			foundMutualExcl = true
		}
	}
	if !foundMutualExcl {
		t.Errorf("expected mutual exclusion error for routing tags, got: %v", errs)
	}

	// 4. REALITY on unsupported protocol
	cfgBadReality := &ManagedConfig{
		Inbounds: []Inbound{
			{
				Tag:      "socks-reality",
				Port:     1080,
				Protocol: "socks",
				Security: "reality",
				Reality: &RealityConfig{
					Target:      "ya.ru:443",
					ServerNames: []string{"ya.ru"},
					PrivateKey:  "secret-key",
				},
			},
		},
	}
	errs = validator.Validate(cfgBadReality)
	foundProtoErr := false
	for _, e := range errs {
		if strings.Contains(e.Message, "REALITY поддерживается только для протоколов vless и trojan") {
			foundProtoErr = true
		}
	}
	if !foundProtoErr {
		t.Errorf("expected protocol incompatibility error for reality, got: %v", errs)
	}
}

func TestRedactor_DeepAndRawSanitization(t *testing.T) {
	redactor := NewRedactor()

	cfg := &ManagedConfig{
		LogLevel: "debug",
		Extra: json.RawMessage(`{
			"nested": {
				"apiKey": "super-api-secret-12345",
				"token": "token-xyz-abc",
				"safeField": "public-val"
			}
		}`),
		Inbounds: []Inbound{
			{
				Tag:      "vless-in",
				Port:     9008,
				Protocol: "vless",
				Clients: []Client{
					{UUID: "27c69d32-f887-479b-84aa-cb997437844e", Remark: "Phone", Enabled: true},
				},
				Reality: &RealityConfig{
					PrivateKey: "super_secret_private_key_value",
				},
				RawSettings: json.RawMessage(`{
					"deep": {
						"password": "inbound-secret-password",
						"psk": "pre-shared-key-value",
						"authorization": "Bearer secret-token"
					}
				}`),
			},
		},
		Outbounds: []Outbound{
			{
				Tag:      "out-trojan",
				Protocol: "trojan",
				Password: "outbound-trojan-password",
				RawSettings: json.RawMessage(`{
					"servers": [
						{"password": "nested-server-password"}
					]
				}`),
			},
		},
	}

	redacted := redactor.RedactConfig(cfg)

	// Check Extra
	extraStr := string(redacted.Extra)
	if strings.Contains(extraStr, "super-api-secret-12345") {
		t.Errorf("Extra leaked secret key: %s", extraStr)
	}
	if strings.Contains(extraStr, "token-xyz-abc") {
		t.Errorf("Extra leaked token: %s", extraStr)
	}
	if !strings.Contains(extraStr, "public-val") {
		t.Errorf("Extra lost public value: %s", extraStr)
	}

	// Check Inbound Reality PrivateKey
	if redacted.Inbounds[0].Reality.PrivateKey != "[REDACTED]" {
		t.Errorf("PrivateKey not redacted: %s", redacted.Inbounds[0].Reality.PrivateKey)
	}

	// Check Inbound Client UUID
	if !strings.Contains(redacted.Inbounds[0].Clients[0].UUID, "••••••••") {
		t.Errorf("Client UUID not masked: %s", redacted.Inbounds[0].Clients[0].UUID)
	}

	// Check Inbound RawSettings
	inRawStr := string(redacted.Inbounds[0].RawSettings)
	if strings.Contains(inRawStr, "inbound-secret-password") || strings.Contains(inRawStr, "pre-shared-key-value") || strings.Contains(inRawStr, "secret-token") {
		t.Errorf("Inbound RawSettings leaked secret: %s", inRawStr)
	}

	// Check Outbound Password
	if redacted.Outbounds[0].Password != "[REDACTED]" {
		t.Errorf("Outbound password not redacted: %s", redacted.Outbounds[0].Password)
	}

	// Check Outbound RawSettings
	outRawStr := string(redacted.Outbounds[0].RawSettings)
	if strings.Contains(outRawStr, "nested-server-password") {
		t.Errorf("Outbound RawSettings leaked nested password: %s", outRawStr)
	}
}

func TestDiff_FullFieldCoverageAndSecretSafety(t *testing.T) {
	baseCfg := &ManagedConfig{
		LogLevel:     "warning",
		StatsEnabled: false,
		Inbounds: []Inbound{
			{
				Tag:       "in-1",
				Listen:    "127.0.0.1",
				Port:      8080,
				Protocol:  "vless",
				Transport: "tcp",
				Security:  "none",
				Path:      "/old-path",
				Host:      "old.host.com",
				Clients: []Client{
					{
						ID:      "c-1",
						UUID:    "27c69d32-f887-479b-84aa-cb997437844e",
						Remark:  "Old Remark",
						Email:   "old@awgm",
						Flow:    "none",
						Level:   0,
						Enabled: true,
					},
				},
				RawSettings: json.RawMessage(`{"k": "v1"}`),
			},
		},
		Outbounds: []Outbound{
			{
				Tag:         "out-1",
				Protocol:    "trojan",
				Server:      "trojan.old.com",
				Port:        443,
				Password:    "secret-trojan-password-123",
				Transport:   "tcp",
				Security:    "tls",
				RawSettings: json.RawMessage(`{"v": 1}`),
			},
		},
		RoutingRules: []RoutingRule{
			{
				Type:        "field",
				InboundTag:  []string{"in-1"},
				OutboundTag: "out-1",
			},
		},
	}

	// Mutate every single supported field
	mutatedCfg := &ManagedConfig{
		LogLevel:     "debug", // changed
		StatsEnabled: true,    // changed
		Inbounds: []Inbound{
			{
				Tag:            "in-1",
				Listen:         "0.0.0.0", // changed
				Port:           8443,      // changed
				Protocol:       "trojan",  // changed
				Transport:      "ws",      // changed
				Security:       "tls",     // changed
				Path:           "/new-path", // changed
				Host:           "new.host.com", // changed
				UpstreamDevice: "wg0",     // changed
				Sniffing:       &SniffingConfig{Enabled: true}, // changed
				TLS:            &TLSConfig{ServerName: "new.host.com"}, // changed
				Clients: []Client{
					{
						ID:      "c-1",
						UUID:    "ca293bc6-771b-4443-9f0e-ca22fa600f8b", // changed
						Remark:  "New Remark",                           // changed
						Email:   "new@awgm",                             // changed
						Flow:    "xtls-rprx-vision",                     // changed
						Level:   1,                                      // changed
						Enabled: false,                                  // changed
					},
				},
				RawSettings: json.RawMessage(`{"k": "v2"}`), // changed
			},
		},
		Outbounds: []Outbound{
			{
				Tag:         "out-1",
				Protocol:    "vless",                  // changed
				Server:      "vless.new.com",          // changed
				Port:        8443,                     // changed
				UUID:        "33333333-3333-3333-3333-333333333333", // changed
				Password:    "new-secret-trojan-password-456",       // changed
				Transport:   "xhttp",                  // changed
				Security:    "reality",                // changed
				Path:        "/new-xhttp",             // changed
				Host:        "xhttp.new.com",          // changed
				SendThrough: "192.168.90.1",           // changed
				Reality:     &RealityConfig{Target: "dest.com:443"}, // changed
				RawSettings: json.RawMessage(`{"v": 2}`), // changed
			},
		},
		RoutingRules: []RoutingRule{
			{
				Type:        "field",
				InboundTag:  []string{"in-1"},
				OutboundTag: "out-1",
				Domain:      []string{"example.com"}, // changed
			},
		},
	}

	diff := CalculateDiff(baseCfg, mutatedCfg)

	if !diff.RestartRequired {
		t.Fatal("expected RestartRequired to be true")
	}

	if len(diff.Items) < 15 {
		t.Fatalf("expected at least 15 diff items for exhaustive mutation, got %d", len(diff.Items))
	}

	// Verify no secret leak in descriptions
	diffBytes, _ := json.Marshal(diff)
	diffStr := string(diffBytes)

	if strings.Contains(diffStr, "secret-trojan-password") || strings.Contains(diffStr, "new-secret-trojan-password") {
		t.Errorf("diff leaked password in text: %s", diffStr)
	}

	// Verify UUIDs in diff are masked
	if strings.Contains(diffStr, "27c69d32-f887-479b-84aa-cb997437844e") {
		t.Errorf("diff leaked unmasked client UUID: %s", diffStr)
	}
	if strings.Contains(diffStr, "33333333-3333-3333-3333-333333333333") {
		t.Errorf("diff leaked unmasked outbound UUID: %s", diffStr)
	}
}

func TestJSONEqual_LargeNumbersAndFormatting(t *testing.T) {
	// 64-bit integer that loses precision if parsed as float64
	json1 := []byte(`{"id": 9223372036854775807, "name": "test", "active": true}`)
	json2 := []byte(`{"name": "test", "active": true, "id": 9223372036854775807}`)
	jsonDiffNum := []byte(`{"id": 9223372036854775806, "name": "test", "active": true}`)

	if !JSONEqual(json1, json2) {
		t.Errorf("expected json1 and json2 to be semantically equal despite key order")
	}

	if JSONEqual(json1, jsonDiffNum) {
		t.Errorf("expected json1 and jsonDiffNum to NOT be equal (64-bit integer precision)")
	}

	// Whitespace variations
	jsonWhitespace := []byte(`{
		"id": 9223372036854775807,
		"name": "test",
		"active": true
	}`)
	if !JSONEqual(json1, jsonWhitespace) {
		t.Errorf("expected json1 and jsonWhitespace to be semantically equal")
	}
}

func TestParser_LosslessRoundTripAndOverlay(t *testing.T) {
	rawDoc := `{
		"log": {"loglevel": "info"},
		"inbounds": [
			{
				"tag": "vless-in",
				"listen": "0.0.0.0",
				"port": 443,
				"protocol": "vless",
				"settings": {
					"clients": [{"id": "27c69d32-f887-479b-84aa-cb997437844e", "email": "user@awgm", "level": 0}],
					"decryption": "none"
				},
				"streamSettings": {
					"network": "tcp",
					"security": "reality",
					"realitySettings": {
						"dest": "example.com:443",
						"serverNames": ["example.com"],
						"privateKey": "privkey123",
						"shortIds": ["01234567"]
					},
					"sockopt": {"mark": 255, "tcpFastOpen": true}
				},
				"sniffing": {
					"enabled": true,
					"destOverride": ["http", "tls"]
				}
			}
		],
		"outbounds": [
			{
				"tag": "direct",
				"protocol": "freedom",
				"settings": {},
				"mux": {"enabled": true, "concurrency": 8}
			}
		],
		"routing": {
			"domainStrategy": "AsIs",
			"rules": [
				{"type": "field", "inboundTag": ["vless-in"], "outboundTag": "direct"}
			],
			"balancers": [
				{
					"tag": "balancer-main",
					"selector": ["direct"],
					"strategy": {"type": "leastPing"}
				}
			]
		},
		"stats": {}
	}`

	parser := NewParser()
	doc, err := parser.ParseDocument([]byte(rawDoc))
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}

	cfg, err := parser.DocumentToManaged(doc)
	if err != nil {
		t.Fatalf("DocumentToManaged failed: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected loglevel 'info', got %q", cfg.LogLevel)
	}
	if !cfg.StatsEnabled {
		t.Errorf("expected StatsEnabled true")
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Tag != "vless-in" {
		t.Fatalf("expected 1 inbound 'vless-in', got %v", cfg.Inbounds)
	}
	if len(cfg.Inbounds[0].Clients) != 1 || cfg.Inbounds[0].Clients[0].UUID != "27c69d32-f887-479b-84aa-cb997437844e" {
		t.Fatalf("expected client UUID parsed, got %v", cfg.Inbounds[0].Clients)
	}
	if len(cfg.Balancers) != 1 || cfg.Balancers[0].Tag != "balancer-main" {
		t.Fatalf("expected 1 balancer, got %v", cfg.Balancers)
	}

	// Now recompile and verify preservation of sockopt and mux via raw overlay
	compiler := NewCompiler()
	compiledDoc, err := compiler.CompileWithBase(nil, cfg)
	if err != nil {
		t.Fatalf("CompileWithBase failed: %v", err)
	}

	compiledJSON, err := json.Marshal(compiledDoc)
	if err != nil {
		t.Fatalf("json.Marshal compiled doc failed: %v", err)
	}

	str := string(compiledJSON)
	if !strings.Contains(str, `"sockopt"`) || !strings.Contains(str, `"tcpFastOpen":true`) {
		t.Errorf("expected inbound sockopt overlay preserved in compiled output:\n%s", str)
	}
	if !strings.Contains(str, `"mux"`) || !strings.Contains(str, `"concurrency":8`) {
		t.Errorf("expected outbound mux overlay preserved in compiled output:\n%s", str)
	}
	if !strings.Contains(str, `"balancer-main"`) {
		t.Errorf("expected balancer-main in compiled output:\n%s", str)
	}
}

func TestValidator_NegativeOutboundProtocols(t *testing.T) {
	validator := NewValidator()

	// 1. Missing server and port
	cfgMissingServer := &ManagedConfig{
		Outbounds: []Outbound{
			{Tag: "vless-out", Protocol: "vless", UUID: "27c69d32-f887-479b-84aa-cb997437844e"},
		},
	}
	errs := validator.Validate(cfgMissingServer)
	if len(errs) == 0 {
		t.Fatal("expected validation error for missing server and port")
	}

	// 2. Shadowsocks 2022 key length checks
	cfgSSBadKey := &ManagedConfig{
		Outbounds: []Outbound{
			{
				Tag:      "ss-out",
				Protocol: "shadowsocks",
				Server:   "ss.example.com",
				Port:     8388,
				Method:   "2022-blake3-aes-128-gcm",
				Password: "bm90LXNpeHRlZW4tYnl0ZXM=", // "not-sixteen-bytes" -> 17 bytes base64
			},
		},
	}
	errs = validator.Validate(cfgSSBadKey)
	foundKeyErr := false
	for _, e := range errs {
		if strings.Contains(e.Message, "требуется ключ base64 длиной ровно 16 байт") {
			foundKeyErr = true
		}
	}
	if !foundKeyErr {
		t.Errorf("expected SS-2022 key length error, got: %v", errs)
	}

	// 3. Valid SS-2022 16 byte key
	// 16 bytes: "1234567890123456" -> MTIzNDU2Nzg5MDEyMzQ1Ng==
	cfgSSValid := &ManagedConfig{
		Outbounds: []Outbound{
			{
				Tag:      "ss-out",
				Protocol: "shadowsocks",
				Server:   "ss.example.com",
				Port:     8388,
				Method:   "2022-blake3-aes-128-gcm",
				Password: "MTIzNDU2Nzg5MDEyMzQ1Ng==",
			},
		},
	}
	errs = validator.Validate(cfgSSValid)
	if len(errs) != 0 {
		t.Errorf("expected valid SS config to have 0 errors, got: %v", errs)
	}
}

func TestValidator_ClientUniquenessAcrossInbounds(t *testing.T) {
	validator := NewValidator()

	cfgDupUUID := &ManagedConfig{
		Inbounds: []Inbound{
			{
				Tag:      "in-1",
				Port:     443,
				Protocol: "vless",
				Clients: []Client{
					{UUID: "27c69d32-f887-479b-84aa-cb997437844e", Email: "user1@awgm", Enabled: true},
				},
			},
			{
				Tag:      "in-2",
				Port:     8443,
				Protocol: "vless",
				Clients: []Client{
					{UUID: "27c69d32-f887-479b-84aa-cb997437844e", Email: "user2@awgm", Enabled: true}, // duplicate UUID
				},
			},
		},
	}

	errs := validator.Validate(cfgDupUUID)
	foundDupUUID := false
	for _, e := range errs {
		if strings.Contains(e.Message, "дубликат UUID клиента") {
			foundDupUUID = true
		}
	}
	if !foundDupUUID {
		t.Errorf("expected duplicate UUID error across inbounds, got: %v", errs)
	}
}

func TestValidator_BalancersAndRouting(t *testing.T) {
	validator := NewValidator()

	cfg := &ManagedConfig{
		Inbounds: []Inbound{
			{Tag: "in-1", Port: 1080, Protocol: "socks"},
		},
		Outbounds: []Outbound{
			{Tag: "out-1", Protocol: "freedom"},
		},
		Balancers: []Balancer{
			{Tag: "balancer-1", Selector: []string{"out-1"}},
		},
		RoutingRules: []RoutingRule{
			{
				Type:        "field",
				InboundTag:  []string{"in-1"},
				BalancerTag: "non-existent-balancer", // invalid reference!
			},
		},
	}

	errs := validator.Validate(cfg)
	foundBalancerRefErr := false
	for _, e := range errs {
		if strings.Contains(e.Message, "ссылка на несуществующий balancer") {
			foundBalancerRefErr = true
		}
	}
	if !foundBalancerRefErr {
		t.Errorf("expected error for non-existent balancer reference, got: %v", errs)
	}
}

func TestMigrations_SchemaVersion(t *testing.T) {
	cfg := &ManagedConfig{
		LogLevel: "info",
	}

	// 1. Same version
	migrated, err := MigrateConfig(cfg, 1, 1)
	if err != nil || migrated == nil {
		t.Fatalf("expected migrate 1->1 to succeed: %v", err)
	}

	// 2. Future fromVersion rejected
	_, err = MigrateConfig(cfg, 999, 1)
	if err == nil {
		t.Fatal("expected error for future schema version")
	}

	// 3. Downgrade rejected
	_, err = MigrateConfig(cfg, 2, 1)
	if err == nil {
		t.Fatal("expected error for schema downgrade")
	}
}

