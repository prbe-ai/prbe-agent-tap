package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type WatcherConfig struct {
	ProjectsRoot  string
	Storage       *storage.Storage
	BatchMaxLines int
	BatchMaxAge   time.Duration
	IdleAfter     time.Duration
	PollInterval  time.Duration
	DeviceIDFunc  func() (string, error)
}

type Watcher struct {
	cfg   WatcherConfig
	mu    sync.Mutex
	files map[string]*fileState
}

type fileState struct {
	reader   *Reader
	buf      *Buffer
	sessID   string
	cwd      string
	lineNo   int64
	batchSeq int64
}

func NewWatcher(cfg WatcherConfig) (*Watcher, error) {
	if cfg.BatchMaxLines == 0 {
		cfg.BatchMaxLines = 10
	}
	if cfg.BatchMaxAge == 0 {
		cfg.BatchMaxAge = 5 * time.Second
	}
	if cfg.IdleAfter == 0 {
		cfg.IdleAfter = 30 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.DeviceIDFunc == nil {
		cfg.DeviceIDFunc = func() (string, error) {
			return cfg.Storage.GetMeta("device_id")
		}
	}
	return &Watcher{cfg: cfg, files: map[string]*fileState{}}, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}
	defer fw.Close()

	if err := w.bootstrap(fw); err != nil {
		return err
	}

	t := time.NewTicker(w.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			w.flushAll(time.Now())
			return ctx.Err()
		case ev := <-fw.Events:
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.handleEvent(ev.Name, fw)
			}
		case err := <-fw.Errors:
			if err != nil {
				slog.Warn("watch: fsnotify error", "error", err.Error())
			}
		case <-t.C:
			w.tick()
		}
	}
}

func (w *Watcher) bootstrap(fw *fsnotify.Watcher) error {
	root := w.cfg.ProjectsRoot
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := fw.Add(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(root, entry.Name())
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			continue
		}
		if err := fw.Add(full); err != nil {
			slog.Warn("watch: fsnotify Add failed", "path", full, "error", err.Error())
		}
		files, _ := os.ReadDir(full)
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(full, f.Name())
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if err := w.registerExistingFile(path); err != nil {
				continue
			}
		}
	}
	return nil
}

func (w *Watcher) registerExistingFile(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.files[path]; ok {
		return nil
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		return statErr
	}
	currentInode := int64(inodeOf(info))
	currentSize := info.Size()

	stored, hasStored, err := w.cfg.Storage.GetOffset(path)
	if err != nil {
		return err
	}

	resume := hasStored && stored.Inode == currentInode && currentSize >= stored.Size

	var fs *fileState
	if resume {
		// Resume from where the last persist left off.
		reader, err := OpenReaderAtOffset(path, stored.Size)
		if err != nil {
			return err
		}
		sessID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		cwd := decodeProjectDir(filepath.Base(filepath.Dir(path)))
		fs = &fileState{
			reader: reader,
			sessID: sessID,
			cwd:    cwd,
			lineNo: stored.LastLineNo,
			buf: NewBuffer(BufferConfig{
				MaxLines: w.cfg.BatchMaxLines,
				MaxAge:   w.cfg.BatchMaxAge,
			}),
		}
	} else {
		// New file (no stored row, or inode/size mismatch indicating rotation/truncation).
		// Skip historical content: lineNo = current line count, reader at EOF.
		reader, lineCount, err := OpenReaderAtEnd(path)
		if err != nil {
			return err
		}
		sessID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		cwd := decodeProjectDir(filepath.Base(filepath.Dir(path)))
		fs = &fileState{
			reader: reader,
			sessID: sessID,
			cwd:    cwd,
			lineNo: lineCount,
			buf: NewBuffer(BufferConfig{
				MaxLines: w.cfg.BatchMaxLines,
				MaxAge:   w.cfg.BatchMaxAge,
			}),
		}
	}

	if err := w.persistOffset(path, fs); err != nil {
		_ = fs.reader.Close()
		return err
	}
	w.files[path] = fs
	return nil
}

