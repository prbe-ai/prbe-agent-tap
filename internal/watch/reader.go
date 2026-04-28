package watch

import (
	"io"
	"os"
)

// Reader tracks a single JSONL file's read offset across appends and detects truncation.
type Reader struct {
	path   string
	f      *os.File
	offset int64
}

func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Reader{path: path, f: f}, nil
}

func (r *Reader) Close() error { return r.f.Close() }

// ReadNew reads bytes from the current offset to EOF, returning complete lines.
// Detects truncation (size shrinking) by reopening from offset 0.
// Partial trailing lines are kept for the next call.
func (r *Reader) ReadNew() ([][]byte, error) {
	info, err := r.f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < r.offset {
		_ = r.f.Close()
		f2, err := os.Open(r.path)
		if err != nil {
			return nil, err
		}
		r.f = f2
		r.offset = 0
	}
	if _, err := r.f.Seek(r.offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(r.f)
	if err != nil {
		return nil, err
	}
	lines, partial, err := SplitLines(buf)
	if err != nil {
		return nil, err
	}
	r.offset += int64(len(buf) - partial)
	return lines, nil
}

// CurrentLineCount returns the number of complete lines in the file (used at pair time
// to skip historical content during `watch`).
func CurrentLineCount(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	lines, _, _ := SplitLines(buf)
	return int64(len(lines)), nil
}
