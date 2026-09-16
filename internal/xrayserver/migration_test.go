package xrayserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverLegacyTopologyA(t *testing.T) {
	cfgPath := filepath.Join("testdata", "legacy_direct.json")
	initPath := filepath.Join("testdata", "legacy_init_script.sh")

	disc, err := DiscoverLegacy(cfgPath, initPath)
	if err != nil {
		t.Fatalf("DiscoverLegacy failed: %v", err)
	}

	if !disc.Found {
		t.Fatalf("expected legacy config to be found")
	}
	if disc.Topology != TopologyA {
		t.Fatalf("expected TopologyA, got %v", disc.Topology)
	}
	if disc.ListenPort != 9009 {
		t.Fatalf("expected ListenPort 9009, got %d", disc.ListenPort)
	}
	if disc.PublicDomain != "vpn.example.com" {
		t.Fatalf("expected vpn.example.com, got %s", disc.PublicDomain)
	}
	if len(disc.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(disc.Clients))
	}
	if disc.Clients[0].ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected client id: %s", disc.Clients[0].ID)
	}
	if disc.Checksums["config.json"] == "" {
		t.Fatalf("expected sha256 checksum for config.json")
	}
}

func TestDiscoverLegacyTopologyB(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfgJSON := `{
		"inbounds": [
			{
				"listen": "127.0.0.1",
				"port": 9008,
				"protocol": "vless",
				"settings": {
					"clients": [{"id": "uuid-123"}]
				},
				"streamSettings": {
					"network": "xhttp",
					"xhttpSettings": {
						"host": "custom.domain.com",
						"path": "/cdn-bridge/"
					}
				}
			}
		]
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	disc, err := DiscoverLegacy(cfgPath, "")
	if err != nil {
		t.Fatalf("DiscoverLegacy failed: %v", err)
	}
	if disc.Topology != TopologyB {
		t.Fatalf("expected TopologyB, got %v", disc.Topology)
	}
	if disc.ListenAddress != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %s", disc.ListenAddress)
	}
	if disc.ListenPort != 9008 {
		t.Fatalf("expected port 9008, got %d", disc.ListenPort)
	}
}

func TestDiscoverLegacyTopologyC(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	// Multiple VLESS inbounds -> ambiguous
	cfgJSON := `{
		"inbounds": [
			{"protocol": "vless", "port": 9008},
			{"protocol": "vless", "port": 9009}
		]
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	disc, err := DiscoverLegacy(cfgPath, "")
	if err != nil {
		t.Fatalf("DiscoverLegacy failed: %v", err)
	}
	if disc.Topology != TopologyC {
		t.Fatalf("expected TopologyC, got %v", disc.Topology)
	}
	if disc.ConflictReason == "" {
		t.Fatalf("expected ConflictReason to be populated")
	}
}

func TestMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyCfg := filepath.Join(dir, "legacy_config.json")
	initScript := filepath.Join(dir, "S99xray-cdn")

	origCfg, err := os.ReadFile(filepath.Join("testdata", "legacy_direct.json"))
	if err != nil {
		t.Fatal(err)
	}
	origInit, err := os.ReadFile(filepath.Join("testdata", "legacy_init_script.sh"))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(legacyCfg, origCfg, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initScript, origInit, 0755); err != nil {
		t.Fatal(err)
	}

	disc, err := DiscoverLegacy(legacyCfg, initScript)
	if err != nil {
		t.Fatalf("DiscoverLegacy failed: %v", err)
	}

	// First Migration
	newCfg, err := MigrateLegacy(context.Background(), disc, dir, 9008)
	if err != nil {
		t.Fatalf("MigrateLegacy failed: %v", err)
	}

	if newCfg.ListenAddress != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %s", newCfg.ListenAddress)
	}
	if newCfg.ListenPort != 9008 {
		t.Fatalf("expected 9008, got %d", newCfg.ListenPort)
	}
	if newCfg.DispatcherPort != 9009 {
		t.Fatalf("expected 9009, got %d", newCfg.DispatcherPort)
	}
	if len(newCfg.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(newCfg.Clients))
	}

	// Init script must be renamed to .disabled
	if fileExists(initScript) {
		t.Fatalf("legacy init script should have been renamed")
	}
	if !fileExists(initScript + ".disabled") {
		t.Fatalf("renamed init script .disabled does not exist")
	}

	// Marker and decision files must exist
	markerPath := filepath.Join(dir, "xray", "migration-marker.json")
	if !fileExists(markerPath) {
		t.Fatalf("migration-marker.json not found")
	}

	decision, err := GetRuntimeDecision(dir)
	if err != nil {
		t.Fatalf("GetRuntimeDecision failed: %v", err)
	}
	if decision.ActiveGeneration != "new" {
		t.Fatalf("expected active_generation=new, got %s", decision.ActiveGeneration)
	}

	// Idempotency: second migration must return ErrAlreadyMigrated
	_, err = MigrateLegacy(context.Background(), disc, dir, 9008)
	if !errors.Is(err, ErrAlreadyMigrated) {
		t.Fatalf("expected ErrAlreadyMigrated, got %v", err)
	}
}

