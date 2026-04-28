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
