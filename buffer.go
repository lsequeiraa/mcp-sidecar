package main

import (
	"strings"
	"sync"
)

// LineBuffer is a thread-safe, line-based ring buffer that caps memory usage
// by evicting the oldest lines when totalBytes exceeds maxBytes.
// It implements io.Writer so it can be wired directly to exec.Cmd's Stdout/Stderr.
type LineBuffer struct {
	mu         sync.Mutex
	lines      []string
	partial    string // incomplete line (no trailing newline yet)
	totalBytes int
	maxBytes   int
}

// NewLineBuffer creates a buffer that stores up to maxBytes of line data.
func NewLineBuffer(maxBytes int) *LineBuffer {
	return &LineBuffer{
		maxBytes: maxBytes,
	}
}

// Write implements io.Writer. It splits incoming data on newlines, stores
// complete lines, and keeps any trailing partial line until the next write
// completes it.
func (b *LineBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	data := b.partial + string(p)
	b.partial = ""

	parts := strings.Split(data, "\n")

	// Last element is either empty (data ended with \n) or a partial line.
	if parts[len(parts)-1] != "" {
		b.partial = parts[len(parts)-1]
	}
	// Process all complete lines (everything except the last split element).
	for i := 0; i < len(parts)-1; i++ {
		line := parts[i]
		lineBytes := len(line)
		b.lines = append(b.lines, line)
		b.totalBytes += lineBytes
	}

	b.evict()
	return len(p), nil
}

// evict drops the oldest lines until totalBytes is within maxBytes.
// Must be called with mu held.
func (b *LineBuffer) evict() {
	for b.totalBytes > b.maxBytes && len(b.lines) > 0 {
		b.totalBytes -= len(b.lines[0])
		b.lines = b.lines[1:]
	}
}

// Lines returns the last n complete lines. If n <= 0, all lines are returned.
func (b *LineBuffer) Lines(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n <= 0 || n > len(b.lines) {
		out := make([]string, len(b.lines))
		copy(out, b.lines)
		return out
	}

	start := len(b.lines) - n
	out := make([]string, n)
	copy(out, b.lines[start:])
	return out
}

// Len returns the total bytes of stored complete lines.
func (b *LineBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totalBytes
}
