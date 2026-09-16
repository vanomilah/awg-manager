package xrayserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/xrayconfig"
)

func TestDiskSecretStore_LifecycleAndIsolation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xray-sec-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewDiskSecretStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskSecretStore: %v", err)
	}

	ctx := context.Background()
	txID := "tx-test-01"

	// 1. Stage secret
	ref, err := store.StageSecret(ctx, txID, "password", "test-pass", "secret-password-val")
	if err != nil {
		t.Fatalf("StageSecret failed: %v", err)
	}

	if ref.ID == "" {
		t.Fatal("expected non-empty secret ID")
	}

	// Should not be resolvable yet before commit
	_, err = store.ResolveSecret(ctx, ref)
	if err == nil {
		t.Fatal("expected error resolving uncommitted secret, got nil")
	}

	// 2. Commit transaction
	if err := store.CommitTx(ctx, txID); err != nil {
		t.Fatalf("CommitTx failed: %v", err)
	}

	// Should now resolve successfully
	resolved, err := store.ResolveSecret(ctx, ref)
	if err != nil {
		t.Fatalf("ResolveSecret after commit failed: %v", err)
	}
	if resolved != "secret-password-val" {
		t.Errorf("expected 'secret-password-val', got %q", resolved)
	}

	// 3. List secrets - must return metadata without cleartext
	list, err := store.ListSecrets(ctx)
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != ref.ID {
		t.Fatalf("expected 1 secret with ID %s, got %v", ref.ID, list)
	}

	// 4. Staged secret rollback
	txID2 := "tx-test-02"
	ref2, err := store.StageSecret(ctx, txID2, "key", "test-key", "key-val")
	if err != nil {
		t.Fatalf("StageSecret 2 failed: %v", err)
	}
	if err := store.RollbackTx(ctx, txID2); err != nil {
		t.Fatalf("RollbackTx failed: %v", err)
	}
	_, err = store.ResolveSecret(ctx, ref2)
	if err == nil {
		t.Fatal("expected error resolving rolled back secret")
	}

	// 5. Path traversal protection
	badRef := xrayconfig.SecretRef{ID: "../escape"}
	_, err = store.ResolveSecret(ctx, badRef)
	if err == nil {
		t.Fatal("expected error on path traversal ID")
	}
}

func TestDiskProfileStore_GenerationsAndCrashConsistency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xray-prof-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewDiskProfileStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskProfileStore: %v", err)
	}

	ctx := context.Background()
	profileID := "prof-01"

	cfg1 := &xrayconfig.ManagedConfig{
		LogLevel: "warning",
		Inbounds: []xrayconfig.Inbound{
			{Tag: "vless-in", Port: 9008, Protocol: "vless"},
		},
		Outbounds: []xrayconfig.Outbound{
			{Tag: "direct", Protocol: "freedom"},
		},
	}

	// 1. Save generation 1
	gen1, err := store.SaveProfile(ctx, profileID, "Primary Profile", xrayconfig.RoleServer, true, cfg1, nil)
	if err != nil {
		t.Fatalf("SaveProfile gen1 failed: %v", err)
	}

	active, err := store.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile failed: %v", err)
	}
	if active.Metadata.GenerationID != gen1 {
		t.Errorf("expected active generation %s, got %s", gen1, active.Metadata.GenerationID)
	}
	if active.Managed.Inbounds[0].Port != 9008 {
		t.Errorf("expected port 9008, got %d", active.Managed.Inbounds[0].Port)
	}

	// 2. Save generation 2
	cfg2 := &xrayconfig.ManagedConfig{
		LogLevel: "info",
		Inbounds: []xrayconfig.Inbound{
			{Tag: "vless-in", Port: 8443, Protocol: "vless"},
		},
		Outbounds: []xrayconfig.Outbound{
			{Tag: "direct", Protocol: "freedom"},
		},
	}
	rawOverlay := []byte(`{"customOverlayField": true}`)

	gen2, err := store.SaveProfile(ctx, profileID, "Primary Profile", xrayconfig.RoleServer, true, cfg2, rawOverlay)
	if err != nil {
		t.Fatalf("SaveProfile gen2 failed: %v", err)
	}

	active2, err := store.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile gen2 failed: %v", err)
	}
	if active2.Metadata.GenerationID != gen2 {
		t.Errorf("expected active generation %s, got %s", gen2, active2.Metadata.GenerationID)
	}
	if active2.Managed.Inbounds[0].Port != 8443 {
		t.Errorf("expected port 8443, got %d", active2.Managed.Inbounds[0].Port)
	}
	if !strings.Contains(string(active2.RawOverlay), "customOverlayField") {
		t.Errorf("expected rawOverlay preserved in gen2: %s", string(active2.RawOverlay))
	}

	// 3. Rollback to generation 1
	if err := store.RollbackToGeneration(ctx, profileID, gen1); err != nil {
		t.Fatalf("RollbackToGeneration failed: %v", err)
	}
	activeAfterRollback, err := store.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile after rollback failed: %v", err)
	}
	if activeAfterRollback.Metadata.GenerationID != gen1 {
		t.Errorf("expected active generation to be %s after rollback, got %s", gen1, activeAfterRollback.Metadata.GenerationID)
	}
	if activeAfterRollback.Managed.Inbounds[0].Port != 9008 {
		t.Errorf("expected port 9008 restored, got %d", activeAfterRollback.Managed.Inbounds[0].Port)
	}

	// 4. Index recovery from generation metadata
	index, err := store.RecoverIndex(ctx)
	if err != nil {
		t.Fatalf("RecoverIndex failed: %v", err)
	}
	if len(index) != 1 || index[0].ProfileID != profileID {
		t.Fatalf("expected 1 recovered profile %s, got %v", profileID, index)
	}
}

