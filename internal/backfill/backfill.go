package backfill

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
	"github.com/prbe-ai/prbe-agent-tap/internal/watch"
)

const maxBackfillWindow = 365 * 24 * time.Hour

// Enumerate returns JSONL paths under root with mtime >= since (clamped to 365d back).
// Sorted ascending by mtime. Symlinks are skipped.
func Enumerate(root string, since time.Time) ([]string, error) {
	earliest := time.Now().Add(-maxBackfillWindow)
	if since.Before(earliest) {
		since = earliest
	}
	type entry struct {
		path  string
		mtime time.Time
	}
	var hits []entry

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, lerr := os.Lstat(path)
		if lerr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info.ModTime().Before(since) {
			return nil
		}
		hits = append(hits, entry{path: path, mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mtime.Before(hits[j].mtime) })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.path
	}
	return out, nil
}

type Config struct {
	Root          string
	Since         time.Time
	Storage       *storage.Storage
	BatchMaxLines int
	TargetDepth   int
	LowWater      int
	DeviceIDFunc  func() (string, error)
	// DrainObserver is called periodically while the outbox is above LowWater.
	// Return true once the test or external drainer made progress.
	DrainObserver func() bool
}

// Run enqueues lines from sessions into the outbox, pacing on outbox depth.
func Run(ctx context.Context, cfg Config) error {
	if cfg.BatchMaxLines == 0 {
		cfg.BatchMaxLines = 10
	}
	if cfg.TargetDepth == 0 {
		cfg.TargetDepth = 50
	}
	if cfg.LowWater == 0 {
		cfg.LowWater = 25
	}
	files, err := Enumerate(cfg.Root, cfg.Since)
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}
	deviceID, _ := cfg.DeviceIDFunc()
	// nextSeq tracks the next batch_seq to use per session_id, seeded from
	// the storage so that re-runs and concurrent `watch` processes don't collide
	// on the (session_id, batch_seq) UNIQUE constraint.
	nextSeq := map[string]int64{}
	for _, path := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		buf := make([]byte, 0, 64*1024)
		scratch := make([]byte, 32*1024)
		for {
			n, rerr := f.Read(scratch)
			if n > 0 {
				buf = append(buf, scratch[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		_ = f.Close()
		lines, _, _ := watch.SplitLines(buf)
		sessID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		cwd := strings.ReplaceAll(filepath.Base(filepath.Dir(path)), "-", "/")

		// Seed nextSeq for this session from the current DB state.
		if _, seen := nextSeq[sessID]; !seen {
			max, err := cfg.Storage.MaxBatchSeq(sessID)
			if err != nil {
				slog.Warn("backfill: MaxBatchSeq failed", "session_id", sessID, "error", err.Error())
				max = -1
			}
			nextSeq[sessID] = max + 1
		}

		var lineNo int64
		for i := 0; i < len(lines); i += cfg.BatchMaxLines {
			end := i + cfg.BatchMaxLines
			if end > len(lines) {
				end = len(lines)
			}
			batch := lines[i:end]
			batchSeq := nextSeq[sessID]
			body, err := buildBackfillBody(deviceID, sessID, cwd, batchSeq, lineNo, batch)
			if err != nil {
				continue
			}
			now := time.Now().Unix()
			if err := cfg.Storage.EnqueueBatch(storage.OutboxRow{
				SessionID: sessID, BatchSeq: batchSeq, CWD: cwd, Body: body,
				CreatedAt: now, NextAttemptAt: now,
			}); err != nil {
				slog.Warn("backfill: EnqueueBatch failed", "session_id", sessID, "batch_seq", batchSeq, "error", err.Error())
				continue
			}
			nextSeq[sessID]++
			lineNo += int64(len(batch))

			for {
				rows, err := cfg.Storage.OutboxRowCount()
				if err != nil {
					break
				}
				if int(rows) <= cfg.TargetDepth {
					break
				}
				if cfg.DrainObserver != nil && cfg.DrainObserver() {
					continue
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
	}
	return nil
}

func buildBackfillBody(deviceID, sessionID, cwd string, batchSeq, baseLineNo int64, lines [][]byte) ([]byte, error) {
	type event struct {
		LineNo int64           `json:"line_no"`
		Raw    json.RawMessage `json:"raw"`
	}
	events := make([]event, 0, len(lines))
	for i, l := range lines {
		events = append(events, event{LineNo: baseLineNo + int64(i), Raw: l})
	}
	body := struct {
		DeviceID  string  `json:"device_id"`
		SessionID string  `json:"session_id"`
		BatchSeq  int64   `json:"batch_seq"`
		CWD       string  `json:"cwd"`
		Events    []event `json:"events"`
	}{deviceID, sessionID, batchSeq, cwd, events}
	return json.Marshal(body)
}
