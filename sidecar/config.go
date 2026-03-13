package sidecar

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultMaxProcesses = 10
	defaultBufferSize   = 1_048_576 // 1 MB
	defaultKillTimeout  = 5000      // milliseconds
	defaultCleanupAfter = 1800      // seconds (30 minutes)
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	MaxProcesses  int
	BufferSize    int
	KillTimeout   time.Duration
	CleanupAfter  time.Duration // 0 = disabled; exited processes removed after this duration
	MaxOutputSize int           // 0 = unlimited; cap on bytes returned by the output tool
}

// LoadConfig reads configuration from environment variables, falling back to
// defaults defined in PLAN.md.
func LoadConfig() *Config {
	return &Config{
		MaxProcesses:  envInt("SIDECAR_MAX_PROCESSES", defaultMaxProcesses),
		BufferSize:    envInt("SIDECAR_BUFFER_SIZE", defaultBufferSize),
		KillTimeout:   time.Duration(envInt("SIDECAR_KILL_TIMEOUT", defaultKillTimeout)) * time.Millisecond,
		CleanupAfter:  time.Duration(envInt("SIDECAR_CLEANUP_AFTER", defaultCleanupAfter)) * time.Second,
		MaxOutputSize: envInt("SIDECAR_MAX_OUTPUT_SIZE", 0),
	}
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
