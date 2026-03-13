package sidecar

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ProcessState represents the lifecycle state of a managed process.
type ProcessState string

const (
	StateRunning ProcessState = "running"
	StateExited  ProcessState = "exited"
	StateFailed  ProcessState = "failed"
	StateKilled  ProcessState = "killed"
)

// Process wraps a single child process, managing its I/O streams,
// lifecycle state, and output buffers.
type Process struct {
	ID        string
	Name      string
	Command   string
	Pid       int
	State     ProcessState
	ExitCode  int
	StartTime time.Time

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	Stdout *LineBuffer
	Stderr *LineBuffer

	mu     sync.Mutex
	killed bool // true if stopped via the stop tool
	done   chan struct{}
}

// newProcess configures and starts a child process using the provided
// exec.Cmd. It wires stdout and stderr into LineBuffers and launches a
// goroutine to wait for the process to exit. The caller is responsible
// for constructing cmd (shell-wrapped or direct exec).
func newProcess(id, name, command, cwd string, env map[string]string, bufSize int, cmd *exec.Cmd) (*Process, error) {
	setSysProcAttr(cmd)

	if cwd != "" {
		cmd.Dir = cwd
	}

	if len(env) > 0 {
		// Inherit current environment, then overlay provided vars.
		cmd.Env = cmd.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutBuf := NewLineBuffer(bufSize)
	stderrBuf := NewLineBuffer(bufSize)

	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	p := &Process{
		ID:        id,
		Name:      name,
		Command:   command,
		Pid:       cmd.Process.Pid,
		State:     StateRunning,
		StartTime: time.Now(),
		cmd:       cmd,
		stdin:     stdinPipe,
		Stdout:    stdoutBuf,
		Stderr:    stderrBuf,
		done:      make(chan struct{}),
	}

	go p.waitForExit()

	return p, nil
}

// waitForExit blocks until the process exits, then updates state and exit code.
func (p *Process) waitForExit() {
	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.killed {
		p.State = StateKilled
	} else if err != nil {
		p.State = StateFailed
	} else {
		p.State = StateExited
	}

	if p.cmd.ProcessState != nil {
		p.ExitCode = p.cmd.ProcessState.ExitCode()
	}

	close(p.done)
}

// Stop sends a graceful termination signal, waits for the timeout, then
// force-kills if the process hasn't exited.
func (p *Process) Stop(timeout time.Duration) error {
	p.mu.Lock()
	if p.State != StateRunning {
		p.mu.Unlock()
		return nil
	}
	p.killed = true
	pid := p.Pid
	p.mu.Unlock()

	// Attempt graceful stop.
	_ = gracefulStop(pid)

	// Wait for exit or timeout.
	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
	}

	// Force kill (best-effort; process may have already exited from graceful
	// stop, making the PID invalid).
	forceErr := forceKill(pid)

	// Wait for the process to fully exit.
	select {
	case <-p.done:
		return nil
	case <-time.After(3 * time.Second):
		if forceErr != nil {
			return fmt.Errorf("force kill pid %d: %w", pid, forceErr)
		}
		return fmt.Errorf("process pid %d did not exit after force kill", pid)
	}
}

// Send writes data to the process's stdin.
func (p *Process) Send(input string) error {
	p.mu.Lock()
	if p.State != StateRunning {
		p.mu.Unlock()
		return fmt.Errorf("process %s is not running (state: %s)", p.ID, p.State)
	}
	p.mu.Unlock()

	_, err := io.WriteString(p.stdin, input)
	return err
}

// Uptime returns how long the process has been (or was) alive.
func (p *Process) Uptime() time.Duration {
	return time.Since(p.StartTime).Truncate(time.Second)
}

// IsRunning returns true if the process is still alive.
func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State == StateRunning
}
