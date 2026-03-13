package sidecar

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Ensure env vars are unset.
	t.Setenv("SIDECAR_MAX_PROCESSES", "")
	t.Setenv("SIDECAR_BUFFER_SIZE", "")
	t.Setenv("SIDECAR_KILL_TIMEOUT", "")
	t.Setenv("SIDECAR_CLEANUP_AFTER", "")
	t.Setenv("SIDECAR_MAX_OUTPUT_SIZE", "")

	// Unset by clearing (Setenv to "" then unset via lookup behavior).
	// envInt checks os.LookupEnv, which returns ok=true for empty string,
	// then Atoi("") fails, so it falls back to default. Same effect.

	cfg := LoadConfig()

	if cfg.MaxProcesses != defaultMaxProcesses {
		t.Errorf("MaxProcesses = %d, want %d", cfg.MaxProcesses, defaultMaxProcesses)
	}
	if cfg.BufferSize != defaultBufferSize {
		t.Errorf("BufferSize = %d, want %d", cfg.BufferSize, defaultBufferSize)
	}
	if cfg.KillTimeout != time.Duration(defaultKillTimeout)*time.Millisecond {
		t.Errorf("KillTimeout = %v, want %v", cfg.KillTimeout, time.Duration(defaultKillTimeout)*time.Millisecond)
	}
	if cfg.CleanupAfter != time.Duration(defaultCleanupAfter)*time.Second {
		t.Errorf("CleanupAfter = %v, want %v", cfg.CleanupAfter, time.Duration(defaultCleanupAfter)*time.Second)
	}
	if cfg.MaxOutputSize != 0 {
		t.Errorf("MaxOutputSize = %d, want 0", cfg.MaxOutputSize)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	t.Setenv("SIDECAR_MAX_PROCESSES", "20")
	t.Setenv("SIDECAR_BUFFER_SIZE", "2048")
	t.Setenv("SIDECAR_KILL_TIMEOUT", "10000")
	t.Setenv("SIDECAR_CLEANUP_AFTER", "60")
	t.Setenv("SIDECAR_MAX_OUTPUT_SIZE", "524288")

	cfg := LoadConfig()

	if cfg.MaxProcesses != 20 {
		t.Errorf("MaxProcesses = %d, want 20", cfg.MaxProcesses)
	}
	if cfg.BufferSize != 2048 {
		t.Errorf("BufferSize = %d, want 2048", cfg.BufferSize)
	}
	if cfg.KillTimeout != 10*time.Second {
		t.Errorf("KillTimeout = %v, want 10s", cfg.KillTimeout)
	}
	if cfg.CleanupAfter != 60*time.Second {
		t.Errorf("CleanupAfter = %v, want 60s", cfg.CleanupAfter)
	}
	if cfg.MaxOutputSize != 524288 {
		t.Errorf("MaxOutputSize = %d, want 524288", cfg.MaxOutputSize)
	}
}

func TestLoadConfig_CleanupDisabled(t *testing.T) {
	t.Setenv("SIDECAR_CLEANUP_AFTER", "0")

	cfg := LoadConfig()

	if cfg.CleanupAfter != 0 {
		t.Errorf("CleanupAfter = %v, want 0 (disabled)", cfg.CleanupAfter)
	}
}

func TestLoadConfig_InvalidValues(t *testing.T) {
	t.Setenv("SIDECAR_MAX_PROCESSES", "notanumber")
	t.Setenv("SIDECAR_BUFFER_SIZE", "abc")
	t.Setenv("SIDECAR_KILL_TIMEOUT", "xyz")

	cfg := LoadConfig()

	if cfg.MaxProcesses != defaultMaxProcesses {
		t.Errorf("MaxProcesses = %d, want default %d", cfg.MaxProcesses, defaultMaxProcesses)
	}
	if cfg.BufferSize != defaultBufferSize {
		t.Errorf("BufferSize = %d, want default %d", cfg.BufferSize, defaultBufferSize)
	}
	if cfg.KillTimeout != time.Duration(defaultKillTimeout)*time.Millisecond {
		t.Errorf("KillTimeout = %v, want default %v", cfg.KillTimeout, time.Duration(defaultKillTimeout)*time.Millisecond)
	}
}

func TestLoadConfig_PartialOverride(t *testing.T) {
	t.Setenv("SIDECAR_MAX_PROCESSES", "5")
	t.Setenv("SIDECAR_BUFFER_SIZE", "")      // invalid -> default
	t.Setenv("SIDECAR_KILL_TIMEOUT", "3000") // valid

	cfg := LoadConfig()

	if cfg.MaxProcesses != 5 {
		t.Errorf("MaxProcesses = %d, want 5", cfg.MaxProcesses)
	}
	if cfg.BufferSize != defaultBufferSize {
		t.Errorf("BufferSize = %d, want default %d", cfg.BufferSize, defaultBufferSize)
	}
	if cfg.KillTimeout != 3*time.Second {
		t.Errorf("KillTimeout = %v, want 3s", cfg.KillTimeout)
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		setEnv   bool
		fallback int
		want     int
	}{
		{
			name:     "valid value",
			key:      "TEST_ENVINT_VALID",
			envVal:   "42",
			setEnv:   true,
			fallback: 10,
			want:     42,
		},
		{
			name:     "invalid value falls back",
			key:      "TEST_ENVINT_INVALID",
			envVal:   "notanumber",
			setEnv:   true,
			fallback: 10,
			want:     10,
		},
		{
			name:     "empty value falls back",
			key:      "TEST_ENVINT_EMPTY",
			envVal:   "",
			setEnv:   true,
			fallback: 10,
			want:     10,
		},
		{
			name:     "zero is valid",
			key:      "TEST_ENVINT_ZERO",
			envVal:   "0",
			setEnv:   true,
			fallback: 10,
			want:     0,
		},
		{
			name:     "negative is valid",
			key:      "TEST_ENVINT_NEG",
			envVal:   "-5",
			setEnv:   true,
			fallback: 10,
			want:     -5,
		},
		{
			name:     "unset env var falls back",
			key:      "TEST_ENVINT_GUARANTEED_UNSET_KEY",
			setEnv:   false,
			fallback: 99,
			want:     99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envVal)
			}
			got := envInt(tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("envInt(%q, %d) = %d, want %d", tt.key, tt.fallback, got, tt.want)
			}
		})
	}
}
