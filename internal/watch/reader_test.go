package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, `{"a":1}`+"\n"+`{"b":2}`+"\n")

	r, err := OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer r.Close()

	lines, err := r.ReadNew()
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines want 2", len(lines))
	}
	writeFile(t, path, `{"a":1}`+"\n"+`{"b":2}`+"\n"+`{"c":3}`+"\n")
	lines, err = r.ReadNew()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d new lines want 1", len(lines))
	}
}

func TestReadDetectsTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, `{"a":1}`+"\n"+`{"b":2}`+"\n")

	r, _ := OpenReader(path)
	defer r.Close()
	_, _ = r.ReadNew()

	writeFile(t, path, `{"z":99}`+"\n")
	lines, err := r.ReadNew()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1 (truncation should reset offset)", len(lines))
	}
}

// TestOpenReaderAtEndCapturesLinesAppendedDuringOpen verifies that lines
// appended to a file after OpenReaderAtEnd has read the existing content are
// still picked up by the first ReadNew call (i.e. no lines are silently lost).
func TestOpenReaderAtEndCapturesLinesAppendedDuringOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// Write two pre-existing lines.
	existing := `{"n":1}` + "\n" + `{"n":2}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	reader, lineCount, err := OpenReaderAtEnd(path)
	if err != nil {
		t.Fatalf("OpenReaderAtEnd: %v", err)
	}
	defer reader.Close()

	if lineCount != 2 {
		t.Fatalf("expected lineCount=2, got %d", lineCount)
	}

	// Append a new line AFTER OpenReaderAtEnd has already read the file.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"n":3}` + "\n")
	_ = f.Close()

	// ReadNew must return exactly the one newly-appended line.
	lines, err := reader.ReadNew()
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 new line, got %d", len(lines))
	}
}

func TestReadSkipsPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	writeFile(t, path, `{"a":1}`+"\n"+`{"b`)

	r, _ := OpenReader(path)
	defer r.Close()
	lines, err := r.ReadNew()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1", len(lines))
	}
	writeFile(t, path, `{"a":1}`+"\n"+`{"b":2}`+"\n")
	lines, _ = r.ReadNew()
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1 after completing partial", len(lines))
	}
}
