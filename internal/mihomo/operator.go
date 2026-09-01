package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Operator manages the Mihomo process and implements proxyengine.Engine
type Operator struct {
	mu             sync.Mutex
	binaryPath     string
	configDir      string
	cmd            *exec.Cmd
	stopping       *exec.Cmd
	done           chan struct{}
	running        bool
	pid            int
	generation     uint64
	lastError      string
	commandFn      func(string, ...string) *exec.Cmd
	readyFn        func(context.Context) error
	afterWait      func()
	cleanupStaleFn func(string, string) error
	onExit         func(uint64)
}

func NewOperator(binaryPath, configDir string) *Operator {
	return &Operator{
		binaryPath:     binaryPath,
		configDir:      configDir,
		commandFn:      exec.Command,
		cleanupStaleFn: cleanupStaleManagedProcesses,
	}
}

// SetOnUnexpectedExit registers a fail-safe invoked after a non-intentional
// process exit. Production uses it to withdraw NDMS ProxyN exports immediately
// so they never keep routing into dead loopback listeners.
func (o *Operator) SetOnUnexpectedExit(fn func(generation uint64)) {
	o.mu.Lock()
	o.onExit = fn
	o.mu.Unlock()
}

// CurrentGeneration identifies the most recently started process. Exit hooks
// use it together with the bridge-runtime mutex to ignore a delayed callback
// from an older process after a successful restart.
func (o *Operator) CurrentGeneration() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.generation
}

