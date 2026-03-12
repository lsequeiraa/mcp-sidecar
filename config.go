package main

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultMaxProcesses = 10
	defaultBufferSize   = 1_048_576 // 1 MB
	defaultKillTimeout  = 5000      // milliseconds
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	MaxProcesses int
	BufferSize   int
	KillTimeout  time.Duration
}

// LoadConfig reads configuration from environment variables, falling back to
// defaults defined in PLAN.md.
func LoadConfig() *Config {
	return &Config{
		MaxProcesses: envInt("SIDECAR_MAX_PROCESSES", defaultMaxProcesses),
		BufferSize:   envInt("SIDECAR_BUFFER_SIZE", defaultBufferSize),
		KillTimeout:  time.Duration(envInt("SIDECAR_KILL_TIMEOUT", defaultKillTimeout)) * time.Millisecond,
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
