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
