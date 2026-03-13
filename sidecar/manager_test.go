package sidecar

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateID_Format(t *testing.T) {
	id := generateID()

	if !strings.HasPrefix(id, "sc-") {
		t.Errorf("generateID() = %q, want prefix 'sc-'", id)
	}

	// "sc-" (3 chars) + 6 hex chars = 9 total
	if len(id) != 9 {
		t.Errorf("generateID() = %q (len %d), want length 9", id, len(id))
	}

	// Verify hex suffix.
	hex := id[3:]
	for _, c := range hex {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("generateID() = %q, non-hex char %q in suffix", id, string(c))
		}
	}
}

func TestGenerateID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		if seen[id] {
			t.Fatalf("generateID() produced duplicate %q on iteration %d", id, i)
		}
		seen[id] = true
	}
}

func TestNewManager(t *testing.T) {
	cfg := &Config{MaxProcesses: 5, BufferSize: 1024, KillTimeout: 3 * time.Second}
	mgr := NewManager(cfg, nil, nil)

	if mgr.config != cfg {
		t.Error("NewManager did not store config")
	}
	if mgr.processes == nil {
		t.Error("NewManager did not initialize processes map")
	}
	if len(mgr.processes) != 0 {
		t.Errorf("new manager has %d processes, want 0", len(mgr.processes))
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	cfg := &Config{MaxProcesses: 5, BufferSize: 1024, KillTimeout: 3 * time.Second}
	mgr := NewManager(cfg, nil, nil)

	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("Get(nonexistent) returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestManager_Get_Found(t *testing.T) {
	cfg := &Config{MaxProcesses: 5, BufferSize: 1024, KillTimeout: 3 * time.Second}
	mgr := NewManager(cfg, nil, nil)

	// Inject a process directly.
	p := &Process{ID: "sc-test1", Name: "test", State: StateRunning}
	mgr.processes["sc-test1"] = p

	got, err := mgr.Get("sc-test1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ID != "sc-test1" {
		t.Errorf("got ID %q, want %q", got.ID, "sc-test1")
	}
}

func TestManager_List_Empty(t *testing.T) {
	cfg := &Config{MaxProcesses: 5, BufferSize: 1024, KillTimeout: 3 * time.Second}
	mgr := NewManager(cfg, nil, nil)

	procs := mgr.List()
	if len(procs) != 0 {
		t.Errorf("List() on empty manager returned %d processes, want 0", len(procs))
	}
}

func TestManager_List_WithProcesses(t *testing.T) {
	cfg := &Config{MaxProcesses: 5, BufferSize: 1024, KillTimeout: 3 * time.Second}
	mgr := NewManager(cfg, nil, nil)

	mgr.processes["sc-a"] = &Process{ID: "sc-a", Name: "a", State: StateRunning}
	mgr.processes["sc-b"] = &Process{ID: "sc-b", Name: "b", State: StateExited}

	procs := mgr.List()
	if len(procs) != 2 {
		t.Fatalf("List() returned %d processes, want 2", len(procs))
	}

	ids := map[string]bool{}
	for _, p := range procs {
		ids[p.ID] = true
	}
	if !ids["sc-a"] || !ids["sc-b"] {
		t.Errorf("List() returned unexpected IDs: %v", ids)
	}
}

// --- Uptime tests ---

func TestProcess_Uptime_Running(t *testing.T) {
	p := &Process{
		StartTime: time.Now().Add(-10 * time.Second),
		State:     StateRunning,
	}

	uptime := p.Uptime()
	if uptime < 9*time.Second || uptime > 11*time.Second {
		t.Errorf("Uptime() = %v, want ~10s", uptime)
	}
}

func TestProcess_Uptime_Exited(t *testing.T) {
	start := time.Now().Add(-60 * time.Second)
	end := start.Add(5 * time.Second) // ran for 5 seconds

	p := &Process{
		StartTime: start,
		EndTime:   end,
		State:     StateExited,
	}

	uptime := p.Uptime()
	if uptime != 5*time.Second {
		t.Errorf("Uptime() = %v, want 5s (actual runtime, not time since start)", uptime)
	}
}

// --- Cleanup tests ---

func TestManager_Cleanup_RemovesExpiredProcess(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Second,
	}
	mgr := NewManager(cfg, nil, nil)
	defer mgr.Close()

	// Inject an exited process with EndTime in the past.
	p := &Process{
		ID:        "sc-expired",
		Name:      "expired",
		State:     StateExited,
		StartTime: time.Now().Add(-10 * time.Second),
		EndTime:   time.Now().Add(-5 * time.Second), // exited 5s ago, TTL is 1s
		done:      make(chan struct{}),
	}
	close(p.done)

	mgr.mu.Lock()
	mgr.processes["sc-expired"] = p
	mgr.mu.Unlock()

	// Run cleanup directly.
	mgr.cleanupExpired()

	mgr.mu.RLock()
	_, found := mgr.processes["sc-expired"]
	mgr.mu.RUnlock()

	if found {
		t.Error("expired process should have been removed by cleanup")
	}
}

func TestManager_Cleanup_KeepsRunningProcess(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Second,
	}
	mgr := NewManager(cfg, nil, nil)
	defer mgr.Close()

	p := &Process{
		ID:        "sc-running",
		Name:      "running",
		State:     StateRunning,
		StartTime: time.Now().Add(-10 * time.Second),
		done:      make(chan struct{}),
	}

	mgr.mu.Lock()
	mgr.processes["sc-running"] = p
	mgr.mu.Unlock()

	mgr.cleanupExpired()

	mgr.mu.RLock()
	_, found := mgr.processes["sc-running"]
	mgr.mu.RUnlock()

	if !found {
		t.Error("running process should NOT be removed by cleanup")
	}
}

