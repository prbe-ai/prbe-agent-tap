package storage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutboxEnqueueAndNextDue(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	if err := s.EnqueueBatch(OutboxRow{
		SessionID: "sid", BatchSeq: 0, CWD: "/", Body: []byte(`{"a":1}`),
		CreatedAt: now, NextAttemptAt: now,
	}); err != nil {
		t.Fatalf("EnqueueBatch: %v", err)
	}

	row, ok, err := s.NextDueBatch(now + 1)
	if err != nil {
		t.Fatalf("NextDueBatch: %v", err)
	}
	if !ok {
		t.Fatal("expected a due row")
	}
	if row.SessionID != "sid" || row.BatchSeq != 0 {
		t.Fatalf("got %+v", row)
	}
}

func TestOutboxDedupOnSessionAndSeq(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	body := OutboxRow{SessionID: "s", BatchSeq: 0, CWD: "/", Body: []byte(`{}`), CreatedAt: now, NextAttemptAt: now}
	if err := s.EnqueueBatch(body); err != nil {
		t.Fatal(err)
	}
	err = s.EnqueueBatch(body)
	if err == nil {
		t.Fatal("expected duplicate to fail")
	}
}

func TestOutboxMarkSuccessDeletes(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	_ = s.EnqueueBatch(OutboxRow{SessionID: "s", BatchSeq: 0, CWD: "/", Body: []byte(`{}`), CreatedAt: now, NextAttemptAt: now})
	row, _, _ := s.NextDueBatch(now + 1)

	if err := s.MarkSuccess(row.ID); err != nil {
		t.Fatalf("MarkSuccess: %v", err)
	}
	_, ok, _ := s.NextDueBatch(now + 1)
	if ok {
		t.Fatal("expected no rows after success")
	}
}

func TestOutboxMarkFailureBacksOff(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	_ = s.EnqueueBatch(OutboxRow{SessionID: "s", BatchSeq: 0, CWD: "/", Body: []byte(`{}`), CreatedAt: now, NextAttemptAt: now})
	row, _, _ := s.NextDueBatch(now + 1)

	if err := s.MarkFailure(row.ID, now+60, "503 from server"); err != nil {
		t.Fatalf("MarkFailure: %v", err)
	}
	got, ok, _ := s.NextDueBatch(now + 30)
	if ok {
		t.Fatal("expected row to NOT be due at +30s")
	}
	got, ok, _ = s.NextDueBatch(now + 120)
	if !ok {
		t.Fatal("expected row to be due at +120s")
	}
	if got.AttemptCount != 1 || !strings.Contains(got.LastError, "503") {
		t.Fatalf("got %+v", got)
	}
}

func TestOutboxCapDropsOldest(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	bigBody := make([]byte, 1024)
	for i := range bigBody {
		bigBody[i] = 'x'
	}
	for i := 0; i < 20; i++ {
		_ = s.EnqueueBatch(OutboxRow{
			SessionID: "s", BatchSeq: int64(i), CWD: "/", Body: bigBody,
			CreatedAt: now, NextAttemptAt: now,
		})
	}

	dropped, err := s.EnforceOutboxCap(10 * 1024)
	if err != nil {
		t.Fatalf("EnforceOutboxCap: %v", err)
	}
	if dropped < 8 {
		t.Fatalf("expected at least 8 rows dropped, got %d", dropped)
	}

	row, ok, _ := s.NextDueBatch(now + 1)
	if !ok {
		t.Fatal("expected at least one remaining row")
	}
	if row.BatchSeq < int64(dropped) {
		t.Fatalf("expected oldest dropped first; remaining row has batch_seq=%d, dropped=%d", row.BatchSeq, dropped)
	}
}
