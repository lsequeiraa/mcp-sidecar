package sidecar

import (
	"crypto/rand"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Manager tracks all managed processes and enforces limits.
type Manager struct {
	mu          sync.RWMutex
	processes   map[string]*Process
	config      *Config
	security    *SecurityValidator
	audit       *AuditLogger
	cleanupOnce sync.Once     // ensures Close only fires once
	cleanupDone chan struct{} // closed to stop the cleanup goroutine
}

// NewManager creates a process manager with the given configuration.
// security and audit may be nil to disable those features.
// If CleanupAfter > 0, a background goroutine periodically removes
// exited processes; call Close to stop it.
func NewManager(config *Config, security *SecurityValidator, audit *AuditLogger) *Manager {
	m := &Manager{
		processes: make(map[string]*Process),
		config:    config,
		security:  security,
		audit:     audit,
	}

	if config.CleanupAfter > 0 {
		m.cleanupDone = make(chan struct{})
		m.startCleanup()
	}

	return m
}

// Start spawns a new background process and registers it with the manager.
// When security is enabled, the command is validated against the allowlist
// and executed directly (no shell). Otherwise it is run through the
// platform shell (sh -c / cmd /C).
func (m *Manager) Start(command, name, cwd string, env map[string]string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.processes) >= m.config.MaxProcesses {
		return nil, fmt.Errorf("max processes reached (%d)", m.config.MaxProcesses)
	}

	// Security validation + command building.
	var cmd *exec.Cmd
	if m.security.IsEnabled() {
		if err := m.security.ValidateCommand(command); err != nil {
			m.audit.LogBlocked(command, err.Error())
			return nil, fmt.Errorf("security: %w", err)
		}
		directCmd, err := m.security.BuildCommand(command)
		if err != nil {
			return nil, fmt.Errorf("security: %w", err)
		}
		cmd = directCmd
	} else {
		cmd = buildCommand(command)
	}

	id := generateID()

	if name == "" {
		name = id
	}

	p, err := newProcess(id, name, command, cwd, env, m.config.BufferSize, cmd)
	if err != nil {
		return nil, err
	}

	m.audit.LogStart(id, command, cwd)

	m.processes[id] = p
	return p, nil
}

// Stop terminates a process by ID and returns it.
func (m *Manager) Stop(id string) (*Process, error) {
	p, err := m.Get(id)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	stopErr := p.Stop(m.config.KillTimeout)
	m.audit.LogStop(id, p.ExitCode, time.Since(start))

	return p, stopErr
}

// List returns all managed processes (running and finished).
func (m *Manager) List() []*Process {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Process, 0, len(m.processes))
	for _, p := range m.processes {
		out = append(out, p)
	}
	return out
}

// Get returns a single process by ID or an error if not found.
func (m *Manager) Get(id string) (*Process, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.processes[id]
	if !ok {
		return nil, fmt.Errorf("process %q not found", id)
	}
	return p, nil
}

// StopAll terminates every managed process. Used during server shutdown.
func (m *Manager) StopAll() {
	m.mu.RLock()
	procs := make([]*Process, 0, len(m.processes))
	for _, p := range m.processes {
		procs = append(procs, p)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(proc *Process) {
			defer wg.Done()
			start := time.Now()
			_ = proc.Stop(m.config.KillTimeout)
			m.audit.LogStop(proc.ID, proc.ExitCode, time.Since(start))
		}(p)
	}
	wg.Wait()
}

// Close stops the cleanup goroutine if one is running. It is safe to call
// multiple times.
func (m *Manager) Close() {
	m.cleanupOnce.Do(func() {
		if m.cleanupDone != nil {
			close(m.cleanupDone)
		}
	})
}

// startCleanup launches a background goroutine that periodically removes
// exited processes whose EndTime is older than CleanupAfter.
func (m *Manager) startCleanup() {
	interval := m.config.CleanupAfter
	if interval > 60*time.Second {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanupExpired()
			case <-m.cleanupDone:
				return
			}
		}
	}()
}

// cleanupExpired removes processes that have exited and whose EndTime is
// older than the configured CleanupAfter duration.
func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, p := range m.processes {
		p.mu.Lock()
		expired := p.State != StateRunning &&
			!p.EndTime.IsZero() &&
			now.Sub(p.EndTime) > m.config.CleanupAfter
		p.mu.Unlock()

		if expired {
			delete(m.processes, id)
		}
	}
}

// generateID produces a short random identifier like "sc-a1b2c3".
func generateID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sc-%x", b)
}
