package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

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
