package xrayserver

import (
	"strings"
	"testing"
)

func TestXHTTPTuning_ValidationAndPresets(t *testing.T) {
	// 1. Live Low-Latency Preset
	tuning := XHTTPTuning{Preset: "live_low_latency"}
	if err := ValidateAndNormalizeXHTTPTuning(&tuning); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if tuning.UplinkMethod != "GET" {
		t.Errorf("expected UplinkMethod GET, got %s", tuning.UplinkMethod)
	}
	if tuning.MinPostsIntervalMs != "5-10" {
		t.Errorf("expected MinPostsIntervalMs 5-10, got %s", tuning.MinPostsIntervalMs)
	}
	if tuning.EnableXmux {
		t.Errorf("expected EnableXmux false, got true")
	}
	if tuning.StreamPathSuffix != "stream.ts" {
		t.Errorf("expected StreamPathSuffix stream.ts, got %s", tuning.StreamPathSuffix)
	}

	// 2. Balanced Preset
	tuningBal := XHTTPTuning{Preset: "balanced"}
	if err := ValidateAndNormalizeXHTTPTuning(&tuningBal); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if tuningBal.UplinkMethod != "POST" {
		t.Errorf("expected UplinkMethod POST, got %s", tuningBal.UplinkMethod)
	}
	if !tuningBal.EnableXmux {
		t.Errorf("expected EnableXmux true, got false")
	}

	// 3. Custom Valid
	tuningCustom := XHTTPTuning{
		Preset:             "custom",
		UplinkMethod:       "post",
		MinPostsIntervalMs: "15-50",
		MaxBufferedPosts:   50,
		StreamUpServerSecs: "30-90",
		StreamPathSuffix:   "/video/stream.ts?foo=bar#hash",
		EnableXmux:         false,
		XPaddingBytes:      "200-500",
	}
	if err := ValidateAndNormalizeXHTTPTuning(&tuningCustom); err != nil {
		t.Fatalf("unexpected validation error for custom: %v", err)
	}
	if tuningCustom.UplinkMethod != "POST" {
		t.Errorf("expected normalized method POST, got %s", tuningCustom.UplinkMethod)
	}
	// Verify sanitization of stream path suffix
	if strings.Contains(tuningCustom.StreamPathSuffix, "?") || strings.Contains(tuningCustom.StreamPathSuffix, "#") {
		t.Errorf("path suffix should have been sanitized, got %s", tuningCustom.StreamPathSuffix)
	}

	// 4. Custom Invalid Ranges (min > max)
	tuningInvRange := XHTTPTuning{
		Preset:             "custom",
		MinPostsIntervalMs: "50-10",
	}
	if err := ValidateAndNormalizeXHTTPTuning(&tuningInvRange); err == nil {
		t.Errorf("expected error for min > max in MinPostsIntervalMs, got nil")
	}

	// 5. Custom Invalid Buffer
	tuningInvBuf := XHTTPTuning{
		Preset:           "custom",
		MaxBufferedPosts: 2000,
	}
	if err := ValidateAndNormalizeXHTTPTuning(&tuningInvBuf); err == nil {
		t.Errorf("expected error for MaxBufferedPosts > 1000, got nil")
	}
}

func TestBuildShareLinks_WithTuning(t *testing.T) {
	tuning := DefaultLiveLowLatencyTuning()
	cfg := Config{
		Enabled:       true,
		PublicDomain:  "cdn.example.org",
		PublicPort:    443,
		Path:          "/cdn-bridge",
		Transport:     "xhttp",
		Mode:          "packet-up",
		XHTTPTuning:   &tuning,
	}
	client := Client{
		ID:     "test-uuid-9999",
		Remark: "UltraLowLatencyClient",
	}

	links, err := BuildShareLinks(cfg, client)
	if err != nil {
		t.Fatalf("failed to build share links: %v", err)
	}

	// Verify stream.ts appended to path
	if !strings.Contains(links.VlessURL, "path=%2Fcdn-bridge%2Fstream.ts") {
		t.Errorf("expected stream.ts in VlessURL path: %s", links.VlessURL)
	}
	// Verify scMinPostsIntervalMs in extra
	if !strings.Contains(links.VlessURL, "scMinPostsIntervalMs") {
		t.Errorf("expected scMinPostsIntervalMs in VlessURL: %s", links.VlessURL)
	}
	// Verify Happ JSON contains ultra low latency xhttp block
	if !strings.Contains(links.HappJSON, `"scMinPostsIntervalMs": "5-10"`) {
		t.Errorf("expected scMinPostsIntervalMs in HappJSON: %s", links.HappJSON)
	}
	if !strings.Contains(links.HappJSON, `"enableXmux": false`) {
		t.Errorf("expected enableXmux false in HappJSON: %s", links.HappJSON)
	}
	// Verify Singbox JSON contains xhttp transport
	if !strings.Contains(links.SingboxJSON, `"type": "xhttp"`) {
		t.Errorf("expected xhttp in SingboxJSON: %s", links.SingboxJSON)
	}
	if !strings.Contains(links.SingboxJSON, `"uplink_http_method": "GET"`) {
		t.Errorf("expected uplink_http_method GET in SingboxJSON: %s", links.SingboxJSON)
	}
}

func TestRenderRuntimeConfig_WithTuning(t *testing.T) {
	tuning := DefaultLiveLowLatencyTuning()
	cfg := Config{
		Enabled:      true,
		ListenPort:   9008,
		PublicDomain: "cdn.example.org",
		Path:         "/cdn-bridge/",
		Transport:    "xhttp",
		Mode:         "packet-up",
		XHTTPTuning:  &tuning,
		Clients: []Client{
			{ID: "test-uuid-1", Remark: "Phone", Enabled: true},
		},
	}

	raw, err := RenderRuntimeConfig(cfg)
	if err != nil {
		t.Fatalf("RenderRuntimeConfig: %v", err)
	}

	rawStr := string(raw)
	if !strings.Contains(rawStr, `"/cdn-bridge/stream.ts"`) {
		t.Errorf("expected server path /cdn-bridge/stream.ts, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, `"scMinPostsIntervalMs": "5-10"`) {
		t.Errorf("expected scMinPostsIntervalMs 5-10 in server config, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, `"uplinkHTTPMethod": "GET"`) {
		t.Errorf("expected uplinkHTTPMethod GET in server config, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, `"enableXmux": false`) {
		t.Errorf("expected enableXmux false in server config, got: %s", rawStr)
	}
}
