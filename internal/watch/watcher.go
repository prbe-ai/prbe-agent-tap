package watch

import (
	"context"
	"encoding/json"
	"fmt"
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
		case <-fw.Errors:
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
		_ = fw.Add(full)
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
	fs, err := w.openFile(path)
	if err != nil {
		return err
	}
	existing, _ := CurrentLineCount(path)
	fs.lineNo = existing
	if _, err := os.Stat(path); err == nil {
		_ = fs.reader.Close()
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		info, _ := f.Stat()
		fs.reader = &Reader{path: path, f: f, offset: info.Size()}
	}
	if err := w.persistOffset(path, fs); err != nil {
		return err
	}
	w.files[path] = fs
	return nil
}

func (w *Watcher) openFile(path string) (*fileState, error) {
	r, err := OpenReader(path)
	if err != nil {
		return nil, err
	}
	sessID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	cwd := decodeProjectDir(filepath.Base(filepath.Dir(path)))
	return &fileState{
		reader: r,
		sessID: sessID,
		cwd:    cwd,
		buf: NewBuffer(BufferConfig{
			MaxLines: w.cfg.BatchMaxLines,
			MaxAge:   w.cfg.BatchMaxAge,
		}),
	}, nil
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
			_ = fw.Add(path)
		}
		return
	}
	w.mu.Lock()
	fs, ok := w.files[path]
	w.mu.Unlock()
	if !ok {
		_ = w.registerExistingFile(path)
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
	lines := fs.buf.Drain()
	if len(lines) == 0 {
		return
	}
	deviceID, _ := w.cfg.DeviceIDFunc()
	body, err := buildBatchBody(deviceID, fs.sessID, fs.cwd, fs.batchSeq, fs.lineNo, lines)
	if err != nil {
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
		return
	}
	fs.lineNo += int64(len(lines))
	fs.batchSeq++
	_ = w.persistOffset(path, fs)
}

func (w *Watcher) persistOffset(path string, fs *fileState) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return w.cfg.Storage.UpsertOffset(storage.FileOffset{
		Path: path, SessionID: fs.sessID, CWD: fs.cwd,
		LastLineNo: fs.lineNo, LastSeenAt: time.Now().Unix(),
		Inode: int64(inodeOf(info)), Size: info.Size(),
	})
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
