package xrayserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestXrayRuntimeConfigByTransport(t *testing.T) {
	t.Run("xhttp transport", func(t *testing.T) {
		cfg := Config{
			Enabled:      true,
			ListenPort:   9008,
			PublicDomain: "cdn.example.com",
			PublicPort:   443,
			Path:         "/cdn-bridge/",
			Transport:    "xhttp",
			Mode:         "packet-up",
			UplinkMethod: "GET",
			Clients: []Client{
				{ID: "test-uuid-1", Remark: "Phone", Enabled: true},
			},
		}

		raw, err := RenderRuntimeConfig(cfg)
		if err != nil {
			t.Fatalf("RenderRuntimeConfig: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		inbounds := parsed["inbounds"].([]interface{})
		inbound := inbounds[0].(map[string]interface{})
		streamSettings := inbound["streamSettings"].(map[string]interface{})

		if streamSettings["network"] != "xhttp" {
			t.Errorf("expected network xhttp, got %v", streamSettings["network"])
		}
		xhttpSettings := streamSettings["xhttpSettings"].(map[string]interface{})
		if xhttpSettings["path"] != "/cdn-bridge/" {
			t.Errorf("expected path /cdn-bridge/, got %v", xhttpSettings["path"])
		}
		if xhttpSettings["mode"] != "packet-up" {
			t.Errorf("expected mode packet-up, got %v", xhttpSettings["mode"])
		}
	})

	t.Run("ws transport", func(t *testing.T) {
		cfg := Config{
			Enabled:      true,
			ListenPort:   9008,
			PublicDomain: "cdn.example.com",
			PublicPort:   443,
			Path:         "/cdn-bridge/",
			Transport:    "ws",
			Clients: []Client{
				{ID: "test-uuid-2", Remark: "Laptop", Enabled: true},
			},
		}

		raw, err := RenderRuntimeConfig(cfg)
		if err != nil {
			t.Fatalf("RenderRuntimeConfig: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		inbounds := parsed["inbounds"].([]interface{})
		inbound := inbounds[0].(map[string]interface{})
		streamSettings := inbound["streamSettings"].(map[string]interface{})

		if streamSettings["network"] != "ws" {
			t.Errorf("expected network ws, got %v", streamSettings["network"])
		}
		wsSettings := streamSettings["wsSettings"].(map[string]interface{})
		if wsSettings["path"] != "/cdn-bridge/" {
			t.Errorf("expected path /cdn-bridge/, got %v", wsSettings["path"])
		}
		headers := wsSettings["headers"].(map[string]interface{})
		if headers["Host"] != "cdn.example.com" {
			t.Errorf("expected Host header cdn.example.com, got %v", headers["Host"])
		}
	})
}

func TestXrayShareLinksByTransport(t *testing.T) {
	t.Run("xhttp links", func(t *testing.T) {
		s := New(t.TempDir(), nil)
		s.config = Config{
			PublicDomain: "cdn.example.com",
			PublicPort:   443,
			Path:         "/cdn-bridge/",
			Transport:    "xhttp",
			Mode:         "packet-up",
			UplinkMethod: "GET",
			Clients: []Client{
				{ID: "uuid-1234", Remark: "HappPhone", Enabled: true},
			},
		}

		links, err := s.GenerateLinks("uuid-1234")
		if err != nil {
			t.Fatalf("GenerateLinks: %v", err)
		}

		if !strings.Contains(links.VlessURL, "type=xhttp") {
			t.Errorf("VlessURL missing type=xhttp: %s", links.VlessURL)
		}
		if !strings.Contains(links.HappJSON, `"network": "xhttp"`) {
			t.Errorf("HappJSON missing xhttp network: %s", links.HappJSON)
		}
		if !strings.Contains(links.SingboxJSON, `"type": "xhttp"`) {
			t.Errorf("SingboxJSON missing xhttp type: %s", links.SingboxJSON)
		}
		if !strings.Contains(links.MihomoYAML, "network: xhttp") {
			t.Errorf("MihomoYAML missing network: xhttp: %s", links.MihomoYAML)
		}
	})

	t.Run("ws links", func(t *testing.T) {
		s := New(t.TempDir(), nil)
		s.config = Config{
			PublicDomain: "cdn.example.com",
			PublicPort:   443,
			Path:         "/cdn-bridge/",
			Transport:    "ws",
			Clients: []Client{
				{ID: "uuid-5678", Remark: "WSDacha", Enabled: true},
			},
		}

		links, err := s.GenerateLinks("uuid-5678")
		if err != nil {
			t.Fatalf("GenerateLinks: %v", err)
		}

		if !strings.Contains(links.VlessURL, "type=ws") {
			t.Errorf("VlessURL missing type=ws: %s", links.VlessURL)
		}
		if !strings.Contains(links.HappJSON, `"network": "ws"`) {
			t.Errorf("HappJSON missing ws network: %s", links.HappJSON)
		}
		if !strings.Contains(links.SingboxJSON, `"type": "ws"`) {
			t.Errorf("SingboxJSON missing ws type: %s", links.SingboxJSON)
		}
		if !strings.Contains(links.MihomoYAML, "network: ws") {
			t.Errorf("MihomoYAML missing network: ws: %s", links.MihomoYAML)
		}
	})
}

func TestXrayOutboundMaterialization(t *testing.T) {
	t.Run("direct outbound", func(t *testing.T) {
		cfg := Config{
			Enabled:      true,
			OutboundMode: "direct",
		}
		raw, err := RenderRuntimeConfig(cfg)
		if err != nil {
			t.Fatalf("RenderRuntimeConfig: %v", err)
		}
		var parsed map[string]interface{}
		_ = json.Unmarshal(raw, &parsed)
		outbounds := parsed["outbounds"].([]interface{})
		if len(outbounds) != 1 {
			t.Fatalf("expected 1 outbound, got %d", len(outbounds))
		}
		out := outbounds[0].(map[string]interface{})
		if out["protocol"] != "freedom" {
			t.Errorf("expected freedom protocol, got %v", out["protocol"])
		}
	})

	t.Run("socks outbound", func(t *testing.T) {
		cfg := Config{
			Enabled:           true,
			OutboundMode:      "socks",
			OutboundSocksPort: 1099,
		}
		raw, err := RenderRuntimeConfig(cfg)
		if err != nil {
			t.Fatalf("RenderRuntimeConfig: %v", err)
		}
		var parsed map[string]interface{}
		_ = json.Unmarshal(raw, &parsed)
		outbounds := parsed["outbounds"].([]interface{})
		if len(outbounds) != 2 {
			t.Fatalf("expected 2 outbounds (socks + freedom fallback), got %d", len(outbounds))
		}
		socksOut := outbounds[0].(map[string]interface{})
		if socksOut["protocol"] != "socks" {
			t.Errorf("expected socks protocol, got %v", socksOut["protocol"])
		}
	})

	t.Run("interface outbound", func(t *testing.T) {
		cfg := Config{
			Enabled:           true,
			OutboundMode:      "interface",
			OutboundInterface: "nwg1",
		}
		raw, err := RenderRuntimeConfig(cfg)
		if err != nil {
			t.Fatalf("RenderRuntimeConfig: %v", err)
		}
		var parsed map[string]interface{}
		_ = json.Unmarshal(raw, &parsed)
		outbounds := parsed["outbounds"].([]interface{})
		if len(outbounds) != 1 {
			t.Fatalf("expected 1 outbound, got %d", len(outbounds))
		}
		ifaceOut := outbounds[0].(map[string]interface{})
		if ifaceOut["protocol"] != "freedom" {
			t.Errorf("expected freedom protocol, got %v", ifaceOut["protocol"])
		}

		// Strictly verify streamSettings.sockopt.interface == "nwg1" per implementation note #1
		streamSettings, ok := ifaceOut["streamSettings"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing streamSettings in interface outbound")
		}
		sockopt, ok := streamSettings["sockopt"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing sockopt in streamSettings")
		}
		if sockopt["interface"] != "nwg1" {
			t.Errorf("expected sockopt.interface to be 'nwg1', got %v", sockopt["interface"])
		}
	})
}
