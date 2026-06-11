package router

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
)

// These tests cover the direct-save path that replaced SaveDraft for
// system-driven config writes (Enable / Disable legacy / healTProxyInbound).
// The bug they regress against: on every router reboot a `pending/20-router.json`
// appeared because the boot-time Reconcile→Enable cycle staged its
// idempotently-regenerated config as if it were a user edit, leaving the
// UI banner "Несохранённые правки" stuck until the user clicked Apply.
//
// The fix splits persistConfig (still staged) from persistConfigDirect
// (direct write to active, with byte-equal short-circuit). Boot recovery
// goes through persistConfigDirect → no pending → no banner.

func TestPersistConfigDirect_NoOpWhenActiveMatches(t *testing.T) {
	svc, dir := newOrchedTestService(t)

	// Active file pre-exists with what marshalling NewEmptyConfig would
	// produce — Bootstrap below sees it and marks the slot enabled.
	cfg := NewEmptyConfig()
	bytesNow, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	activePath := filepath.Join(dir, "20-router.json")
	if err := os.WriteFile(activePath, bytesNow, 0644); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	// Re-bootstrap so the orchestrator picks up the active file.
	if err := svc.deps.Orch.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Capture mtime to verify atomic rewrite did NOT happen.
	before, err := os.Stat(activePath)
	if err != nil {
		t.Fatalf("stat active: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // separate possible mtime windows

	if err := svc.persistConfigDirect(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfigDirect: %v", err)
	}

	after, err := os.Stat(activePath)
	if err != nil {
		t.Fatalf("stat active after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("active should not be re-written when bytes match (before=%v after=%v)", before.ModTime(), after.ModTime())
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("pending must not exist after byte-equal direct save: %v", err)
	}
}

func TestPersistConfigDirect_WritesActiveWhenDifferent(t *testing.T) {
	svc, dir := newOrchedTestService(t)

	// Seed active with stale bytes (different from what marshalling our
	// cfg below will produce). Bootstrap marks the slot enabled.
	activePath := filepath.Join(dir, "20-router.json")
	if err := os.WriteFile(activePath, []byte(`{"stale": true}`), 0644); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	if err := svc.deps.Orch.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	cfg := NewEmptyConfig()
	cfg.Route.Rules = append(cfg.Route.Rules, Rule{Action: "route", Outbound: "direct"})

	if err := svc.persistConfigDirect(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfigDirect: %v", err)
	}

	got, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read active: %v", err)
	}
	want, _ := json.MarshalIndent(cfg, "", "  ")
	if string(got) != string(want) {
		t.Errorf("active not overwritten with new bytes\nwant: %s\ngot:  %s", want, got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("pending must not exist after direct save: %v", err)
	}
}

func TestPersistConfigDirect_WritesActiveWhenAbsent(t *testing.T) {
	svc, dir := newOrchedTestService(t)

	// No active file. Bootstrap sees nothing → enabled=false; explicit
	// SetEnabled flips it to true so orch.Save targets activePath.
	if err := svc.deps.Orch.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := svc.deps.Orch.SetEnabled(orchestrator.SlotRouter, true); err != nil {
		t.Fatalf("SetEnabled true: %v", err)
	}

	cfg := NewEmptyConfig()
	cfg.Route.Rules = append(cfg.Route.Rules, Rule{Action: "route", Outbound: "direct"})

	if err := svc.persistConfigDirect(context.Background(), cfg); err != nil {
		t.Fatalf("persistConfigDirect: %v", err)
	}

	activePath := filepath.Join(dir, "20-router.json")
	got, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read active: %v", err)
	}
	want, _ := json.MarshalIndent(cfg, "", "  ")
	if string(got) != string(want) {
		t.Errorf("active not created with expected bytes\nwant: %s\ngot:  %s", want, got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("pending must not exist after direct save: %v", err)
	}
}

func TestWaitForSingbox_ReturnsWhenRunning(t *testing.T) {
	svc, _ := newOrchedTestService(t)
	stubListeningProbe(t, func() bool { return true })

	calls := 0
	svc.deps.Singbox.(*fakeSingbox).isRunningFn = func() (bool, int) {
		calls++
		return calls >= 3, 1234 // false, false, true
	}

	start := time.Now()
	if err := svc.waitForSingbox(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("waitForSingbox: %v", err)
	}
	if calls < 3 {
		t.Errorf("expected at least 3 polls, got %d", calls)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waitForSingbox took unexpectedly long: %v", elapsed)
	}
}

func TestWaitForSingbox_TimesOutWhenNeverRunning(t *testing.T) {
	svc, _ := newOrchedTestService(t)
	// Default fakeSingbox.IsRunning returns (false, 0) — perfect for this case.

	start := time.Now()
	err := svc.waitForSingbox(context.Background(), 250*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("waitForSingbox returned too early: %v", elapsed)
	}
	if elapsed > 1*time.Second {
		t.Errorf("waitForSingbox returned too late: %v", elapsed)
	}
}
