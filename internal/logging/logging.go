package logging

import (
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

// levelFilter is a tiny io.Writer that only forwards lines whose JSON "level" >= min.
type levelFilter struct {
	w   io.Writer
	min slog.Level
}

func (lf levelFilter) Write(p []byte) (int, error) {
	return lf.w.Write(p)
}
