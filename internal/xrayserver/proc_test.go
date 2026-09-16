package xrayserver

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestManagedProc_Lifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping process test on windows (uses sleep)")
	}
	cmd := exec.Command("sleep", "0.2")
	proc := newManagedProc(cmd)

	if proc.IsRunning() {
		t.Errorf("expected not running before Start")
	}

	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start failed: %v", err)
	}

	if proc.PID() <= 0 {
		t.Errorf("expected valid PID, got %d", proc.PID())
	}

	if !proc.IsRunning() {
		t.Errorf("expected running after Start")
	}

	// Wait for process to exit normally
	select {
	case <-proc.Exited():
	case <-time.After(2 * time.Second):
		t.Fatalf("process did not exit in time")
	}

	if proc.IsRunning() {
		t.Errorf("expected not running after exit")
	}

	if err := proc.ExitError(); err != nil {
		t.Errorf("expected nil ExitError on clean exit, got: %v", err)
	}
}

func TestManagedProc_StopGraceful(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping process test on windows (uses sleep)")
	}
	cmd := exec.Command("sleep", "5")
	proc := newManagedProc(cmd)

	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start failed: %v", err)
	}

	if err := proc.Stop(500 * time.Millisecond); err != nil {
		t.Fatalf("proc.Stop failed: %v", err)
	}

	if proc.IsRunning() {
		t.Errorf("expected process to be stopped")
	}
}
