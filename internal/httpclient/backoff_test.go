package httpclient

import (
	"testing"
	"time"
)

func TestBackoffMonotonicAndCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 0; attempt < 12; attempt++ {
		got := Backoff(attempt)
		if got > 5*time.Minute+time.Second {
			t.Fatalf("attempt %d: backoff %v exceeds cap", attempt, got)
		}
		if attempt > 0 && got < prev/2 {
			t.Fatalf("attempt %d: backoff %v dropped sharply from %v", attempt, got, prev)
		}
		prev = got
	}
}

func TestBackoffJitterBounded(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := Backoff(0)
		if got < time.Second || got >= 2*time.Second {
			t.Fatalf("Backoff(0)=%v not in [1s, 2s)", got)
		}
	}
}

func TestBackoffCapEnforced(t *testing.T) {
	got := Backoff(20)
	if got < 5*time.Minute || got >= 5*time.Minute+time.Second {
		t.Fatalf("Backoff(20)=%v not in [5m, 5m+1s)", got)
	}
}