func TestLegacyToManagedConfig_Conversion(t *testing.T) {
	legacy := Config{
		Enabled:           true,
		ListenAddress:     "0.0.0.0",
		ListenPort:        9008,
		PublicDomain:      "cdn.example.com",
		Path:              "/cdn-bridge/",
		Transport:         "xhttp",
		OutboundMode:      "socks",
		OutboundSocksPort: 1099,
		Clients: []Client{
			{ID: "ca293bc6-771b-4443-9f0e-ca22fa600f8b", Remark: "Phone (Happ)", Enabled: true},
		},
	}

	managed := LegacyToManagedConfig(legacy)
	if managed == nil {
		t.Fatal("expected non-nil managed config")
	}

	if len(managed.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(managed.Inbounds))
	}
	in := managed.Inbounds[0]
	if in.Listen != "0.0.0.0" || in.Port != 9008 || in.Transport != "xhttp" {
		t.Errorf("unexpected inbound fields: %+v", in)
	}
	if len(in.Clients) != 1 || in.Clients[0].UUID != "ca293bc6-771b-4443-9f0e-ca22fa600f8b" {
		t.Errorf("unexpected clients in inbound: %+v", in.Clients)
	}

	if len(managed.Outbounds) != 2 {
		t.Fatalf("expected 2 outbounds (socks proxy + direct), got %d", len(managed.Outbounds))
	}
	if managed.Outbounds[0].Protocol != "socks" || managed.Outbounds[0].Port != 1099 {
		t.Errorf("unexpected primary outbound: %+v", managed.Outbounds[0])
	}

	if len(managed.RoutingRules) != 1 || managed.RoutingRules[0].OutboundTag != "mihomo-proxy" {
		t.Errorf("unexpected routing rules: %+v", managed.RoutingRules)
	}
}

func TestDiskProfileStore_FailureBetweenManagedAndRaw(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "prof-fail-1-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewDiskProfileStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskProfileStore: %v", err)
	}

	ctx := context.Background()
	profileID := "prof-fail-test"

	cfg1 := &xrayconfig.ManagedConfig{
		LogLevel: "warning",
		Inbounds: []xrayconfig.Inbound{{Tag: "in-1", Port: 9008, Protocol: "vless"}},
	}
	gen1, err := store.SaveProfile(ctx, profileID, "Stable Profile", xrayconfig.RoleServer, true, cfg1, nil)
	if err != nil {
		t.Fatalf("SaveProfile gen1 failed: %v", err)
	}

	// Simulate a partial crash during writing gen2 (e.g. managed.json written, but failed before raw.json / active pointer)
	incompleteGenDir := store.generationPath(profileID, "gen-interrupted-02")
	_ = os.MkdirAll(incompleteGenDir, 0700)
	_ = os.WriteFile(filepath.Join(incompleteGenDir, "managed.json"), []byte(`{"inbounds":[{"tag":"interrupted"}]}`), 0600)
	// active.json was NOT replaced

	// Active profile must still remain intact as gen1
	active, err := store.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile failed after partial generation write: %v", err)
	}
	if active.Metadata.GenerationID != gen1 {
		t.Errorf("expected active generation %s, got %s", gen1, active.Metadata.GenerationID)
	}
	if active.Managed.Inbounds[0].Port != 9008 {
		t.Errorf("expected port 9008, got %d", active.Managed.Inbounds[0].Port)
	}
}

