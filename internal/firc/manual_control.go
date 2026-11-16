package firc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrManuallyDisabled = errors.New("firc: disabled by user; explicitly enable FIRC first")

// LockRuntimeMutation serializes package operations with migration and process
// lifecycle. Callers must not call Start/Stop while holding this lock.
func (o *Operator) LockRuntimeMutation() func() {
	o.mutationMu.Lock()
	o.lifecycleMu.Lock()
	return func() { o.lifecycleMu.Unlock(); o.mutationMu.Unlock() }
}

// The init script uses the same durable marker. Internal shutdown/rollback Stop
// must not modify user intent, unlike the explicit UI action.
func (o *Operator) disabledPath() string { return filepath.Join(o.configDir, ".awgm-disabled") }

func (o *Operator) ManuallyDisabled() bool {
	_, err := os.Lstat(o.disabledPath())
	return !os.IsNotExist(err) // fail closed if intent cannot be read
}

func (o *Operator) StopByUser(ctx context.Context) error {
	o.mutationMu.Lock()
	defer o.mutationMu.Unlock()
	o.lifecycleMu.Lock()
	defer o.lifecycleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(o.configDir, 0700); err != nil {
		return err
	}
	if err := atomicWriteYAMLFile(o.disabledPath(), []byte("disabled by user\n"), 0600); err != nil {
		return fmt.Errorf("persist disabled state: %w", err)
	}
	// Drain any pass that began before the disable marker. Later passes check
	// the marker under the same netfilter lock and cannot reinstall hooks.
	extNetfilterGen.Add(1)
	o.netfilterMu.Lock()
	defer o.netfilterMu.Unlock()
	// Keep disabled intent even if stop fails, so recovery cannot relaunch it.
	if err := o.stopRuntime(ctx); err != nil {
		return err
	}
	if o.IsRunning(ctx) {
		return errors.New("firc: daemon still responds after stop")
	}
	return nil
}

func (o *Operator) StartByUser(ctx context.Context) error {
	o.mutationMu.Lock()
	defer o.mutationMu.Unlock()
	o.lifecycleMu.Lock()
	defer o.lifecycleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := o.checkMutationAllowed(); err != nil {
		return err
	}
	wasDisabled := o.ManuallyDisabled()
	if err := os.Remove(o.disabledPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if wasDisabled {
		if err := syncParentDir(o.disabledPath(), nil); err != nil {
			return errors.Join(err, atomicWriteYAMLFile(o.disabledPath(), []byte("disabled by user\n"), 0600))
		}
	}
	if err := o.startRuntime(ctx); err != nil {
		if wasDisabled {
			if restoreErr := atomicWriteYAMLFile(o.disabledPath(), []byte("disabled by user\n"), 0600); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cleanupErr := o.stopRuntime(cleanupCtx); cleanupErr != nil {
				return errors.Join(err, fmt.Errorf("cleanup failed enable: %w", cleanupErr))
			}
		}
		return err
	}
	return nil
}
