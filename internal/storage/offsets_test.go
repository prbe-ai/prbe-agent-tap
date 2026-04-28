package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileOffsetsUpsertAndGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	row := FileOffset{
		Path: "/tmp/x.jsonl", SessionID: "sid", CWD: "/repo",
		LastLineNo: 0, LastSeenAt: now, Inode: 99, Size: 1000,
	}
	if err := s.UpsertOffset(row); err != nil {
		t.Fatalf("UpsertOffset: %v", err)
	}

	got, ok, err := s.GetOffset("/tmp/x.jsonl")
	if err != nil {
		t.Fatalf("GetOffset: %v", err)
	}
	if !ok {
		t.Fatal("expected row to exist")
	}
	if got.LastLineNo != 0 || got.Size != 1000 || got.SessionID != "sid" {
		t.Fatalf("got %+v want %+v", got, row)
	}

	row.LastLineNo = 42
	row.Size = 5000
	_ = s.UpsertOffset(row)
	got, _, _ = s.GetOffset("/tmp/x.jsonl")
	if got.LastLineNo != 42 || got.Size != 5000 {
		t.Fatalf("after update got %+v", got)
	}
}

func TestGetOffsetMissingReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, ok, err := s.GetOffset("/missing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for missing path")
	}
}

func TestListOffsetsAndDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		_ = s.UpsertOffset(FileOffset{
			Path: filepath.Join("/p", string(rune('a'+i))+".jsonl"),
			SessionID: "s", CWD: "/", LastLineNo: 0, LastSeenAt: 0, Size: 0,
		})
	}
	all, err := s.ListOffsets()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d rows want 3", len(all))
	}
	if err := s.DeleteOffset(all[0].Path); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListOffsets()
	if len(all) != 2 {
		t.Fatalf("after delete got %d rows want 2", len(all))
	}
}