func TestDiskProfileStore_FailureBeforeActivePointer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "prof-fail-2-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewDiskProfileStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskProfileStore: %v", err)
	}

	ctx := context.Background()
	profileID := "prof-fail-ptr"

	cfg1 := &xrayconfig.ManagedConfig{
		LogLevel: "warning",
		Inbounds: []xrayconfig.Inbound{{Tag: "in-1", Port: 9008, Protocol: "vless"}},
	}
	gen1, err := store.SaveProfile(ctx, profileID, "Profile 1", xrayconfig.RoleServer, true, cfg1, nil)
	if err != nil {
		t.Fatalf("SaveProfile gen1 failed: %v", err)
	}

	// Fully write a new generation directory, but simulate power failure right before active.json pointer update
	unpointedGenDir := store.generationPath(profileID, "gen-unpointed")
	_ = os.MkdirAll(unpointedGenDir, 0700)
	_ = os.WriteFile(filepath.Join(unpointedGenDir, "managed.json"), []byte(`{"inbounds":[{"tag":"unpointed","port":1234}]}`), 0600)
	_ = os.WriteFile(filepath.Join(unpointedGenDir, "metadata.json"), []byte(`{"generation_id":"gen-unpointed"}`), 0600)

	// Active profile must remain gen1
	active, err := store.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile failed: %v", err)
	}
	if active.Metadata.GenerationID != gen1 {
		t.Errorf("expected active generation %s, got %s", gen1, active.Metadata.GenerationID)
	}
}

func TestDiskProfileStore_CrashAfterActivePointer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "prof-crash-ptr-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store1, err := NewDiskProfileStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskProfileStore 1: %v", err)
	}

	ctx := context.Background()
	profileID := "prof-crash-restart"

	cfg := &xrayconfig.ManagedConfig{
		LogLevel: "info",
		Inbounds: []xrayconfig.Inbound{{Tag: "in-1", Port: 8443, Protocol: "vless"}},
	}
	genID, err := store1.SaveProfile(ctx, profileID, "Profile Restart", xrayconfig.RoleServer, true, cfg, nil)
	if err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Simulate service restart by creating a completely fresh store instance pointing to same directory
	store2, err := NewDiskProfileStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskProfileStore 2: %v", err)
	}

	active, err := store2.GetActiveProfile(ctx, profileID)
	if err != nil {
		t.Fatalf("GetActiveProfile after restart failed: %v", err)
	}
	if active.Metadata.GenerationID != genID {
		t.Errorf("expected generation %s, got %s", genID, active.Metadata.GenerationID)
	}
	if active.Managed.Inbounds[0].Port != 8443 {
		t.Errorf("expected port 8443, got %d", active.Managed.Inbounds[0].Port)
	}
}

func TestDiskSecretStore_CorruptMetadataDoesNotDeleteSecrets(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sec-corrupt-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewDiskSecretStore(tempDir)
	if err != nil {
		t.Fatalf("NewDiskSecretStore: %v", err)
	}

	ctx := context.Background()
	secretID := "sec-corrupt-test"
	secretPath := filepath.Join(tempDir, "active", secretID+".json")

	// Write corrupt JSON into secret file
	if err := os.WriteFile(secretPath, []byte(`{not-valid-json`), 0600); err != nil {
		t.Fatalf("write corrupt secret file: %v", err)
	}

	ref := xrayconfig.SecretRef{ID: secretID}
	_, err = store.ResolveSecret(ctx, ref)
	if err == nil {
		t.Fatal("expected error resolving corrupt secret, got nil")
	}

	// Verify that secret file is NOT deleted automatically (so manual recovery is possible)
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		t.Fatal("corrupt secret file was unexpectedly deleted automatically!")
	}
}

