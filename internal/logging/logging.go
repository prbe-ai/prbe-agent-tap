package logging

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"gopkg.in/natefinch/lumberjack.v2"
)

// New configures the default logger to write JSON to stderr (WARN+) and to a rotating file (all levels).
// Returns the underlying io.Closer for the rotation file.
func New() (io.Closer, error) {
	stateDir, err := creds.StateDir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	rot := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "agent-tap.log"),
		MaxSize:    10,
		MaxBackups: 5,
		Compress:   true,
	}
	multi := io.MultiWriter(rot, levelFilter{w: os.Stderr, min: slog.LevelWarn})
	slog.SetDefault(slog.New(handler(multi)))
	return rot, nil
}

// NewWithWriter is a test helper.
func NewWithWriter(w io.Writer) *slog.Logger {
	return slog.New(handler(w))
}

func handler(w io.Writer) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				return slog.String(a.Key, creds.RedactField(a.Key, a.Value.String()))
			}
			return a
		},
	})
}

// levelFilter writes each line to w only when its slog "level" field is >= min.
// Each Write call is expected to contain one complete JSON log entry (slog's
// JSONHandler emits one line per record).
type levelFilter struct {
	w   io.Writer
	min slog.Level
}

func (lf levelFilter) Write(p []byte) (int, error) {
	level, ok := parseLevel(p)
	if !ok || level < lf.min {
		// Pretend we wrote everything so MultiWriter doesn't error out.
		return len(p), nil
	}
	return lf.w.Write(p)
}

// parseLevel finds the "level" string in a slog JSON record and maps it to slog.Level.
// Returns (level, true) on success, (0, false) on any parse failure.
func parseLevel(p []byte) (slog.Level, bool) {
	var rec struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal(p, &rec); err != nil {
		return 0, false
	}
	switch rec.Level {
	case "DEBUG":
		return slog.LevelDebug, true
	case "INFO":
		return slog.LevelInfo, true
	case "WARN":
		return slog.LevelWarn, true
	case "ERROR":
		return slog.LevelError, true
	}
	return 0, false
}
