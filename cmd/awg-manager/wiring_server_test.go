package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
)

func TestWiringServer_ShutdownHookPreservesEnabledState(t *testing.T) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	tgDir := filepath.Join(tmpDir, "tproxy")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(tgDir, 0755)

	// Write Xray config with Enabled: true
	xrayCfg := xrayserver.Config{
		Enabled:    true,
		ListenPort: 9008,
	}
	xrayData, _ := json.MarshalIndent(xrayCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(xrayDir, "xray-server-settings.json"), xrayData, 0600); err != nil {
		t.Fatalf("write xray settings: %v", err)
	}

	// Write TG config with Enabled: true
	tgCfg := tgwebproxy.Config{
		Enabled:    true,
		ListenPort: 8085,
	}
	tgData, _ := json.MarshalIndent(tgCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(tgDir, "tgwebproxy-settings.json"), tgData, 0600); err != nil {
		t.Fatalf("write tg settings: %v", err)
	}

	xraySvc := xrayserver.New(tmpDir, nil)
	tgSvc := tgwebproxy.New(tmpDir, nil)

	if !xraySvc.GetConfig().Enabled {
		t.Fatalf("initial xray config must be enabled")
	}
	if !tgSvc.GetConfig().Enabled {
		t.Fatalf("initial tgwebproxy config must be enabled")
	}

	a := &app{
		xrayServerService: xraySvc,
		tgWebProxyService: tgSvc,
	}

	// Call production shutdown helper exactly as wired in wiring_server.go
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdownIngressRuntime(ctx, a.xrayServerService, a.tgWebProxyService, a.cdnDispatcher); err != nil {
		t.Fatalf("shutdownIngressRuntime failed: %v", err)
	}

	// 1. In-memory config must remain enabled (NOT set to false like Stop() would)
	if !xraySvc.GetConfig().Enabled {
		t.Errorf("expected in-memory xray Enabled to remain true after shutdown hook")
	}
	if !tgSvc.GetConfig().Enabled {
		t.Errorf("expected in-memory tgwebproxy Enabled to remain true after shutdown hook")
	}

	// 2. Persistent disk config must remain enabled when reloaded from scratch
	reloadedXray := xrayserver.New(tmpDir, nil)
	if !reloadedXray.GetConfig().Enabled {
		t.Errorf("expected persistent on-disk xray Enabled to remain true across shutdown/reload")
	}

	reloadedTG := tgwebproxy.New(tmpDir, nil)
	if !reloadedTG.GetConfig().Enabled {
		t.Errorf("expected persistent on-disk tgwebproxy Enabled to remain true across shutdown/reload")
	}
}
