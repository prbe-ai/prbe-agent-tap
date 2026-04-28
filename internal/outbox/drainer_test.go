package outbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func mustOpenStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDrainerHappyPathPostsAllRows(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer srv.Close()

	s := mustOpenStorage(t)
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		_ = s.EnqueueBatch(storage.OutboxRow{
			SessionID: "sid", BatchSeq: int64(i), CWD: "/", Body: []byte(`{}`),
			CreatedAt: now, NextAttemptAt: now,
		})
	}

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	d := New(Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return "tok", nil },
		PollInterval:   10 * time.Millisecond,
		Now:            func() time.Time { return time.Now() },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go d.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&hits) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) < 3 {
		t.Fatalf("expected 3 hits, got %d", hits)
	}
}

func TestDrainerHaltsOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s := mustOpenStorage(t)
	now := time.Now().Unix()
	_ = s.EnqueueBatch(storage.OutboxRow{
		SessionID: "sid", BatchSeq: 0, CWD: "/", Body: []byte(`{}`),
		CreatedAt: now, NextAttemptAt: now,
	})

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	d := New(Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return "tok", nil },
		PollInterval:   10 * time.Millisecond,
		Now:            func() time.Time { return time.Now() },
	})

	err := d.Run(context.Background())
	if err == nil || !errIsHalt(err) {
		t.Fatalf("Run err = %v want halt", err)
	}

	last401, _ := s.GetMeta("last_401_at")
	if last401 == "" {
		t.Fatal("last_401_at not set")
	}
	row, ok, _ := s.NextDueBatch(time.Now().Unix() + 9999)
	if ok {
		t.Fatalf("outbox not cleared: %+v", row)
	}
}

func errIsHalt(err error) bool { return err != nil && err.Error() == "halted: device token revoked" }

// TestDrainerContinuesOnTransientDBError verifies that NextDueBatch failures
// are logged and skipped rather than causing the drainer to exit.
func TestDrainerContinuesOnTransientDBError(t *testing.T) {
	s := mustOpenStorage(t)

	// Enqueue one row so the drainer has something to try before we close the DB.
	now := time.Now().Unix()
	_ = s.EnqueueBatch(storage.OutboxRow{
		SessionID: "sid", BatchSeq: 0, CWD: "/", Body: []byte(`{}`),
		CreatedAt: now, NextAttemptAt: now,
	})

	// Close the storage immediately so every NextDueBatch call will fail.
	s.Close()

	// Use a no-op HTTP client; we only care that the drainer keeps ticking.
	hc := httpclient.New(httpclient.Options{BaseURL: "http://127.0.0.1:0", Version: "test"})

	var ticks int64
	d := New(Config{
		Storage: s,
		Client:  hc,
		BearerProvider: func() (string, error) { return "tok", nil },
		PollInterval:   10 * time.Millisecond,
		Now: func() time.Time {
			ticks++
			return time.Now()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)

	// Must exit with context.DeadlineExceeded, not a DB error.
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("expected context error, got: %v", err)
	}
	// Should have ticked at least twice (drainer kept looping despite errors).
	if ticks < 2 {
		t.Fatalf("expected ≥2 ticks, got %d", ticks)
	}
}