func (o *Operator) Reload() error {
	if running, _ := o.IsRunning(); !running {
		return o.Start()
	}

	// Remove cache.db so url-test groups start fresh without fixed selections
	// that were persisted when the group was in selector mode.
	_ = os.Remove(filepath.Join(o.configDir, "cache.db"))

	payload := map[string]string{
		"path": filepath.Join(o.configDir, "config.yaml"),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return o.recordError(err)
	}

	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9090/configs?force=true", bytes.NewReader(body))
	if err != nil {
		return o.recordError(err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return o.recordError(fmt.Errorf("mihomo reload request failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return o.recordError(fmt.Errorf("mihomo reload failed with status: %s", resp.Status))
	}
	o.clearError()
	return nil
}

func (o *Operator) IsRunning() (bool, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// A process selected for shutdown must not be treated as a valid reload
	// target. Reload falls through to Start, which waits for done before
	// launching the replacement instead of PUTing into a dying controller.
	if o.running && o.stopping == nil {
		return true, o.pid
	}
	return false, 0
}

func (o *Operator) Start() error {
	stopDeadline := time.Now().Add(5 * time.Second)
	for {
		o.mu.Lock()
		if o.stopping == nil {
			break
		}
		done := o.done
		o.mu.Unlock()
		if done == nil {
			return o.recordError(fmt.Errorf("mihomo startup blocked by incomplete shutdown"))
		}
		remaining := time.Until(stopDeadline)
		if remaining <= 0 {
			return o.recordError(fmt.Errorf("mihomo startup timed out waiting for previous process shutdown"))
		}
		timer := time.NewTimer(remaining)
		select {
		case <-done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			return o.recordError(fmt.Errorf("mihomo startup timed out waiting for previous process shutdown"))
		}
	}

	if o.running {
		o.mu.Unlock()
		return nil // already running
	}
	if o.cleanupStaleFn != nil {
		if err := o.cleanupStaleFn(o.binaryPath, o.configDir); err != nil {
			o.lastError = fmt.Sprintf("cleanup stale Mihomo process: %v", err)
			o.mu.Unlock()
			return fmt.Errorf("cleanup stale Mihomo process: %w", err)
		}
	}

	// Remove cache.db so url-test groups start fresh without fixed selections
	// that were persisted when the group was in selector mode.
	_ = os.Remove(filepath.Join(o.configDir, "cache.db"))

	cmd := o.commandFn(o.binaryPath, "-d", o.configDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		o.lastError = err.Error()
		o.mu.Unlock()
		return err
	}
	o.cmd = cmd
	o.running = true
	o.pid = cmd.Process.Pid
	done := make(chan struct{})
	o.done = done
	o.generation++
	generation := o.generation
	o.lastError = ""

	readyFn := o.readyFn
	o.mu.Unlock()
	go o.wait(cmd, generation, &stdout, &stderr, done)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if readyFn == nil {
		readyFn = o.waitForController
	}
	if err := readyFn(ctx); err != nil {
		_ = o.Stop()
		return o.recordError(fmt.Errorf("mihomo startup failed: %w", err))
	}
	// A stale controller could answer while the just-started process is about
	// to fail on a busy port. Require a short stable window before success.
	select {
	case <-ctx.Done():
		_ = o.Stop()
		return o.recordError(fmt.Errorf("mihomo startup failed: %w", ctx.Err()))
	case <-time.After(200 * time.Millisecond):
	}
	if running, _ := o.IsRunning(); !running {
		detail := o.LastError()
		if detail == "" {
			detail = "process exited during startup"
		}
		return o.recordError(fmt.Errorf("mihomo startup failed: %s", detail))
	}
	o.clearError()
	return nil
}

func (o *Operator) waitForController(ctx context.Context) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if running, _ := o.IsRunning(); !running {
			detail := o.LastError()
			if detail == "" {
				detail = "process exited before controller became ready"
			}
			return fmt.Errorf("%s", detail)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9090/version", nil)
		if err == nil {
			if resp, requestErr := client.Do(req); requestErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("controller readiness: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (o *Operator) wait(cmd *exec.Cmd, generation uint64, stdout, stderr *bytes.Buffer, done chan struct{}) {
	err := cmd.Wait()
	if o.afterWait != nil {
		o.afterWait()
	}

	o.mu.Lock()
	intentionalStop := o.stopping == cmd
	if intentionalStop {
		o.stopping = nil
	}
	if o.cmd == cmd {
		o.cmd = nil
		o.running = false
		o.pid = 0
	}
	if o.done == done {
		o.done = nil
	}
	if intentionalStop {
		o.mu.Unlock()
		close(done)
		return
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			o.lastError = fmt.Sprintf("mihomo exited: %v: %s", err, detail)
		} else {
			o.lastError = fmt.Sprintf("mihomo exited: %v", err)
		}
	} else {
		o.lastError = "mihomo exited unexpectedly"
	}
	onExit := o.onExit
	o.mu.Unlock()
	close(done)
	if onExit != nil {
		go onExit(generation)
	}
}

func (o *Operator) ClearManualStop() error {
	return nil
}

func (o *Operator) ValidateConfigDir(ctx context.Context) error {
	// Mihomo test config command: mihomo -d configDir -t
	cmd := exec.CommandContext(ctx, o.binaryPath, "-d", o.configDir, "-t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mihomo validation failed: %s (%w)", string(out), err)
	}
	return nil
}

func (o *Operator) ConfigDir() string {
	return o.configDir
}

func (o *Operator) Binary() string {
	return o.binaryPath
}

func (o *Operator) LastError() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastError
}

func (o *Operator) CrashStats() (int, string, time.Time) {
	return 0, "", time.Time{}
}

func (o *Operator) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return o.StopAndWait(ctx)
}

// StopAndWait does not return until the child has been reaped (or ctx
// expires). This makes an immediate Start deterministic and prevents it from
// observing the dying process as an already-running no-op.
func (o *Operator) StopAndWait(ctx context.Context) error {
	o.mu.Lock()
	if !o.running || o.cmd == nil || o.cmd.Process == nil {
		done := o.done
		if done == nil && o.cleanupStaleFn != nil {
			if err := o.cleanupStaleFn(o.binaryPath, o.configDir); err != nil {
				o.lastError = fmt.Sprintf("cleanup stale Mihomo process during stop: %v", err)
				o.mu.Unlock()
				return fmt.Errorf("cleanup stale Mihomo process during stop: %w", err)
			}
		}
		o.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return fmt.Errorf("wait for Mihomo shutdown: %w", ctx.Err())
			}
		}
		return nil
	}

	cmd := o.cmd
	done := o.done
	var killErr error
	if o.stopping == nil {
		o.stopping = cmd
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			killErr = fmt.Errorf("kill Mihomo process: %w", err)
			o.lastError = killErr.Error()
		}
	}
	o.mu.Unlock()

	if done == nil {
		return killErr
	}
	select {
	case <-done:
		return killErr
	case <-ctx.Done():
		return errors.Join(killErr, fmt.Errorf("wait for Mihomo shutdown: %w", ctx.Err()))
	}
}

func (o *Operator) recordError(err error) error {
	o.mu.Lock()
	o.lastError = err.Error()
	o.mu.Unlock()
	return err
}

func (o *Operator) clearError() {
	o.mu.Lock()
	o.lastError = ""
	o.mu.Unlock()
}
