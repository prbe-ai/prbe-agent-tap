package storage

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for _, table := range []string{"file_offsets", "outbox", "meta", "_migrations"} {
		var name string
		row := s.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		)
		if err := row.Scan(&name); err != nil {
			t.Fatalf("missing table %q: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		s.Close()
	}
}

func TestMetaSetGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SetMeta("device_id", "abc-123"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	got, err := s.GetMeta("device_id")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got != "abc-123" {
		t.Fatalf("got %q want %q", got, "abc-123")
	}

	if err := s.SetMeta("device_id", "xyz-789"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetMeta("device_id")
	if got != "xyz-789" {
		t.Fatalf("after overwrite got %q want %q", got, "xyz-789")
	}
}

func TestGetMetaMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.GetMeta("nope")
	if err != nil {
		t.Fatalf("GetMeta missing: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestDeleteMeta(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_ = s.SetMeta("k", "v")
	if err := s.DeleteMeta("k"); err != nil {
		t.Fatalf("DeleteMeta: %v", err)
	}
	got, _ := s.GetMeta("k")
	if got != "" {
		t.Fatalf("after delete got %q want empty", got)
	}
}
