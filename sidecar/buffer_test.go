package sidecar

import (
	"fmt"
	"sync"
	"testing"
)

func TestLineBuffer_Write(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{
			name:   "single write with newlines",
			writes: []string{"hello\nworld\n"},
			want:   []string{"hello", "world"},
		},
		{
			name:   "multiple writes each with newline",
			writes: []string{"hello\n", "world\n"},
			want:   []string{"hello", "world"},
		},
		{
			name:   "partial line completed by next write",
			writes: []string{"hel", "lo\n"},
			want:   []string{"hello"},
		},
		{
			name:   "partial across three writes",
			writes: []string{"a", "b", "c\n"},
			want:   []string{"abc"},
		},
		{
			name:   "no trailing newline produces no complete lines",
			writes: []string{"hello"},
			want:   []string{},
		},
		{
			name:   "empty write is no-op",
			writes: []string{""},
			want:   []string{},
		},
		{
			name:   "only newlines produce empty lines",
			writes: []string{"\n\n\n"},
			want:   []string{"", "", ""},
		},
		{
			name:   "carriage return preserved (split on LF only)",
			writes: []string{"hello\r\nworld\r\n"},
			want:   []string{"hello\r", "world\r"},
		},
		{
			name:   "mixed complete and partial",
			writes: []string{"one\ntwo\nthre", "e\nfour\n"},
			want:   []string{"one", "two", "three", "four"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewLineBuffer(1024)
			for _, w := range tt.writes {
				buf.Write([]byte(w))
			}
			got := buf.Lines(0)
			if len(got) != len(tt.want) {
				t.Fatalf("Lines(0) returned %d lines %v, want %d lines %v",
					len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLineBuffer_Eviction(t *testing.T) {
	t.Run("evicts oldest lines when over capacity", func(t *testing.T) {
		buf := NewLineBuffer(10)
		// "aaaaa" (5) + "bbbbb" (5) + "ccccc" (5) = 15 bytes, limit 10
		buf.Write([]byte("aaaaa\nbbbbb\nccccc\n"))

		lines := buf.Lines(0)
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines after eviction, got %d: %v", len(lines), lines)
		}
		if lines[0] != "bbbbb" || lines[1] != "ccccc" {
			t.Errorf("lines = %v, want [bbbbb, ccccc]", lines)
		}
		if buf.Len() != 10 {
			t.Errorf("Len() = %d, want 10", buf.Len())
		}
	})

	t.Run("single line exceeding maxBytes is evicted", func(t *testing.T) {
		buf := NewLineBuffer(3)
		buf.Write([]byte("toolong\n")) // 7 bytes > 3

		lines := buf.Lines(0)
		if len(lines) != 0 {
			t.Errorf("expected 0 lines, got %d: %v", len(lines), lines)
		}
	})

	t.Run("progressive eviction across writes", func(t *testing.T) {
		buf := NewLineBuffer(6)
		buf.Write([]byte("aaa\n")) // 3 bytes, under limit
		buf.Write([]byte("bbb\n")) // 3+3 = 6, at limit
		buf.Write([]byte("ccc\n")) // 3+3+3 = 9, over limit -> evict "aaa"

		lines := buf.Lines(0)
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
		}
		if lines[0] != "bbb" || lines[1] != "ccc" {
			t.Errorf("lines = %v, want [bbb, ccc]", lines)
		}
	})
}

func TestLineBuffer_Lines(t *testing.T) {
	buf := NewLineBuffer(1024)
	buf.Write([]byte("a\nb\nc\nd\ne\n"))

	t.Run("n=0 returns all", func(t *testing.T) {
		got := buf.Lines(0)
		if len(got) != 5 {
			t.Errorf("Lines(0) returned %d lines, want 5", len(got))
		}
	})

	t.Run("n=2 returns last 2", func(t *testing.T) {
		got := buf.Lines(2)
		if len(got) != 2 || got[0] != "d" || got[1] != "e" {
			t.Errorf("Lines(2) = %v, want [d, e]", got)
		}
	})

	t.Run("n exceeding count returns all", func(t *testing.T) {
		got := buf.Lines(100)
		if len(got) != 5 {
			t.Errorf("Lines(100) returned %d lines, want 5", len(got))
		}
	})

	t.Run("negative n returns all", func(t *testing.T) {
		got := buf.Lines(-1)
		if len(got) != 5 {
			t.Errorf("Lines(-1) returned %d lines, want 5", len(got))
		}
	})

	t.Run("n=1 returns last line", func(t *testing.T) {
		got := buf.Lines(1)
		if len(got) != 1 || got[0] != "e" {
			t.Errorf("Lines(1) = %v, want [e]", got)
		}
	})
}

func TestLineBuffer_ReturnsCopy(t *testing.T) {
	buf := NewLineBuffer(1024)
	buf.Write([]byte("hello\n"))

	lines := buf.Lines(0)
	lines[0] = "modified"

	original := buf.Lines(0)
	if original[0] != "hello" {
		t.Error("Lines() did not return a copy; modifying result affected buffer")
	}
}

func TestLineBuffer_Len(t *testing.T) {
	buf := NewLineBuffer(1024)
	if buf.Len() != 0 {
		t.Errorf("empty buffer Len() = %d, want 0", buf.Len())
	}

	buf.Write([]byte("hello\n")) // 5 bytes of line content (newline not counted)
	if buf.Len() != 5 {
		t.Errorf("after 'hello\\n': Len() = %d, want 5", buf.Len())
	}

	buf.Write([]byte("world\n")) // +5 = 10
	if buf.Len() != 10 {
		t.Errorf("after 'world\\n': Len() = %d, want 10", buf.Len())
	}
}

func TestLineBuffer_WriteReturnsInputLen(t *testing.T) {
	buf := NewLineBuffer(1024)
	input := []byte("hello\nworld\n")
	n, err := buf.Write(input)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(input) {
		t.Errorf("Write returned %d, want %d", n, len(input))
	}
}

func TestLineBuffer_ConcurrentWrites(t *testing.T) {
	buf := NewLineBuffer(1024 * 1024) // large to avoid eviction

	const goroutines = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.Write([]byte(fmt.Sprintf("line-%03d\n", n)))
		}(i)
	}
	wg.Wait()

	lines := buf.Lines(0)
	if len(lines) != goroutines {
		t.Errorf("expected %d lines after concurrent writes, got %d", goroutines, len(lines))
	}
}

func TestLineBuffer_EmptyBuffer(t *testing.T) {
	buf := NewLineBuffer(1024)

	lines := buf.Lines(0)
	if len(lines) != 0 {
		t.Errorf("empty buffer Lines(0) = %v, want []", lines)
	}

	lines = buf.Lines(5)
	if len(lines) != 0 {
		t.Errorf("empty buffer Lines(5) = %v, want []", lines)
	}
}