func TestRuntimeDecisionPersistence(t *testing.T) {
	dir := t.TempDir()
	d := RuntimeDecision{
		ActiveGeneration:  "legacy",
		MigrationStatus:   "deferred",
		GenerationCounter: 2,
		Reason:            "user requested keep_legacy",
	}

	if err := SaveRuntimeDecision(dir, d); err != nil {
		t.Fatalf("SaveRuntimeDecision failed: %v", err)
	}

	readD, err := GetRuntimeDecision(dir)
	if err != nil {
		t.Fatalf("GetRuntimeDecision failed: %v", err)
	}
	if readD.ActiveGeneration != "legacy" {
		t.Fatalf("expected legacy, got %s", readD.ActiveGeneration)
	}
	if readD.GenerationCounter != 2 {
		t.Fatalf("expected 2, got %d", readD.GenerationCounter)
	}
}

func TestDiscoverLegacySymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	initDir := filepath.Join(tmpDir, "init.d")
	evilDir := filepath.Join(tmpDir, "evil")
	_ = os.MkdirAll(initDir, 0755)
	_ = os.MkdirAll(evilDir, 0755)

	evilTarget := filepath.Join(evilDir, "evil_script.sh")
	_ = os.WriteFile(evilTarget, []byte("#!/bin/sh\n"), 0755)

	symlinkPath := filepath.Join(initDir, "S99xray")
	err := os.Symlink(evilTarget, symlinkPath)
	if err != nil {
		t.Skip("skipping symlink test (symlink creation not supported without privs)")
	}

	cfgPath := filepath.Join(tmpDir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(`{"inbounds":[{"protocol":"vless","port":9008}]}`), 0600)

	disc, err := DiscoverLegacy(cfgPath, symlinkPath)
	if err != nil {
		t.Fatalf("DiscoverLegacy failed: %v", err)
	}
	if disc.Topology != TopologyC {
		t.Fatalf("expected TopologyC for escaping symlink, got %v", disc.Topology)
	}
	if disc.ConflictReason == "" || !strings.Contains(disc.ConflictReason, "escapes trusted init.d directory") {
		t.Fatalf("expected escape conflict reason, got: %s", disc.ConflictReason)
	}
}

func TestMigrateLegacyRollbackOnError(t *testing.T) {
	tmpDir := t.TempDir()
	initScript := filepath.Join(tmpDir, "S99xray")
	_ = os.WriteFile(initScript, []byte("#!/bin/sh\n"), 0755)

	disc := &LegacyDiscoveryResult{
		Found:          true,
		Topology:       TopologyA,
		ListenPort:     9009,
		InitScriptPath: initScript,
		Checksums:      map[string]string{"config.json": "abcd"},
	}

	// Create a directory where migration-marker.json should be so writing marker fails
	xrayDir := filepath.Join(tmpDir, "xray")
	_ = os.MkdirAll(filepath.Join(xrayDir, "migration-marker.json"), 0755)

	_, err := MigrateLegacy(context.Background(), disc, tmpDir, 9008)
	if err == nil {
		t.Fatalf("expected MigrateLegacy to fail when marker cannot be written")
	}

	// Verify rollback: init script must be restored and NOT left as .disabled
	if !fileExists(initScript) {
		t.Fatalf("expected init script to be restored to original path on failure")
	}
	if fileExists(initScript + ".disabled") {
		t.Fatalf("disabled init script still exists after rollback")
	}
}

func TestMigrateLegacyRollbackRestoresPreexistingSettings(t *testing.T) {
	tmpDir := t.TempDir()
	initScript := filepath.Join(tmpDir, "S99xray")
	_ = os.WriteFile(initScript, []byte("#!/bin/sh\n"), 0755)

	xrayDir := filepath.Join(tmpDir, "xray")
	_ = os.MkdirAll(xrayDir, 0755)
	settingsPath := filepath.Join(xrayDir, "xray-server-settings.json")
	origBytes := []byte(`{"pre_existing": "critical_user_settings_do_not_delete"}`)
	if err := os.WriteFile(settingsPath, origBytes, 0600); err != nil {
		t.Fatalf("write orig settings: %v", err)
	}

	disc := &LegacyDiscoveryResult{
		Found:          true,
		Topology:       TopologyA,
		ListenPort:     9009,
		InitScriptPath: initScript,
		Checksums:      map[string]string{"config.json": "abcd"},
	}

	// Create a directory where migration-marker.json should be so writing marker fails
	_ = os.MkdirAll(filepath.Join(xrayDir, "migration-marker.json"), 0755)

	_, err := MigrateLegacy(context.Background(), disc, tmpDir, 9008)
	if err == nil {
		t.Fatalf("expected MigrateLegacy to fail when marker cannot be written")
	}

	// Verify pre-existing settings file is preserved byte-for-byte
	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("pre-existing settings file was deleted on rollback: %v", err)
	}
	if string(restored) != string(origBytes) {
		t.Fatalf("settings file corrupted on rollback; want %q, got %q", string(origBytes), string(restored))
	}
}