func decodeProjectDir(name string) string {
	if !strings.HasPrefix(name, "-") {
		return name
	}
	return strings.ReplaceAll(name, "-", "/")
}

func (w *Watcher) handleEvent(path string, fw *fsnotify.Watcher) {
	if !strings.HasSuffix(path, ".jsonl") {
		info, err := os.Lstat(path)
		if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := fw.Add(path); err != nil {
				slog.Warn("watch: fsnotify Add failed", "path", path, "error", err.Error())
			}
		}
		return
	}
	w.mu.Lock()
	fs, ok := w.files[path]
	w.mu.Unlock()
	if !ok {
		if err := w.registerExistingFile(path); err != nil {
			slog.Warn("watch: registerExistingFile failed", "path", path, "error", err.Error())
		}
		w.mu.Lock()
		fs = w.files[path]
		w.mu.Unlock()
	}
	if fs == nil {
		return
	}
	w.readAndBuffer(path, fs)
	w.maybeFlush(path, fs)
}

func (w *Watcher) tick() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for path, fs := range w.files {
		if fs.buf.Len() == 0 {
			continue
		}
		if fs.buf.ShouldFlush() || now.Sub(fs.buf.firstAt) >= w.cfg.IdleAfter {
			w.flushLocked(path, fs, now)
		}
	}
}

func (w *Watcher) readAndBuffer(path string, fs *fileState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines, err := fs.reader.ReadNew()
	if err != nil {
		slog.Warn("watch: read failed", "path", path, "error", err.Error())
		return
	}
	for _, line := range lines {
		if err := ValidateJSON(line); err != nil {
			fs.lineNo++
			continue
		}
		fs.buf.Add(line)
	}
}

func (w *Watcher) maybeFlush(path string, fs *fileState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fs.buf.ShouldFlush() {
		w.flushLocked(path, fs, time.Now())
	}
}

func (w *Watcher) flushAll(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, fs := range w.files {
		if fs.buf.Len() > 0 {
			w.flushLocked(path, fs, now)
		}
	}
}

func (w *Watcher) flushLocked(path string, fs *fileState, now time.Time) {
	lines := fs.buf.Peek()
	if len(lines) == 0 {
		return
	}
	deviceID, _ := w.cfg.DeviceIDFunc()
	body, err := buildBatchBody(deviceID, fs.sessID, fs.cwd, fs.batchSeq, fs.lineNo, lines)
	if err != nil {
		slog.Warn("watch: buildBatchBody failed", "session", fs.sessID, "error", err.Error())
		return
	}
	row := storage.OutboxRow{
		SessionID:     fs.sessID,
		BatchSeq:      fs.batchSeq,
		CWD:           fs.cwd,
		Body:          body,
		CreatedAt:     now.Unix(),
		NextAttemptAt: now.Unix(),
	}
	if err := w.cfg.Storage.EnqueueBatch(row); err != nil {
		slog.Warn("watch: EnqueueBatch failed", "session", fs.sessID, "batch_seq", fs.batchSeq, "error", err.Error())
		return
	}
	// Only drain the buffer after a successful enqueue so that a failure
	// (disk full, UNIQUE constraint, etc.) preserves the lines for retry.
	fs.buf.Drain()
	fs.lineNo += int64(len(lines))
	fs.batchSeq++
	_ = w.persistOffset(path, fs)
}

func (w *Watcher) persistOffset(path string, fs *fileState) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := w.cfg.Storage.UpsertOffset(storage.FileOffset{
		Path: path, SessionID: fs.sessID, CWD: fs.cwd,
		LastLineNo: fs.lineNo, LastSeenAt: time.Now().Unix(),
		Inode: int64(inodeOf(info)), Size: info.Size(),
	}); err != nil {
		slog.Warn("watch: UpsertOffset failed", "path", path, "error", err.Error())
		return err
	}
	return nil
}

func buildBatchBody(deviceID, sessionID, cwd string, batchSeq, baseLineNo int64, lines [][]byte) ([]byte, error) {
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
