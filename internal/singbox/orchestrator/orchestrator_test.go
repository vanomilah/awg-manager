package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestOrch(t *testing.T) (*Orchestrator, string) {
	t.Helper()
	dir := t.TempDir()
	o := New(dir, nil) // nil ProcessController — Save/SetEnabled don't use it
	return o, dir
}

func TestRegisterAndBootstrap(t *testing.T) {
	o, dir := newTestOrch(t)
	if err := o.Register(SlotMeta{Slot: SlotBase, Filename: "00-base.json", AlwaysOn: true}); err != nil {
		t.Fatalf("register base: %v", err)
	}
	if err := o.Register(SlotMeta{Slot: SlotRouter, Filename: "20-router.json"}); err != nil {
		t.Fatalf("register router: %v", err)
	}
	if err := o.Register(SlotMeta{Slot: SlotRouter, Filename: "20-router.json"}); err == nil {
		t.Errorf("expected ErrSlotAlreadyRegistered on duplicate")
	}
	if err := o.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// disabled/ subdir must exist
	if _, err := os.Stat(filepath.Join(dir, "disabled")); err != nil {
		t.Errorf("disabled subdir missing: %v", err)
	}
	snap := o.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	// base AlwaysOn → enabled, no file yet → Present=false
	if !snap[0].Enabled {
		t.Errorf("base should be enabled (AlwaysOn)")
	}
	if snap[0].Present {
		t.Errorf("base file should not exist on fresh dir")
	}
}

func TestSaveWritesActivePathWhenEnabled(t *testing.T) {
	o, dir := newTestOrch(t)
	_ = o.Register(SlotMeta{Slot: SlotBase, Filename: "00-base.json", AlwaysOn: true})
	if err := o.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := o.Save(SlotBase, []byte(`{"x":1}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "00-base.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"x":1}` {
		t.Errorf("content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "disabled", "00-base.json")); !os.IsNotExist(err) {
		t.Errorf("disabled copy should not exist")
	}
}

func TestSaveWritesDisabledPathWhenDisabled(t *testing.T) {
	o, dir := newTestOrch(t)
	_ = o.Register(SlotMeta{Slot: SlotRouter, Filename: "20-router.json"})
	if err := o.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	// Slot is disabled by default (not AlwaysOn, no file yet).
	if err := o.Save(SlotRouter, []byte(`{"y":2}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "disabled", "20-router.json"))
	if err != nil {
		t.Fatalf("read disabled: %v", err)
	}
	if string(data) != `{"y":2}` {
		t.Errorf("content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("active copy should not exist")
	}
}

func TestSetEnabledRenamesFile(t *testing.T) {
	o, dir := newTestOrch(t)
	_ = o.Register(SlotMeta{Slot: SlotRouter, Filename: "20-router.json"})
	if err := o.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := o.Save(SlotRouter, []byte(`{"y":2}`)); err != nil {
		t.Fatal(err)
	}
	// Lives in disabled/.
	if err := o.SetEnabled(SlotRouter, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "20-router.json")); err != nil {
		t.Errorf("expected active path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "disabled", "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("disabled path should be empty")
	}
	// Idempotent.
	if err := o.SetEnabled(SlotRouter, true); err != nil {
		t.Errorf("idempotent enable failed: %v", err)
	}
	// Disable.
	if err := o.SetEnabled(SlotRouter, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "disabled", "20-router.json")); err != nil {
		t.Errorf("expected disabled path: %v", err)
	}
}

func TestSetEnabledRejectsAlwaysOn(t *testing.T) {
	o, _ := newTestOrch(t)
	_ = o.Register(SlotMeta{Slot: SlotBase, Filename: "00-base.json", AlwaysOn: true})
	if err := o.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := o.SetEnabled(SlotBase, false); err != ErrSlotAlwaysOn {
		t.Errorf("disable always-on should error, got %v", err)
	}
}

func TestSaveUnknownSlot(t *testing.T) {
	o, _ := newTestOrch(t)
	if err := o.Save(SlotRouter, []byte(`{}`)); err != ErrUnknownSlot {
		t.Errorf("expected ErrUnknownSlot, got %v", err)
	}
}

func TestBootstrapResolvesBothLocationsConflict(t *testing.T) {
	o, dir := newTestOrch(t)
	_ = o.Register(SlotMeta{Slot: SlotRouter, Filename: "20-router.json"})
	// Pre-seed BOTH locations.
	if err := os.MkdirAll(filepath.Join(dir, "disabled"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20-router.json"), []byte(`{"active":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "disabled", "20-router.json"), []byte(`{"stale":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := o.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "disabled", "20-router.json")); !os.IsNotExist(err) {
		t.Errorf("disabled stale copy should be removed")
	}
	snap := o.Snapshot()
	if len(snap) != 1 || !snap[0].Enabled {
		t.Errorf("router should be enabled after both-locations resolution: %+v", snap)
	}
}
