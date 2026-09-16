package xrayserver

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// managedProc manages a single child process lifecycle with a dedicated Wait goroutine
// and a broadcast channel for exit notification.
type managedProc struct {
	cmd       *exec.Cmd
	exited    chan struct{}
	waitErr   error
	closeOnce sync.Once
}

func newManagedProc(cmd *exec.Cmd) *managedProc {
	return &managedProc{
		cmd:    cmd,
		exited: make(chan struct{}),
	}
}

// Start launches cmd and spawns the single monitor goroutine that calls Wait().
func (p *managedProc) Start() error {
	if p.cmd == nil {
		return errors.New("nil command")
	}
	if err := p.cmd.Start(); err != nil {
		return err
	}

	go func() {
		err := p.cmd.Wait()
		p.closeOnce.Do(func() {
			p.waitErr = err
			close(p.exited)
		})
	}()

	return nil
}

// PID returns the process ID or 0 if not started.
func (p *managedProc) PID() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Exited returns the channel that is closed when the process terminates.
func (p *managedProc) Exited() <-chan struct{} {
	return p.exited
}

// ExitError returns the error from Wait(), valid only after Exited is closed.
func (p *managedProc) ExitError() error {
	select {
	case <-p.exited:
		return p.waitErr
	default:
		return nil
	}
}

// IsRunning returns true if the process was started and has not yet exited.
func (p *managedProc) IsRunning() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	select {
	case <-p.exited:
		return false
	default:
		return p.cmd.Process.Signal(syscall.Signal(0)) == nil
	}
}

// Stop gracefully terminates the process: SIGTERM -> wait up to grace -> SIGKILL -> wait.
func (p *managedProc) Stop(grace time.Duration) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	select {
	case <-p.exited:
		return nil
	default:
	}

	// Send SIGTERM
	_ = p.cmd.Process.Signal(syscall.SIGTERM)

	select {
	case <-p.exited:
		return nil
	case <-time.After(grace):
		// Force kill
		_ = p.cmd.Process.Kill()
		select {
		case <-p.exited:
			return nil
		case <-time.After(time.Second):
			return fmt.Errorf("process %d did not terminate after SIGKILL", p.PID())
		}
	}
}

// WaitExit waits for the process to exit with a given timeout.
func (p *managedProc) WaitExit(timeout time.Duration) error {
	select {
	case <-p.exited:
		return p.waitErr
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for process %d to exit", p.PID())
	}
}
