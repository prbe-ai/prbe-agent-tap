package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

// TestFlushLockedPreservesBufferOnEnqueueFailure verifies that lines are NOT
// dropped when EnqueueBatch returns an error (e.g. UNIQUE constraint violation).
func TestFlushLockedPreservesBufferOnEnqueueFailure(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-Users-foo-bar")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "test-sess-preserve"
	sessionFile := filepath.Join(projectDir, sessID+".jsonl")
	line1, _ := json.Marshal(map[string]string{"k": "v1"})
	if err := os.WriteFile(sessionFile, append(line1, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	s, _ := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	defer s.Close()

	// Pre-insert a row with (session_id, batch_seq) = (sessID, 0) so that the
	// watcher's first EnqueueBatch attempt hits a UNIQUE constraint error.
	now := time.Now().Unix()
	if err := s.EnqueueBatch(storage.OutboxRow{
		SessionID:     sessID,
		BatchSeq:      0,
		CWD:           "/Users/foo/bar",
		Body:          []byte(`{}`),
		CreatedAt:     now,
		NextAttemptAt: now,
	}); err != nil {
		t.Fatal("pre-insert failed:", err)
	}

	// Build a fileState manually so we can inspect the buffer directly
	// without running the full watcher goroutine.
	fs := &fileState{
		sessID: sessID,
		cwd:    "/Users/foo/bar",
		buf: NewBuffer(BufferConfig{
			MaxLines: 100,
			MaxAge:   time.Hour,
		}),
	}
	line2, _ := json.Marshal(map[string]string{"k": "v2"})
	fs.buf.Add(line2)

	watcher := &Watcher{
		cfg: WatcherConfig{
			Storage: s,
			DeviceIDFunc: func() (string, error) {
				return "dev1", nil
			},
		},
		files: map[string]*fileState{},
	}

	// Attempt flush; it should fail due to UNIQUE constraint.
	watcher.mu.Lock()
	watcher.flushLocked(sessionFile, fs, time.Now())
	watcher.mu.Unlock()

	// The buffer must still contain the line.
	if fs.buf.Len() != 1 {
		t.Fatalf("expected buffer to still hold 1 line after failed flush, got %d", fs.buf.Len())
	}
}

func TestWatcherResumesFromPersistedOffset(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-Users-foo-repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(projectDir, "abc-123.jsonl")

	// First two lines exist before the daemon "starts up".
	preExisting := `{"line":0}` + "\n" + `{"line":1}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(preExisting), 0o600); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "state.db")
	s, err := storage.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Pre-populate state.db as if the daemon had already shipped both lines and crashed.
	info, _ := os.Stat(sessionFile)
	if err := s.UpsertOffset(storage.FileOffset{
		Path:       sessionFile,
		SessionID:  "abc-123",
		CWD:        "/Users/foo/repo",
		LastLineNo: 2,
		LastSeenAt: 0,
		Inode:      int64(0), // exact inode set below
		Size:       info.Size(),
	}); err != nil {
		t.Fatal(err)
	}
	// Capture the actual inode and update the row.
	stat := info.Sys()
	if stat == nil {
		t.Skip("FileInfo.Sys unavailable — cannot test inode persistence on this platform")
	}
	if err := s.UpsertOffset(storage.FileOffset{
		Path:       sessionFile,
		SessionID:  "abc-123",
		CWD:        "/Users/foo/repo",
		LastLineNo: 2,
		LastSeenAt: 0,
		Inode:      int64(inodeOf(info)),
		Size:       info.Size(),
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate the daemon being down: append 3 new lines while we have no watcher running.
	f, _ := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0o600)
	for i := 2; i < 5; i++ {
		_, _ = f.WriteString(`{"line":` + strconv.Itoa(i) + `}` + "\n")
	}
	_ = f.Close()

	// Now start the watcher. It should pick up lines 2..4 (3 new lines), not skip them.
	w, err := NewWatcher(WatcherConfig{
		ProjectsRoot:  dir,
		Storage:       s,
		BatchMaxLines: 10,       // big buffer so all 3 lines flush together on idle
		BatchMaxAge:   100 * time.Millisecond,
		IdleAfter:     time.Hour,
		PollInterval:  25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.Run(ctx) }()

	// Trigger another append to fire fsnotify after the watcher is initialized.
	time.Sleep(150 * time.Millisecond)
	f2, _ := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f2.WriteString(`{"line":5}` + "\n")
	_ = f2.Close()

	// Poll the outbox for at least one row containing line_no >= 2.
	deadline := time.Now().Add(2 * time.Second)
	saw := false
	for time.Now().Before(deadline) {
		row, ok, _ := s.NextDueBatch(time.Now().Unix() + 9999)
		if ok {
			if strings.Contains(string(row.Body), `"line_no":2`) {
				saw = true
			}
			_ = s.MarkSuccess(row.ID)
		}
		if saw {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if !saw {
		t.Fatalf("expected line_no=2 to be shipped after restart, but it wasn't (lines 2..5 silently dropped)")
	}
}

func TestWatcherShipsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-Users-foo-repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(projectDir, "abc-123.jsonl")
	if err := os.WriteFile(sessionFile, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	s, _ := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	defer s.Close()

	w, err := NewWatcher(WatcherConfig{
		ProjectsRoot:  dir,
		Storage:       s,
		BatchMaxLines: 2,
		BatchMaxAge:   100 * time.Millisecond,
		IdleAfter:     time.Hour,
		PollInterval:  25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.Run(ctx) }()

	time.Sleep(150 * time.Millisecond)

	f, _ := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0o600)
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(map[string]int{"i": i})
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	deadline := time.Now().Add(2 * time.Second)
	var rows int
	for time.Now().Before(deadline) {
		row, ok, _ := s.NextDueBatch(time.Now().Unix() + 9999)
		if ok {
			rows++
			_ = s.MarkSuccess(row.ID)
			if rows >= 1 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if rows < 1 {
		t.Fatalf("expected at least 1 outbox row enqueued, got %d", rows)
	}
}
