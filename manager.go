package main

import (
	"crypto/rand"
	"fmt"
	"sync"
)

// Manager tracks all managed processes and enforces limits.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process
	config    *Config
}

// NewManager creates a process manager with the given configuration.
func NewManager(config *Config) *Manager {
	return &Manager{
		processes: make(map[string]*Process),
		config:    config,
	}
}

// Start spawns a new background process and registers it with the manager.
func (m *Manager) Start(command, name, cwd string, env map[string]string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.processes) >= m.config.MaxProcesses {
		return nil, fmt.Errorf("max processes reached (%d)", m.config.MaxProcesses)
	}

	id := generateID()

	if name == "" {
		name = id
	}

	p, err := newProcess(id, name, command, cwd, env, m.config.BufferSize)
	if err != nil {
		return nil, err
	}

	m.processes[id] = p
	return p, nil
}

// Stop terminates a process by ID and returns it.
func (m *Manager) Stop(id string) (*Process, error) {
	p, err := m.Get(id)
	if err != nil {
		return nil, err
	}

	if err := p.Stop(m.config.KillTimeout); err != nil {
		return p, err
	}

	return p, nil
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
			_ = proc.Stop(m.config.KillTimeout)
		}(p)
	}
	wg.Wait()
}

// generateID produces a short random identifier like "sc-a1b2c3".
func generateID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sc-%x", b)
}
