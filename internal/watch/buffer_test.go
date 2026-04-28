package watch

import (
	"testing"
	"time"
)

func TestBufferFlushAtSizeThreshold(t *testing.T) {
	b := NewBuffer(BufferConfig{
		MaxLines: 10,
		MaxAge:   time.Hour,
		Now:      func() time.Time { return time.Unix(0, 0) },
	})
	for i := 0; i < 9; i++ {
		if b.ShouldFlush() {
			t.Fatalf("flush at %d lines", i)
		}
		b.Add([]byte(`{}`))
	}
	b.Add([]byte(`{}`)) // 10th line
	if !b.ShouldFlush() {
		t.Fatal("expected flush at 10 lines")
	}
}

func TestBufferFlushAtAge(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBuffer(BufferConfig{
		MaxLines: 100,
		MaxAge:   5 * time.Second,
		Now:      func() time.Time { return now },
	})
	b.Add([]byte(`{}`))
	if b.ShouldFlush() {
		t.Fatal("flush too early")
	}
	now = now.Add(6 * time.Second)
	if !b.ShouldFlush() {
		t.Fatal("expected flush after 6s")
	}
}

func TestBufferDrainResets(t *testing.T) {
	b := NewBuffer(BufferConfig{MaxLines: 2, MaxAge: time.Hour, Now: time.Now})
	b.Add([]byte(`{"a":1}`))
	b.Add([]byte(`{"b":2}`))
	if !b.ShouldFlush() {
		t.Fatal("expected flush")
	}
	lines := b.Drain()
	if len(lines) != 2 {
		t.Fatalf("drained %d want 2", len(lines))
	}
	if b.ShouldFlush() {
		t.Fatal("flush after drain")
	}
	if b.Len() != 0 {
		t.Fatal("len != 0 after drain")
	}
}
