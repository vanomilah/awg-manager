package serveringress

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/cdndispatcher"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/vlessserver"
	"github.com/hoaxisr/awg-manager/internal/xhttpserver"
)

func TestConfigWatcher_HotReloadOnDiskChange(t *testing.T) {
	tempDir := t.TempDir()

	xrayDir := filepath.Join(tempDir, "xray")
	tproxyDir := filepath.Join(tempDir, "tproxy")
	dispDir := filepath.Join(tempDir, "dispatcher")

	_ = os.MkdirAll(xrayDir, 0700)
	_ = os.MkdirAll(tproxyDir, 0700)
	_ = os.MkdirAll(dispDir, 0700)

	xrayFile := filepath.Join(xrayDir, "xray-server-settings.json")
	tgFile := filepath.Join(tproxyDir, "tgwebproxy-settings.json")
	dispFile := filepath.Join(dispDir, "config.json")

	initialXray := vlessserver.Config{
		Enabled:        true,
		Path:           "/cdn-bridge/",
		DomainStrategy: "ipv4_only",
	}
	initialTg := tgwebproxy.Config{
		Enabled:        false,
		PublicHostname: "host1.example.com",
	}
	initialDisp := cdndispatcher.Config{
		Enabled:    true,
		ListenAddr: ":9009",
	}

	xrayBytes, _ := json.Marshal(initialXray)
	tgBytes, _ := json.Marshal(initialTg)
	dispBytes, _ := json.Marshal(initialDisp)

	_ = os.WriteFile(xrayFile, xrayBytes, 0600)
	_ = os.WriteFile(tgFile, tgBytes, 0600)
	_ = os.WriteFile(dispFile, dispBytes, 0600)

	xrayComp := vlessserver.New(tempDir, nil)
	xrayComp.SetTestRuntimeHook(func(cfg xhttpserver.Config) error { return nil })
	dispComp := cdndispatcher.NewWithDataDir(tempDir, initialDisp)
	tgComp := tgwebproxy.New(tempDir, nil)

	coord := New(tempDir, xrayComp, dispComp, tgComp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := coord.StartConfigWatcher(ctx)
	defer watcher.Stop()

	// 1. Modify xray config on disk
	time.Sleep(100 * time.Millisecond)
	updatedXray := initialXray
	updatedXray.DomainStrategy = "prefer_ipv4"
	updatedXrayBytes, _ := json.Marshal(updatedXray)
	_ = os.WriteFile(xrayFile, updatedXrayBytes, 0600)

	// Wait for watcher to pick it up
	var sawStrategy bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cfg := xrayComp.GetConfig()
		if cfg.DomainStrategy == "prefer_ipv4" {
			sawStrategy = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !sawStrategy {
		t.Fatalf("expected watcher to reload DomainStrategy to prefer_ipv4")
	}

	// 2. Test ReloadFromDisk
	updatedXray.DomainStrategy = "ipv4_only"
	updatedXrayBytes2, _ := json.Marshal(updatedXray)
	_ = os.WriteFile(xrayFile, updatedXrayBytes2, 0600)

	if err := coord.ReloadFromDisk(context.Background()); err != nil {
		t.Fatalf("ReloadFromDisk failed: %v", err)
	}

	if xrayComp.GetConfig().DomainStrategy != "ipv4_only" {
		t.Fatalf("expected ReloadFromDisk to update DomainStrategy to ipv4_only")
	}
}