func TestManager_Cleanup_KeepsRecentlyExited(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Hour,
	}
	mgr := NewManager(cfg, nil, nil)
	defer mgr.Close()

	p := &Process{
		ID:        "sc-recent",
		Name:      "recent",
		State:     StateExited,
		StartTime: time.Now().Add(-10 * time.Second),
		EndTime:   time.Now().Add(-5 * time.Second), // exited 5s ago, TTL is 1h
		done:      make(chan struct{}),
	}
	close(p.done)

	mgr.mu.Lock()
	mgr.processes["sc-recent"] = p
	mgr.mu.Unlock()

	mgr.cleanupExpired()

	mgr.mu.RLock()
	_, found := mgr.processes["sc-recent"]
	mgr.mu.RUnlock()

	if !found {
		t.Error("recently exited process should NOT be removed (within TTL)")
	}
}

func TestManager_Cleanup_DisabledWhenZero(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 0, // disabled
	}
	mgr := NewManager(cfg, nil, nil)

	if mgr.cleanupDone != nil {
		t.Error("cleanupDone channel should be nil when CleanupAfter=0")
	}
}

func TestManager_Cleanup_RemovesFailedProcess(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Second,
	}
	mgr := NewManager(cfg, nil, nil)
	defer mgr.Close()

	p := &Process{
		ID:        "sc-failed",
		Name:      "failed",
		State:     StateFailed,
		ExitCode:  1,
		StartTime: time.Now().Add(-10 * time.Second),
		EndTime:   time.Now().Add(-5 * time.Second),
		done:      make(chan struct{}),
	}
	close(p.done)

	mgr.mu.Lock()
	mgr.processes["sc-failed"] = p
	mgr.mu.Unlock()

	mgr.cleanupExpired()

	mgr.mu.RLock()
	_, found := mgr.processes["sc-failed"]
	mgr.mu.RUnlock()

	if found {
		t.Error("failed process should have been removed by cleanup")
	}
}

func TestManager_Cleanup_RemovesKilledProcess(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Second,
	}
	mgr := NewManager(cfg, nil, nil)
	defer mgr.Close()

	p := &Process{
		ID:        "sc-killed",
		Name:      "killed",
		State:     StateKilled,
		ExitCode:  -1,
		StartTime: time.Now().Add(-10 * time.Second),
		EndTime:   time.Now().Add(-5 * time.Second),
		done:      make(chan struct{}),
	}
	close(p.done)

	mgr.mu.Lock()
	mgr.processes["sc-killed"] = p
	mgr.mu.Unlock()

	mgr.cleanupExpired()

	mgr.mu.RLock()
	_, found := mgr.processes["sc-killed"]
	mgr.mu.RUnlock()

	if found {
		t.Error("killed process should have been removed by cleanup")
	}
}

func TestManager_Close_Idempotent(t *testing.T) {
	cfg := &Config{
		MaxProcesses: 5,
		BufferSize:   1024,
		KillTimeout:  3 * time.Second,
		CleanupAfter: 1 * time.Second,
	}
	mgr := NewManager(cfg, nil, nil)

	// Should not panic on repeated Close calls.
	mgr.Close()
	mgr.Close()
}
