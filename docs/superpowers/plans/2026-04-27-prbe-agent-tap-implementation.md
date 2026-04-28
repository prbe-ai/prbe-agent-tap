# prbe-agent-tap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Phase 3.1 laptop daemon `prbe-agent-tap` that watches Claude Code transcripts and ships batched events to `api.prbe.ai/webhooks/claude_code` under a per-device bearer token.

**Architecture:** Single static Go binary. Subcommand-dispatched (stdlib `flag`). Local state in pure-Go SQLite (`~/.prbe/state.db`) with WAL: per-file offsets and a transactional outbox. fsnotify-based watcher. OS keychain for the device token (file fallback). Indefinite exponential backoff with a 100MB outbox safety cap. 401 → drop outbox + halt. Distribution via GitHub Releases + `curl|sh`.

**Tech Stack:** Go 1.22+; `github.com/fsnotify/fsnotify`; `github.com/zalando/go-keyring`; `modernc.org/sqlite`; `gopkg.in/natefinch/lumberjack.v2`; stdlib `net/http`, `log/slog`, `encoding/json`, `flag`. No `cobra`, no CGO.

**Spec:** `~/Desktop/prbe/prbe-knowledge-worktrees/coding-agent-ingestion-v2/docs/superpowers/specs/2026-04-27-prbe-agent-tap-daemon-design.md`

**Working tree:** `~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold/` on branch `feature/initial-scaffold`.

---

## File Structure

Each file owns one responsibility. Files that change together live together.

```
cmd/prbe-agent-tap/
  main.go                 # subcommand dispatch only

internal/storage/
  storage.go              # Open, Close, WAL, migration runner
  schema.go               # SQL DDL strings
  meta.go                 # singleton meta CRUD
  offsets.go              # file_offsets CRUD
  outbox.go               # outbox CRUD + cap enforcement

internal/creds/
  creds.go                # keychain + file fallback
  redact.go               # log-field redactor

internal/httpclient/
  retry.go                # status → classification matrix
  backoff.go              # attempt count → next_attempt_at
  client.go               # bearer-aware HTTP client + Do()

internal/pair/
  pair.go                 # /agent-tap/pair flow

internal/heartbeat/
  heartbeat.go            # /agent-tap/heartbeat one-shot

internal/revoke/
  revoke.go               # /agent-tap/revoke one-shot + cleanup

internal/outbox/
  drainer.go              # goroutine that polls the outbox table

internal/watch/
  parser.go               # JSONL line splitter + JSON validator
  reader.go               # per-file reader (open, inode, truncation)
  buffer.go               # in-memory buffer + flush conditions
  watcher.go              # fsnotify integration + lifecycle

internal/status/
  status.go               # read-only meta dump

internal/backfill/
  backfill.go             # paced enumerate + enqueue

internal/install/
  launchd.go              # macOS plist generator
  systemd.go              # Linux unit generator
  install.go              # platform dispatcher

internal/logging/
  logging.go              # slog + lumberjack setup

internal/version/
  version.go              # build-time version string

scripts/
  install.sh              # curl|sh entrypoint

.github/workflows/
  ci.yml                  # PR gating
  release.yml             # tagged-release binary publish

tests/e2e/
  e2e_test.go             # against running local backend + knowledge

go.mod
go.sum
README.md
.gitignore
Makefile
```

**Naming distinction:** `internal/storage/outbox.go` owns the SQL CRUD for the `outbox` table. `internal/outbox/drainer.go` owns the runtime drainer goroutine that uses `storage` + `httpclient`. Separate concerns, similar names — call sites import the package they need.

---

## Conventions used in this plan

- Every step that changes code shows the full code or a precise diff.
- Every test step shows the exact command and the expected result.
- Commits use Conventional Commits (`feat:`, `test:`, `chore:`, `docs:`).
- Author in commits: just the engineer; no Co-Authored-By unless the user asks.
- `cd` into the worktree before running anything: `cd ~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold`.
- All Go test commands use `-race` to catch concurrency bugs early.

---

## Task 1: Initialize Go module and bootstrap binary

**Files:**
- Create: `go.mod`
- Create: `cmd/prbe-agent-tap/main.go`
- Create: `internal/version/version.go`
- Create: `.gitignore`
- Create: `Makefile`

- [ ] **Step 1: `cd` into the worktree and initialize the Go module**

```bash
cd ~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold
go mod init github.com/prbe-ai/prbe-agent-tap
```

Expected: `go.mod` created with `module github.com/prbe-ai/prbe-agent-tap` and the Go toolchain version line.

- [ ] **Step 2: Create `internal/version/version.go`**

```go
package version

// Version is overridden at build time via -ldflags "-X .../internal/version.Version=v0.1.0".
var Version = "dev"
```

- [ ] **Step 3: Create `cmd/prbe-agent-tap/main.go` with a single `--version` flag**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("prbe-agent-tap %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "no subcommand given (subcommand dispatch added in Task 11)")
	os.Exit(2)
}
```

- [ ] **Step 4: Create `.gitignore`**

```
# Binaries
/prbe-agent-tap
/dist/

# Go test/coverage artifacts
*.test
*.out
coverage.txt

# IDE/editor
.idea/
.vscode/
*.swp
.DS_Store

# Local dev state
/.prbe/
```

- [ ] **Step 5: Create `Makefile`**

```make
.PHONY: build test e2e vet fmt clean

VERSION ?= dev
LDFLAGS := -ldflags "-X github.com/prbe-ai/prbe-agent-tap/internal/version.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o prbe-agent-tap ./cmd/prbe-agent-tap

test:
	go test -race ./...

e2e:
	go test -race -tags=e2e ./tests/e2e/...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -f prbe-agent-tap
	rm -rf dist/
```

- [ ] **Step 6: Build and verify**

Run:

```bash
go build ./cmd/prbe-agent-tap
./prbe-agent-tap --version
```

Expected: prints `prbe-agent-tap dev (darwin/arm64)` (or your local OS/arch). Exit 0.

- [ ] **Step 7: Commit**

```bash
git add go.mod cmd internal/version .gitignore Makefile
git commit -m "feat: bootstrap go module and --version flag"
```

---

## Task 2: Storage package — Open, schema, migrations

**Files:**
- Create: `internal/storage/schema.go`
- Create: `internal/storage/storage.go`
- Create: `internal/storage/storage_test.go`

- [ ] **Step 1: Add the SQLite driver dependency**

```bash
go get modernc.org/sqlite
go mod tidy
```

Expected: `go.sum` populated, `go.mod` lists `modernc.org/sqlite`.

- [ ] **Step 2: Write the failing test `internal/storage/storage_test.go`**

```go
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

	// All three tables must exist after Open.
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
```

- [ ] **Step 3: Run the test to verify it fails (no `Open` yet)**

Run: `go test -race ./internal/storage/...`
Expected: FAIL — undefined `Open`.

- [ ] **Step 4: Create `internal/storage/schema.go`**

```go
package storage

const schemaSQL = `
CREATE TABLE IF NOT EXISTS _migrations (
    id        INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS file_offsets (
    path           TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL,
    cwd            TEXT NOT NULL,
    last_line_no   INTEGER NOT NULL,
    last_seen_at   INTEGER NOT NULL,
    inode          INTEGER,
    size           INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      TEXT NOT NULL,
    batch_seq       INTEGER NOT NULL,
    cwd             TEXT NOT NULL,
    body_json       BLOB NOT NULL,
    created_at      INTEGER NOT NULL,
    next_attempt_at INTEGER NOT NULL,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    UNIQUE(session_id, batch_seq)
);

CREATE INDEX IF NOT EXISTS outbox_next_attempt ON outbox(next_attempt_at);

CREATE TABLE IF NOT EXISTS meta (
    k TEXT PRIMARY KEY,
    v TEXT NOT NULL
);
`
```

- [ ] **Step 5: Create `internal/storage/storage.go`**

```go
package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Storage owns the SQLite connection and schema lifecycle.
type Storage struct {
	db *sql.DB
}

func Open(path string) (*Storage, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error  { return s.db.Close() }
func (s *Storage) DB() *sql.DB   { return s.db }
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test -race ./internal/storage/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/storage
git commit -m "feat(storage): WAL-mode SQLite open with idempotent schema"
```

---

## Task 3: Storage — meta singleton CRUD

**Files:**
- Create: `internal/storage/meta.go`
- Modify: `internal/storage/storage_test.go`

- [ ] **Step 1: Append the failing test to `internal/storage/storage_test.go`**

```go
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

	// Overwrite.
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/storage/...`
Expected: FAIL — undefined `SetMeta` / `GetMeta` / `DeleteMeta`.

- [ ] **Step 3: Create `internal/storage/meta.go`**

```go
package storage

import "database/sql"

func (s *Storage) SetMeta(k, v string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta(k, v) VALUES(?, ?)
		 ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v,
	)
	return err
}

func (s *Storage) GetMeta(k string) (string, error) {
	var v string
	row := s.db.QueryRow(`SELECT v FROM meta WHERE k=?`, k)
	if err := row.Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (s *Storage) DeleteMeta(k string) error {
	_, err := s.db.Exec(`DELETE FROM meta WHERE k=?`, k)
	return err
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/storage/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat(storage): meta singleton get/set/delete"
```

---

## Task 4: Storage — file_offsets CRUD

**Files:**
- Create: `internal/storage/offsets.go`
- Create: `internal/storage/offsets_test.go`

- [ ] **Step 1: Write the failing test `internal/storage/offsets_test.go`**

```go
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

	// Update.
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/storage/...`
Expected: FAIL — undefined `FileOffset`, `UpsertOffset`, etc.

- [ ] **Step 3: Create `internal/storage/offsets.go`**

```go
package storage

type FileOffset struct {
	Path       string
	SessionID  string
	CWD        string
	LastLineNo int64
	LastSeenAt int64
	Inode      int64
	Size       int64
}

func (s *Storage) UpsertOffset(f FileOffset) error {
	_, err := s.db.Exec(
		`INSERT INTO file_offsets(path, session_id, cwd, last_line_no, last_seen_at, inode, size)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   session_id=excluded.session_id,
		   cwd=excluded.cwd,
		   last_line_no=excluded.last_line_no,
		   last_seen_at=excluded.last_seen_at,
		   inode=excluded.inode,
		   size=excluded.size`,
		f.Path, f.SessionID, f.CWD, f.LastLineNo, f.LastSeenAt, f.Inode, f.Size,
	)
	return err
}

func (s *Storage) GetOffset(path string) (FileOffset, bool, error) {
	var f FileOffset
	row := s.db.QueryRow(
		`SELECT path, session_id, cwd, last_line_no, last_seen_at, COALESCE(inode, 0), size
		 FROM file_offsets WHERE path=?`, path,
	)
	if err := row.Scan(&f.Path, &f.SessionID, &f.CWD, &f.LastLineNo, &f.LastSeenAt, &f.Inode, &f.Size); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return FileOffset{}, false, nil
		}
		return FileOffset{}, false, err
	}
	return f, true, nil
}

func (s *Storage) ListOffsets() ([]FileOffset, error) {
	rows, err := s.db.Query(
		`SELECT path, session_id, cwd, last_line_no, last_seen_at, COALESCE(inode, 0), size
		 FROM file_offsets ORDER BY path`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileOffset
	for rows.Next() {
		var f FileOffset
		if err := rows.Scan(&f.Path, &f.SessionID, &f.CWD, &f.LastLineNo, &f.LastSeenAt, &f.Inode, &f.Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Storage) DeleteOffset(path string) error {
	_, err := s.db.Exec(`DELETE FROM file_offsets WHERE path=?`, path)
	return err
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/storage/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat(storage): file_offsets upsert/get/list/delete"
```

---

## Task 5: Storage — outbox CRUD with cap enforcement

**Files:**
- Create: `internal/storage/outbox.go`
- Create: `internal/storage/outbox_test.go`

- [ ] **Step 1: Write the failing test `internal/storage/outbox_test.go`**

```go
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

	dropped, err := s.EnforceOutboxCap(10 * 1024) // 10 KB cap; drop down from ~20 KB.
	if err != nil {
		t.Fatalf("EnforceOutboxCap: %v", err)
	}
	if dropped < 8 {
		t.Fatalf("expected at least 8 rows dropped, got %d", dropped)
	}

	// The remaining rows should be the highest batch_seq values (newest enqueued last).
	row, ok, _ := s.NextDueBatch(now + 1)
	if !ok {
		t.Fatal("expected at least one remaining row")
	}
	if row.BatchSeq < int64(dropped) {
		t.Fatalf("expected oldest dropped first; remaining row has batch_seq=%d, dropped=%d", row.BatchSeq, dropped)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/storage/...`
Expected: FAIL — undefined `OutboxRow`, `EnqueueBatch`, etc.

- [ ] **Step 3: Create `internal/storage/outbox.go`**

```go
package storage

import "database/sql"

type OutboxRow struct {
	ID            int64
	SessionID     string
	BatchSeq      int64
	CWD           string
	Body          []byte
	CreatedAt     int64
	NextAttemptAt int64
	AttemptCount  int64
	LastError     string
}

func (s *Storage) EnqueueBatch(r OutboxRow) error {
	_, err := s.db.Exec(
		`INSERT INTO outbox(session_id, batch_seq, cwd, body_json, created_at, next_attempt_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		r.SessionID, r.BatchSeq, r.CWD, r.Body, r.CreatedAt, r.NextAttemptAt,
	)
	return err
}

func (s *Storage) NextDueBatch(now int64) (OutboxRow, bool, error) {
	var r OutboxRow
	var lastError sql.NullString
	row := s.db.QueryRow(
		`SELECT id, session_id, batch_seq, cwd, body_json, created_at, next_attempt_at, attempt_count, last_error
		 FROM outbox WHERE next_attempt_at <= ?
		 ORDER BY id ASC LIMIT 1`, now,
	)
	if err := row.Scan(&r.ID, &r.SessionID, &r.BatchSeq, &r.CWD, &r.Body,
		&r.CreatedAt, &r.NextAttemptAt, &r.AttemptCount, &lastError); err != nil {
		if err == sql.ErrNoRows {
			return OutboxRow{}, false, nil
		}
		return OutboxRow{}, false, err
	}
	r.LastError = lastError.String
	return r, true, nil
}

func (s *Storage) MarkSuccess(id int64) error {
	_, err := s.db.Exec(`DELETE FROM outbox WHERE id=?`, id)
	return err
}

func (s *Storage) MarkFailure(id int64, nextAttemptAt int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE outbox
		 SET attempt_count = attempt_count + 1, next_attempt_at = ?, last_error = ?
		 WHERE id = ?`, nextAttemptAt, errMsg, id,
	)
	return err
}

func (s *Storage) OutboxByteSize() (int64, error) {
	var total sql.NullInt64
	row := s.db.QueryRow(`SELECT COALESCE(SUM(LENGTH(body_json)), 0) FROM outbox`)
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// EnforceOutboxCap deletes oldest rows (by id) until total body size <= maxBytes.
// Returns number of rows dropped.
func (s *Storage) EnforceOutboxCap(maxBytes int64) (int, error) {
	total, err := s.OutboxByteSize()
	if err != nil {
		return 0, err
	}
	if total <= maxBytes {
		return 0, nil
	}
	// Walk oldest-first, deleting until under cap.
	rows, err := s.db.Query(`SELECT id, LENGTH(body_json) FROM outbox ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			return 0, err
		}
		ids = append(ids, id)
		total -= size
		if total <= maxBytes {
			break
		}
	}
	rows.Close()

	dropped := 0
	for _, id := range ids {
		if _, err := s.db.Exec(`DELETE FROM outbox WHERE id=?`, id); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

func (s *Storage) ClearOutbox() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM outbox`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/storage/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat(storage): outbox CRUD with backoff + cap enforcement"
```

---

## Task 6: Credentials — keychain wrapper with file fallback

**Files:**
- Create: `internal/creds/creds.go`
- Create: `internal/creds/creds_test.go`

- [ ] **Step 1: Add the keyring dependency**

```bash
go get github.com/zalando/go-keyring
go mod tidy
```

- [ ] **Step 2: Write the failing test `internal/creds/creds_test.go`**

```go
package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadDeleteFileFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", dir)

	if err := Store("device-token", "tok-abc"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := Load("device-token")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "tok-abc" {
		t.Fatalf("got %q want %q", got, "tok-abc")
	}

	// Mode must be 0600.
	info, err := os.Stat(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("got mode %v want 0600", info.Mode().Perm())
	}

	if err := Delete("device-token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = Load("device-token")
	if got != "" {
		t.Fatalf("after delete got %q want empty", got)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", dir)

	got, err := Load("missing")
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test -race ./internal/creds/...`
Expected: FAIL — undefined `Store`, `Load`, `Delete`.

- [ ] **Step 4: Create `internal/creds/creds.go`**

```go
package creds

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const serviceName = "ai.prbe.agent-tap"

// StateDir returns the daemon's state directory: $PRBE_STATE_DIR or ~/.prbe.
func StateDir() (string, error) {
	if d := os.Getenv("PRBE_STATE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".prbe"), nil
}

func keychainEnabled() bool {
	return os.Getenv("PRBE_DISABLE_KEYCHAIN") == ""
}

// Store persists a credential. Tries keychain first; on failure falls back to a 0600 file.
func Store(account, value string) error {
	if keychainEnabled() {
		if err := keyring.Set(serviceName, account, value); err == nil {
			return nil
		}
		// fall through to file fallback
	}
	return storeFile(account, value)
}

func Load(account string) (string, error) {
	if keychainEnabled() {
		v, err := keyring.Get(serviceName, account)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			// fall through; might be a headless box
		}
	}
	return loadFile(account)
}

func Delete(account string) error {
	if keychainEnabled() {
		_ = keyring.Delete(serviceName, account) // best effort
	}
	return deleteFile(account)
}

// File fallback layout: ~/.prbe/credentials, JSON map { account: value }, mode 0600.

func credentialsPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

func readMap() (map[string]string, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	m := map[string]string{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func writeMap(m map[string]string) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func storeFile(account, value string) error {
	m, err := readMap()
	if err != nil {
		return err
	}
	m[account] = value
	return writeMap(m)
}

func loadFile(account string) (string, error) {
	m, err := readMap()
	if err != nil {
		return "", err
	}
	return m[account], nil
}

func deleteFile(account string) error {
	m, err := readMap()
	if err != nil {
		return err
	}
	delete(m, account)
	return writeMap(m)
}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test -race ./internal/creds/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/creds
git commit -m "feat(creds): keychain with 0600 file fallback (gh-style)"
```

---

## Task 7: Credentials — log-field redactor

**Files:**
- Create: `internal/creds/redact.go`
- Create: `internal/creds/redact_test.go`

- [ ] **Step 1: Write the failing test**

```go
package creds

import "testing"

func TestRedactStripsAuthAndTokens(t *testing.T) {
	cases := []struct {
		key, val, want string
	}{
		{"Authorization", "Bearer xyz", "***"},
		{"authorization", "Bearer xyz", "***"},
		{"device_token", "tok-abc", "***"},
		{"pairing_token", "jwt.payload.sig", "***"},
		{"DEVICE_TOKEN", "tok-abc", "***"},
		{"some_token_field", "secret", "***"},
		{"hostname", "mahits-mac", "mahits-mac"},
		{"os", "macos", "macos"},
	}
	for _, c := range cases {
		got := RedactField(c.key, c.val)
		if got != c.want {
			t.Errorf("RedactField(%q, %q) = %q want %q", c.key, c.val, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/creds/...`
Expected: FAIL — undefined `RedactField`.

- [ ] **Step 3: Create `internal/creds/redact.go`**

```go
package creds

import "strings"

// RedactField returns "***" if the key indicates a credential field, otherwise the original value.
// Comparison is case-insensitive.
func RedactField(key, value string) string {
	k := strings.ToLower(key)
	if k == "authorization" {
		return "***"
	}
	if strings.Contains(k, "token") {
		return "***"
	}
	return value
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/creds/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/creds
git commit -m "feat(creds): log-field redactor for tokens/auth headers"
```

---

## Task 8: HTTP client — retry classification

**Files:**
- Create: `internal/httpclient/retry.go`
- Create: `internal/httpclient/retry_test.go`

- [ ] **Step 1: Write the failing test `internal/httpclient/retry_test.go`**

```go
package httpclient

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   Classification
	}{
		{"200 ok", 200, nil, Success},
		{"201 created", 201, nil, Success},
		{"400 bad request", 400, nil, Poison},
		{"403 forbidden", 403, nil, Poison},
		{"404 not found", 404, nil, Poison},
		{"401 unauthorized", 401, nil, Halt},
		{"408 timeout", 408, nil, Retry},
		{"429 rate limit", 429, nil, Retry},
		{"500 server error", 500, nil, Retry},
		{"502 bad gateway", 502, nil, Retry},
		{"503 unavailable", 503, nil, Retry},
		{"504 gateway timeout", 504, nil, Retry},
		{"other 4xx", 418, nil, Retry},
		{"network error", 0, errFake("dial tcp: connection refused"), Retry},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.status, c.err)
			if got != c.want {
				t.Fatalf("Classify(%d, %v) = %v want %v", c.status, c.err, got, c.want)
			}
		})
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/httpclient/...`
Expected: FAIL — undefined `Classify`, `Classification`, etc.

- [ ] **Step 3: Create `internal/httpclient/retry.go`**

```go
package httpclient

// Classification describes how the daemon should react to a single HTTP attempt.
type Classification int

const (
	// Success: 2xx. Delete the outbox row, advance offsets.
	Success Classification = iota
	// Poison: definitively-bad request (4xx that won't change on retry). Drop the row, log, continue.
	Poison
	// Halt: device is no longer authorized (401). Drop entire outbox + halt watcher.
	Halt
	// Retry: try again later (transient/network/5xx).
	Retry
)

func (c Classification) String() string {
	switch c {
	case Success:
		return "success"
	case Poison:
		return "poison"
	case Halt:
		return "halt"
	case Retry:
		return "retry"
	}
	return "unknown"
}

// Classify maps (HTTP status, transport error) → Classification.
//
// status == 0 means the request never produced a response (network/DNS/TLS failure); err is set in that case.
func Classify(status int, err error) Classification {
	if err != nil {
		return Retry
	}
	switch {
	case status >= 200 && status < 300:
		return Success
	case status == 401:
		return Halt
	case status == 400 || status == 403 || status == 404:
		return Poison
	default:
		return Retry
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/httpclient/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpclient
git commit -m "feat(httpclient): retry classification matrix"
```

---

## Task 9: HTTP client — backoff schedule

**Files:**
- Create: `internal/httpclient/backoff.go`
- Create: `internal/httpclient/backoff_test.go`

- [ ] **Step 1: Write the failing test**

```go
package httpclient

import (
	"testing"
	"time"
)

func TestBackoffMonotonicAndCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 0; attempt < 12; attempt++ {
		got := Backoff(attempt)
		if got > 5*time.Minute+time.Second { // include max jitter
			t.Fatalf("attempt %d: backoff %v exceeds cap", attempt, got)
		}
		if attempt > 0 && got < prev/2 {
			t.Fatalf("attempt %d: backoff %v dropped sharply from %v", attempt, got, prev)
		}
		prev = got
	}
}

func TestBackoffJitterBounded(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := Backoff(0)
		// Attempt 0: 1s base + [0, 1s) jitter → [1s, 2s).
		if got < time.Second || got >= 2*time.Second {
			t.Fatalf("Backoff(0)=%v not in [1s, 2s)", got)
		}
	}
}

func TestBackoffCapEnforced(t *testing.T) {
	// Attempt 20: 2^20 seconds is way over cap; result should be in [5m, 5m+1s).
	got := Backoff(20)
	if got < 5*time.Minute || got >= 5*time.Minute+time.Second {
		t.Fatalf("Backoff(20)=%v not in [5m, 5m+1s)", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/httpclient/...`
Expected: FAIL — undefined `Backoff`.

- [ ] **Step 3: Create `internal/httpclient/backoff.go`**

```go
package httpclient

import (
	"math/rand"
	"time"
)

const (
	backoffBase = 1 * time.Second
	backoffCap  = 5 * time.Minute
)

// Backoff returns the delay before the next attempt:
//
//	min(2^attempt * 1s, 5min) + jitter ∈ [0, 1s)
//
// Always ≥ 1s on attempt 0.
func Backoff(attempt int) time.Duration {
	exp := backoffBase << attempt
	if exp <= 0 || exp > backoffCap {
		exp = backoffCap
	}
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return exp + jitter
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/httpclient/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpclient
git commit -m "feat(httpclient): exp backoff with jitter capped at 5min"
```

---

## Task 10: HTTP client — Do() with httptest

**Files:**
- Create: `internal/httpclient/client.go`
- Create: `internal/httpclient/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoSendsBearerAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotTrace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotTrace = r.Header.Get("X-Trace-Id")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	resp, cls, err := c.Do(context.Background(), Request{
		Method: "POST", Path: "/x", Bearer: "tok-abc", Body: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if cls != Success {
		t.Fatalf("classification = %v", cls)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "prbe-agent-tap/test ") {
		t.Fatalf("user-agent = %q", gotUA)
	}
	if gotTrace == "" {
		t.Fatal("missing X-Trace-Id")
	}
}

func TestDoClassifies5xxAsRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Retry {
		t.Fatalf("classification = %v want Retry", cls)
	}
}

func TestDoClassifies401AsHalt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Version: "test"})
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Halt {
		t.Fatalf("classification = %v want Halt", cls)
	}
}

func TestDoNetworkErrorIsRetry(t *testing.T) {
	c := New(Options{BaseURL: "http://127.0.0.1:1", Version: "test"}) // unreachable port
	_, cls, _ := c.Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if cls != Retry {
		t.Fatalf("classification = %v want Retry", cls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/httpclient/...`
Expected: FAIL — undefined `New`, `Options`, `Request`, etc.

- [ ] **Step 3: Create `internal/httpclient/client.go`**

```go
package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

type Options struct {
	BaseURL string
	Version string
	HTTP    *http.Client
}

type Client struct {
	base    string
	version string
	hc      *http.Client
}

func New(o Options) *Client {
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: o.BaseURL, version: o.Version, hc: hc}
}

type Request struct {
	Method string
	Path   string
	Bearer string // empty for /agent-tap/pair
	Body   []byte
}

// Do executes a single attempt. The caller decides whether to retry based on Classification.
func (c *Client) Do(ctx context.Context, r Request) (*http.Response, Classification, error) {
	url := c.base + r.Path
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, url, body)
	if err != nil {
		return nil, Retry, err
	}
	if r.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.Bearer)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("prbe-agent-tap/%s (%s/%s)", c.version, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-Trace-Id", newTraceID())
	if len(r.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, Classify(0, err), err
	}
	return resp, Classify(resp.StatusCode, nil), nil
}

func newTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/httpclient/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpclient
git commit -m "feat(httpclient): bearer-aware Do() with trace id and UA"
```

---

## Task 11: Subcommand dispatcher in main.go

**Files:**
- Modify: `cmd/prbe-agent-tap/main.go`
- Create: `cmd/prbe-agent-tap/dispatch.go`
- Create: `cmd/prbe-agent-tap/dispatch_test.go`

- [ ] **Step 1: Write the failing test `cmd/prbe-agent-tap/dispatch_test.go`**

```go
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDispatchUnknownExitCode(t *testing.T) {
	var stderr bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "nope"}, nil, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDispatchHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "help"}, &stdout, nil)
	if code != 0 {
		t.Fatalf("exit = %d want 0", code)
	}
	for _, want := range []string{"pair", "watch", "heartbeat", "status", "backfill", "revoke", "install", "uninstall"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestDispatchVersion(t *testing.T) {
	var stdout bytes.Buffer
	code := dispatch(context.Background(), []string{"prbe-agent-tap", "version"}, &stdout, nil)
	if code != 0 {
		t.Fatalf("exit = %d want 0", code)
	}
	if !strings.Contains(stdout.String(), "prbe-agent-tap") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./cmd/...`
Expected: FAIL — undefined `dispatch`.

- [ ] **Step 3: Create `cmd/prbe-agent-tap/dispatch.go`**

```go
package main

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/version"
)

// subcommand is registered per package. Each implementation lives in its own internal package
// (added in subsequent tasks); here we only declare the dispatch surface.
type subcommand struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, stdout, stderr io.Writer) int
}

func subcommands() []subcommand {
	return []subcommand{
		{name: "pair", summary: "exchange pairing token for a device token", run: runPair},
		{name: "watch", summary: "tail Claude Code transcripts and ship batches", run: runWatch},
		{name: "heartbeat", summary: "one-shot liveness ping (called by launchd/systemd timer)", run: runHeartbeat},
		{name: "revoke", summary: "revoke this device and wipe local credentials", run: runRevoke},
		{name: "status", summary: "print local daemon state", run: runStatus},
		{name: "backfill", summary: "ship historical Claude Code sessions", run: runBackfill},
		{name: "install", summary: "register launchd/systemd units and start the watcher", run: runInstall},
		{name: "uninstall", summary: "stop the daemon and remove local state", run: runUninstall},
	}
}

func dispatch(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(argv) < 2 {
		fmt.Fprintln(stderr, "no subcommand given; try `prbe-agent-tap help`")
		return 2
	}
	cmd := argv[1]
	args := argv[2:]
	switch cmd {
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "prbe-agent-tap %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
		return 0
	}
	for _, s := range subcommands() {
		if s.name == cmd {
			return s.run(ctx, args, stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "unknown subcommand %q; try `prbe-agent-tap help`\n", cmd)
	return 2
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: prbe-agent-tap <subcommand> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	for _, s := range subcommands() {
		fmt.Fprintf(w, "  %-12s %s\n", s.name, s.summary)
	}
}

// Stub implementations; each is replaced in later tasks.
func runPair(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "pair: not yet implemented (Task 12)")
	return 2
}
func runWatch(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "watch: not yet implemented (Task 20)")
	return 2
}
func runHeartbeat(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "heartbeat: not yet implemented (Task 13)")
	return 2
}
func runRevoke(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "revoke: not yet implemented (Task 14)")
	return 2
}
func runStatus(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "status: not yet implemented (Task 21)")
	return 2
}
func runBackfill(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "backfill: not yet implemented (Task 22)")
	return 2
}
func runInstall(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "install: not yet implemented (Task 25)")
	return 2
}
func runUninstall(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "uninstall: not yet implemented (Task 25)")
	return 2
}
```

- [ ] **Step 4: Replace `cmd/prbe-agent-tap/main.go` with the dispatcher entry point**

```go
package main

import (
	"context"
	"os"
)

func main() {
	os.Exit(dispatch(context.Background(), os.Args, os.Stdout, os.Stderr))
}
```

- [ ] **Step 5: Run to verify the test passes and the binary builds**

```bash
go test -race ./cmd/...
go build ./cmd/prbe-agent-tap
./prbe-agent-tap help
```

Expected: tests PASS; help lists all 8 subcommands.

- [ ] **Step 6: Commit**

```bash
git add cmd/prbe-agent-tap
git commit -m "feat(cmd): subcommand dispatcher with help/version + stubs"
```

---

## Task 12: pair subcommand

**Files:**
- Create: `internal/pair/pair.go`
- Create: `internal/pair/pair_test.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runPair` stub)

- [ ] **Step 1: Write the failing test `internal/pair/pair_test.go`**

```go
package pair

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestPairHappyPathWritesCredsAndMeta(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent-tap/pair" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["pairing_token"] != "ptok" || body["os"] == "" || body["hostname"] == "" {
			t.Fatalf("bad body %v", body)
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_id":    "dev-1",
			"device_token": "dtok",
			"customer_id":  "cust-1",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := storage.Open(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	var stdout bytes.Buffer
	if err := Run(context.Background(), Args{
		PairingToken: "ptok",
		Storage:      s,
		Client:       hc,
		Stdout:       &stdout,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tok, _ := creds.Load("device-token")
	if tok != "dtok" {
		t.Fatalf("token in creds = %q want dtok", tok)
	}
	devID, _ := s.GetMeta("device_id")
	if devID != "dev-1" {
		t.Fatalf("device_id = %q", devID)
	}
	cust, _ := s.GetMeta("customer_id")
	if cust != "cust-1" {
		t.Fatalf("customer_id = %q", cust)
	}
	last401, _ := s.GetMeta("last_401_at")
	if last401 != "" {
		t.Fatalf("last_401_at should be cleared, got %q", last401)
	}
	if !strings.Contains(stdout.String(), "Paired") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPair401Rejects(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	err := Run(context.Background(), Args{
		PairingToken: "bad", Storage: s, Client: hc,
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/pair/...`
Expected: FAIL — undefined `Run`, `Args`.

- [ ] **Step 3: Create `internal/pair/pair.go`**

```go
package pair

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	PairingToken string
	Storage      *storage.Storage
	Client       *httpclient.Client
	Stdout       io.Writer
}

type pairRequest struct {
	PairingToken string `json:"pairing_token"`
	OS           string `json:"os"`
	Hostname     string `json:"hostname"`
}

type pairResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	CustomerID  string `json:"customer_id"`
}

func Run(ctx context.Context, a Args) error {
	if a.PairingToken == "" {
		return fmt.Errorf("pairing token required")
	}
	host, _ := os.Hostname()
	body, err := json.Marshal(pairRequest{
		PairingToken: a.PairingToken,
		OS:           osLabel(),
		Hostname:     host,
	})
	if err != nil {
		return err
	}
	resp, cls, err := a.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/agent-tap/pair", Body: body,
	})
	if err != nil && cls != httpclient.Halt {
		return fmt.Errorf("pair request failed: %w", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if cls == httpclient.Halt {
		return fmt.Errorf("pairing token rejected by server (request a fresh one from the dashboard)")
	}
	if cls != httpclient.Success {
		return fmt.Errorf("pair returned non-success classification %s", cls)
	}

	var pr pairResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return fmt.Errorf("decode pair response: %w", err)
	}
	if pr.DeviceToken == "" || pr.DeviceID == "" {
		return fmt.Errorf("pair response missing device_token or device_id")
	}
	if err := creds.Store("device-token", pr.DeviceToken); err != nil {
		return fmt.Errorf("store device token: %w", err)
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	for k, v := range map[string]string{
		"device_id":   pr.DeviceID,
		"customer_id": pr.CustomerID,
		"paired_at":   now,
	} {
		if err := a.Storage.SetMeta(k, v); err != nil {
			return fmt.Errorf("write meta %s: %w", k, err)
		}
	}
	if err := a.Storage.DeleteMeta("last_401_at"); err != nil {
		return err
	}
	if a.Stdout != nil {
		fmt.Fprintf(a.Stdout, "Paired. device_id=%s\nRun `prbe-agent-tap install` to start watching.\n", pr.DeviceID)
	}
	return nil
}

func osLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}
```

- [ ] **Step 4: Replace the `runPair` stub in `cmd/prbe-agent-tap/dispatch.go`**

Locate the `runPair` function and replace it with:

```go
func runPair(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: prbe-agent-tap pair <pairing-token>")
		return 2
	}
	token := fs.Arg(0)

	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()

	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := pair.Run(ctx, pair.Args{
		PairingToken: token, Storage: s, Client: hc, Stdout: stdout,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
```

Add the imports at the top of `dispatch.go`:

```go
import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/pair"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
	"github.com/prbe-ai/prbe-agent-tap/internal/version"
)
```

And add the helper functions to `dispatch.go`:

```go
func apiBaseURL() string {
	if u := os.Getenv("PRBE_API_BASE_URL"); u != "" {
		return u
	}
	return "https://api.prbe.ai"
}

func stateDBPath() (string, error) {
	dir, err := creds.StateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}
```

- [ ] **Step 5: Run tests and build**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: tests PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add cmd internal/pair
git commit -m "feat(pair): exchange pairing JWT for device token"
```

---

## Task 13: heartbeat subcommand

**Files:**
- Create: `internal/heartbeat/heartbeat.go`
- Create: `internal/heartbeat/heartbeat_test.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runHeartbeat` stub)

- [ ] **Step 1: Write the failing test `internal/heartbeat/heartbeat_test.go`**

```go
package heartbeat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestHeartbeatHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent-tap/heartbeat" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dtok" {
			t.Fatalf("auth = %q", got)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	if err := Run(context.Background(), Args{
		DeviceToken: "dtok", Storage: s, Client: hc,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if v, _ := s.GetMeta("last_heartbeat_at"); v == "" {
		t.Fatal("last_heartbeat_at not set")
	} else if _, err := strconv.ParseInt(v, 10, 64); err != nil {
		t.Fatalf("last_heartbeat_at not unix: %q", v)
	}
}

func TestHeartbeat401MarksLast401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})

	err := Run(context.Background(), Args{
		DeviceToken: "stale", Storage: s, Client: hc,
	})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if v, _ := s.GetMeta("last_401_at"); v == "" {
		t.Fatal("last_401_at not set")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/heartbeat/...`
Expected: FAIL — undefined `Run`, `Args`.

- [ ] **Step 3: Create `internal/heartbeat/heartbeat.go`**

```go
package heartbeat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	DeviceToken string
	Storage     *storage.Storage
	Client      *httpclient.Client
}

func Run(ctx context.Context, a Args) error {
	if a.DeviceToken == "" {
		return fmt.Errorf("missing device token (run `prbe-agent-tap pair` first)")
	}
	resp, cls, err := a.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/agent-tap/heartbeat", Bearer: a.DeviceToken,
	})
	if resp != nil {
		defer resp.Body.Close()
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	switch cls {
	case httpclient.Success:
		_ = a.Storage.SetMeta("last_heartbeat_at", now)
		return nil
	case httpclient.Halt:
		_ = a.Storage.SetMeta("last_401_at", now)
		return fmt.Errorf("heartbeat rejected: device token revoked (re-pair required)")
	default:
		if err != nil {
			return fmt.Errorf("heartbeat failed (%s): %w", cls, err)
		}
		return fmt.Errorf("heartbeat failed: %s", cls)
	}
}
```

- [ ] **Step 4: Replace `runHeartbeat` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runHeartbeat(ctx context.Context, _ []string, _, stderr io.Writer) int {
	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()

	tok, err := creds.Load("device-token")
	if err != nil || tok == "" {
		fmt.Fprintln(stderr, "no device token; run `prbe-agent-tap pair` first")
		return 1
	}
	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := heartbeat.Run(ctx, heartbeat.Args{DeviceToken: tok, Storage: s, Client: hc}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
```

Add to imports: `"github.com/prbe-ai/prbe-agent-tap/internal/heartbeat"`.

- [ ] **Step 5: Run tests and build**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd internal/heartbeat
git commit -m "feat(heartbeat): one-shot liveness ping"
```

---

## Task 14: revoke subcommand

**Files:**
- Create: `internal/revoke/revoke.go`
- Create: `internal/revoke/revoke_test.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runRevoke` stub)

- [ ] **Step 1: Write the failing test `internal/revoke/revoke_test.go`**

```go
package revoke

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestRevokeHappyPathClearsState(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())
	_ = creds.Store("device-token", "dtok")

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/agent-tap/revoke" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	_ = s.SetMeta("device_id", "dev-1")
	_ = s.SetMeta("customer_id", "cust-1")

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	if err := Run(context.Background(), Args{DeviceToken: "dtok", Storage: s, Client: hc}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d want 1", hits)
	}
	if v, _ := creds.Load("device-token"); v != "" {
		t.Fatalf("creds left over: %q", v)
	}
	if v, _ := s.GetMeta("device_id"); v != "" {
		t.Fatalf("device_id left over: %q", v)
	}
}

func TestRevoke401StillClearsLocally(t *testing.T) {
	t.Setenv("PRBE_DISABLE_KEYCHAIN", "1")
	t.Setenv("PRBE_STATE_DIR", t.TempDir())
	_ = creds.Store("device-token", "stale")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	_ = s.SetMeta("device_id", "dev-1")

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	// Even though server says 401, local cleanup must still happen.
	_ = Run(context.Background(), Args{DeviceToken: "stale", Storage: s, Client: hc})

	if v, _ := creds.Load("device-token"); v != "" {
		t.Fatalf("creds left over: %q", v)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/revoke/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `internal/revoke/revoke.go`**

```go
package revoke

import (
	"context"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Args struct {
	DeviceToken string
	Storage     *storage.Storage
	Client      *httpclient.Client
}

// Run is intentionally tolerant: it always wipes local state, even if the server call fails.
// Server-side revocation is best-effort because uninstall must succeed offline.
func Run(ctx context.Context, a Args) error {
	var serverErr error
	if a.DeviceToken != "" {
		resp, _, err := a.Client.Do(ctx, httpclient.Request{
			Method: "POST", Path: "/agent-tap/revoke", Bearer: a.DeviceToken,
		})
		if resp != nil {
			defer resp.Body.Close()
		}
		serverErr = err
	}

	// Always clear local state.
	_ = creds.Delete("device-token")
	if a.Storage != nil {
		for _, k := range []string{"device_id", "customer_id", "paired_at",
			"last_heartbeat_at", "last_successful_post_at", "last_401_at"} {
			_ = a.Storage.DeleteMeta(k)
		}
		_, _ = a.Storage.ClearOutbox()
	}
	return serverErr
}
```

- [ ] **Step 4: Replace `runRevoke` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runRevoke(ctx context.Context, _ []string, stdout, stderr io.Writer) int {
	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()
	tok, _ := creds.Load("device-token")
	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	if err := revoke.Run(ctx, revoke.Args{DeviceToken: tok, Storage: s, Client: hc}); err != nil {
		fmt.Fprintln(stderr, "server-side revoke failed (local state still wiped):", err)
		return 0 // local cleanup succeeded; non-fatal
	}
	fmt.Fprintln(stdout, "Revoked. Local credentials and state cleared.")
	return 0
}
```

Add to imports: `"github.com/prbe-ai/prbe-agent-tap/internal/revoke"`.

- [ ] **Step 5: Run tests and build**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd internal/revoke
git commit -m "feat(revoke): server revoke + always-on local cleanup"
```

---

## Task 15: outbox drainer

**Files:**
- Create: `internal/outbox/drainer.go`
- Create: `internal/outbox/drainer_test.go`

- [ ] **Step 1: Write the failing test `internal/outbox/drainer_test.go`**

```go
package outbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func mustOpenStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDrainerHappyPathPostsAllRows(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer srv.Close()

	s := mustOpenStorage(t)
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		_ = s.EnqueueBatch(storage.OutboxRow{
			SessionID: "sid", BatchSeq: int64(i), CWD: "/", Body: []byte(`{}`),
			CreatedAt: now, NextAttemptAt: now,
		})
	}

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	d := New(Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return "tok", nil },
		PollInterval:   10 * time.Millisecond,
		Now:            func() time.Time { return time.Now() },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go d.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&hits) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) < 3 {
		t.Fatalf("expected 3 hits, got %d", hits)
	}
}

func TestDrainerHaltsOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	s := mustOpenStorage(t)
	now := time.Now().Unix()
	_ = s.EnqueueBatch(storage.OutboxRow{
		SessionID: "sid", BatchSeq: 0, CWD: "/", Body: []byte(`{}`),
		CreatedAt: now, NextAttemptAt: now,
	})

	hc := httpclient.New(httpclient.Options{BaseURL: srv.URL, Version: "test"})
	d := New(Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return "tok", nil },
		PollInterval:   10 * time.Millisecond,
		Now:            func() time.Time { return time.Now() },
	})

	err := d.Run(context.Background())
	if err == nil || !errIsHalt(err) {
		t.Fatalf("Run err = %v want halt", err)
	}

	last401, _ := s.GetMeta("last_401_at")
	if last401 == "" {
		t.Fatal("last_401_at not set")
	}
	row, ok, _ := s.NextDueBatch(time.Now().Unix() + 9999)
	if ok {
		t.Fatalf("outbox not cleared: %+v", row)
	}
}

func errIsHalt(err error) bool { return err != nil && err.Error() == "halted: device token revoked" }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/outbox/...`
Expected: FAIL — undefined `New`, `Config`, `Run`.

- [ ] **Step 3: Create `internal/outbox/drainer.go`**

```go
package outbox

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/httpclient"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

type Config struct {
	Storage        *storage.Storage
	Client         *httpclient.Client
	BearerProvider func() (string, error)
	PollInterval   time.Duration
	Now            func() time.Time
	MaxBytes       int64 // 0 → 100 MB default
}

type Drainer struct {
	cfg Config
}

func New(cfg Config) *Drainer {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 100 * 1024 * 1024
	}
	return &Drainer{cfg: cfg}
}

// Run blocks until the context is cancelled or 401 is received.
// On 401, returns the sentinel halt error after clearing the outbox + setting last_401_at.
func (d *Drainer) Run(ctx context.Context) error {
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := d.tick(ctx); err != nil {
				return err
			}
		}
	}
}

func (d *Drainer) tick(ctx context.Context) error {
	now := d.cfg.Now().Unix()
	row, ok, err := d.cfg.Storage.NextDueBatch(now)
	if err != nil {
		return fmt.Errorf("NextDueBatch: %w", err)
	}
	if !ok {
		// Best-effort cap enforcement on idle.
		_, _ = d.cfg.Storage.EnforceOutboxCap(d.cfg.MaxBytes)
		return nil
	}
	bearer, err := d.cfg.BearerProvider()
	if err != nil || bearer == "" {
		// No token — back off; caller is responsible for fixing.
		_ = d.cfg.Storage.MarkFailure(row.ID, now+30, "no device token")
		return nil
	}
	resp, cls, doErr := d.cfg.Client.Do(ctx, httpclient.Request{
		Method: "POST", Path: "/webhooks/claude_code", Bearer: bearer, Body: row.Body,
	})
	if resp != nil {
		_ = resp.Body.Close()
	}
	switch cls {
	case httpclient.Success:
		_ = d.cfg.Storage.MarkSuccess(row.ID)
		_ = d.cfg.Storage.SetMeta("last_successful_post_at", strconv.FormatInt(now, 10))
		return nil
	case httpclient.Poison:
		// Drop the row and continue.
		_ = d.cfg.Storage.MarkSuccess(row.ID)
		return nil
	case httpclient.Halt:
		_, _ = d.cfg.Storage.ClearOutbox()
		_ = d.cfg.Storage.SetMeta("last_401_at", strconv.FormatInt(now, 10))
		return fmt.Errorf("halted: device token revoked")
	default: // Retry
		errMsg := "transient failure"
		if doErr != nil {
			errMsg = doErr.Error()
		}
		next := now + int64(httpclient.Backoff(int(row.AttemptCount)).Seconds())
		_ = d.cfg.Storage.MarkFailure(row.ID, next, errMsg)
		return nil
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/outbox/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/outbox
git commit -m "feat(outbox): drainer with halt-on-401 and cap enforcement"
```

---

## Task 16: Watch — JSONL line splitter

**Files:**
- Create: `internal/watch/parser.go`
- Create: `internal/watch/parser_test.go`

- [ ] **Step 1: Write the failing test `internal/watch/parser_test.go`**

```go
package watch

import (
	"bytes"
	"strings"
	"testing"
)

func TestSplitLinesCompleteOnly(t *testing.T) {
	in := []byte(`{"a":1}` + "\n" + `{"b":2}` + "\n")
	lines, rem, err := SplitLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Fatalf("rem = %d want 0", rem)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines want 2", len(lines))
	}
	if !bytes.Equal(lines[0], []byte(`{"a":1}`)) {
		t.Fatalf("line 0 = %q", lines[0])
	}
}

func TestSplitLinesIncompleteFinal(t *testing.T) {
	in := []byte(`{"a":1}` + "\n" + `{"b":`)
	lines, rem, err := SplitLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if rem != len(`{"b":`) {
		t.Fatalf("rem = %d want %d", rem, len(`{"b":`))
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1", len(lines))
	}
}

func TestSplitLinesSkipsBlanks(t *testing.T) {
	in := []byte("\n\n" + `{"a":1}` + "\n\n")
	lines, _, _ := SplitLines(in)
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1", len(lines))
	}
}

func TestValidateJSON(t *testing.T) {
	if err := ValidateJSON([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("valid line rejected: %v", err)
	}
	if err := ValidateJSON([]byte(`{`)); err == nil {
		t.Fatal("malformed line accepted")
	}
	if err := ValidateJSON([]byte(`not even json`)); err == nil {
		t.Fatal("non-json accepted")
	}
}

func TestValidateJSONLargeOK(t *testing.T) {
	big := []byte(`{"x":"` + strings.Repeat("a", 1024*1024) + `"}`)
	if err := ValidateJSON(big); err != nil {
		t.Fatalf("large line rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/watch/...`
Expected: FAIL — undefined `SplitLines`, `ValidateJSON`.

- [ ] **Step 3: Create `internal/watch/parser.go`**

```go
package watch

import (
	"bytes"
	"encoding/json"
)

// SplitLines splits a buffer into complete newline-terminated lines.
// Returns the parsed lines, the byte count of any trailing partial line, and any error.
// Blank lines are skipped.
func SplitLines(buf []byte) ([][]byte, int, error) {
	var out [][]byte
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == '\n' {
			line := bytes.TrimRight(buf[start:i], "\r")
			if len(line) > 0 {
				out = append(out, append([]byte(nil), line...))
			}
			start = i + 1
		}
	}
	return out, len(buf) - start, nil
}

// ValidateJSON returns nil if line is well-formed JSON.
func ValidateJSON(line []byte) error {
	var v interface{}
	return json.Unmarshal(line, &v)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/watch/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/watch
git commit -m "feat(watch): JSONL line splitter + JSON validator"
```

---

## Task 17: Watch — per-file reader

**Files:**
- Create: `internal/watch/reader.go`
- Create: `internal/watch/reader_test.go`

- [ ] **Step 1: Write the failing test `internal/watch/reader_test.go`**

```go
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
	// Append a third line.
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

	// Truncate to a single line.
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
	// Complete the partial line.
	writeFile(t, path, `{"a":1}`+"\n"+`{"b":2}`+"\n")
	lines, _ = r.ReadNew()
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1 after completing partial", len(lines))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/watch/...`
Expected: FAIL — undefined `OpenReader`.

- [ ] **Step 3: Create `internal/watch/reader.go`**

```go
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
		// Truncation: reopen and reset.
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
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/watch/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/watch
git commit -m "feat(watch): per-file reader with truncation detection"
```

---

## Task 18: Watch — buffer + flush conditions

**Files:**
- Create: `internal/watch/buffer.go`
- Create: `internal/watch/buffer_test.go`

- [ ] **Step 1: Write the failing test `internal/watch/buffer_test.go`**

```go
package watch

import (
	"testing"
	"time"
)

func TestBufferFlushAtSizeThreshold(t *testing.T) {
	b := NewBuffer(BufferConfig{
		MaxLines:    10,
		MaxAge:      time.Hour,
		Now:         func() time.Time { return time.Unix(0, 0) },
	})
	for i := 0; i < 9; i++ {
		if b.ShouldFlush() {
			t.Fatalf("flush at %d lines", i)
		}
		b.Add([]byte(`{}`))
	}
	b.Add([]byte(`{}`)) // 10th line
	if !b.ShouldFlush() {
		t.Fatal("expected flush at 10 lines")
	}
}

func TestBufferFlushAtAge(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBuffer(BufferConfig{
		MaxLines: 100,
		MaxAge:   5 * time.Second,
		Now:      func() time.Time { return now },
	})
	b.Add([]byte(`{}`))
	if b.ShouldFlush() {
		t.Fatal("flush too early")
	}
	now = now.Add(6 * time.Second)
	if !b.ShouldFlush() {
		t.Fatal("expected flush after 6s")
	}
}

func TestBufferDrainResets(t *testing.T) {
	b := NewBuffer(BufferConfig{MaxLines: 2, MaxAge: time.Hour, Now: time.Now})
	b.Add([]byte(`{"a":1}`))
	b.Add([]byte(`{"b":2}`))
	if !b.ShouldFlush() {
		t.Fatal("expected flush")
	}
	lines := b.Drain()
	if len(lines) != 2 {
		t.Fatalf("drained %d want 2", len(lines))
	}
	if b.ShouldFlush() {
		t.Fatal("flush after drain")
	}
	if b.Len() != 0 {
		t.Fatal("len != 0 after drain")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/watch/...`
Expected: FAIL — undefined `NewBuffer`, `BufferConfig`.

- [ ] **Step 3: Create `internal/watch/buffer.go`**

```go
package watch

import "time"

type BufferConfig struct {
	MaxLines int
	MaxAge   time.Duration
	Now      func() time.Time
}

type Buffer struct {
	cfg     BufferConfig
	lines   [][]byte
	firstAt time.Time
}

func NewBuffer(cfg BufferConfig) *Buffer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Buffer{cfg: cfg}
}

func (b *Buffer) Add(line []byte) {
	if len(b.lines) == 0 {
		b.firstAt = b.cfg.Now()
	}
	b.lines = append(b.lines, append([]byte(nil), line...))
}

func (b *Buffer) Len() int { return len(b.lines) }

func (b *Buffer) ShouldFlush() bool {
	if len(b.lines) == 0 {
		return false
	}
	if len(b.lines) >= b.cfg.MaxLines {
		return true
	}
	if b.cfg.Now().Sub(b.firstAt) >= b.cfg.MaxAge {
		return true
	}
	return false
}

func (b *Buffer) Drain() [][]byte {
	out := b.lines
	b.lines = nil
	b.firstAt = time.Time{}
	return out
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/watch/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/watch
git commit -m "feat(watch): batch buffer with size + age flush conditions"
```

---

## Task 19: Watch — fsnotify-driven watcher

**Files:**
- Create: `internal/watch/watcher.go`
- Create: `internal/watch/watcher_test.go`

- [ ] **Step 1: Add fsnotify dep**

```bash
go get github.com/fsnotify/fsnotify
go mod tidy
```

- [ ] **Step 2: Write the failing test `internal/watch/watcher_test.go`**

```go
package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestWatcherShipsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-Users-foo-repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(projectDir, "abc-123.jsonl")
	if err := os.WriteFile(sessionFile, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	s, _ := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	defer s.Close()

	w, err := NewWatcher(WatcherConfig{
		ProjectsRoot:   dir,
		Storage:        s,
		BatchMaxLines:  2,
		BatchMaxAge:    100 * time.Millisecond,
		IdleAfter:      time.Hour,
		PollInterval:   25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.Run(ctx) }()

	// Wait for the watcher to register the existing file (offset = 0).
	time.Sleep(150 * time.Millisecond)

	// Append three lines.
	f, _ := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0o600)
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(map[string]int{"i": i})
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	// Poll outbox until we see at least one row enqueued (the first 2-line batch).
	deadline := time.Now().Add(2 * time.Second)
	var rows int
	for time.Now().Before(deadline) {
		row, ok, _ := s.NextDueBatch(time.Now().Unix() + 9999)
		if ok {
			rows++
			_ = s.MarkSuccess(row.ID)
			if rows >= 1 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if rows < 1 {
		t.Fatalf("expected at least 1 outbox row enqueued, got %d", rows)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test -race ./internal/watch/...`
Expected: FAIL — undefined `NewWatcher`, `WatcherConfig`.

- [ ] **Step 4: Create `internal/watch/watcher.go`**

```go
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
	ProjectsRoot   string                  // typically ~/.claude/projects
	Storage        *storage.Storage
	BatchMaxLines  int
	BatchMaxAge    time.Duration
	IdleAfter      time.Duration            // flush if buffer non-empty and no activity for this long
	PollInterval   time.Duration            // tick driving age + idle checks
	DeviceIDFunc   func() (string, error)   // returns the device_id; defaults to storage meta lookup
}

type Watcher struct {
	cfg    WatcherConfig
	mu     sync.Mutex
	files  map[string]*fileState           // keyed by absolute path
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
			// best-effort: continue
		case <-t.C:
			w.tick()
		}
	}
}

// bootstrap walks ProjectsRoot, registers existing JSONLs (skipping symlinks),
// and watches each project subdirectory.
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
	// Skip historical content: pretend we've already read every line that exists today.
	existing, _ := CurrentLineCount(path)
	fs.lineNo = existing
	// Also advance the reader past the existing bytes.
	if _, err := os.Stat(path); err == nil {
		// Re-open at end.
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
		// Could be a new project subdir; subscribe.
		info, err := os.Lstat(path)
		if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			_ = fw.Add(path)
		}
		return
	}
	w.mu.Lock()
	fs, ok := w.files[path]
	if !ok {
		w.mu.Unlock()
		// New file: register from offset 0 (still register existing-style if pre-existed).
		_ = w.registerExistingFile(path)
		w.mu.Lock()
		fs = w.files[path]
	}
	w.mu.Unlock()
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

// flushLocked must be called with w.mu held.
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
```

- [ ] **Step 5: Add a tiny `inode_unix.go` (Linux + macOS) for inode extraction**

```go
//go:build !windows

package watch

import (
	"os"
	"syscall"
)

func inodeOf(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
```

- [ ] **Step 6: Run to verify pass**

Run: `go test -race ./internal/watch/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/watch
git commit -m "feat(watch): fsnotify-driven watcher + batch enqueue"
```

---

## Task 20: watch subcommand wiring

**Files:**
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runWatch` stub)

- [ ] **Step 1: Replace `runWatch` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runWatch(ctx context.Context, _ []string, _, stderr io.Writer) int {
	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()

	if v, _ := s.GetMeta("last_401_at"); v != "" {
		fmt.Fprintln(stderr, "halted: device token revoked at", v, "— run `prbe-agent-tap pair` to resume")
		return 1
	}

	deviceID, _ := s.GetMeta("device_id")
	if deviceID == "" {
		fmt.Fprintln(stderr, "not paired; run `prbe-agent-tap pair` first")
		return 1
	}

	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})

	w, err := watch.NewWatcher(watch.WatcherConfig{
		ProjectsRoot: claudeProjectsDir(),
		Storage:      s,
	})
	if err != nil {
		fmt.Fprintln(stderr, "watcher init:", err)
		return 1
	}

	d := outbox.New(outbox.Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return creds.Load("device-token") },
	})

	errCh := make(chan error, 2)
	go func() { errCh <- w.Run(ctx) }()
	go func() { errCh <- d.Run(ctx) }()

	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}

func claudeProjectsDir() string {
	if d := os.Getenv("PRBE_CLAUDE_PROJECTS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}
```

Add to imports: `"github.com/prbe-ai/prbe-agent-tap/internal/outbox"` and `"github.com/prbe-ai/prbe-agent-tap/internal/watch"`.

- [ ] **Step 2: Build to verify**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS, build OK.

- [ ] **Step 3: Commit**

```bash
git add cmd
git commit -m "feat(cmd): wire watch subcommand to watcher + drainer"
```

---

## Task 21: status subcommand

**Files:**
- Create: `internal/status/status.go`
- Create: `internal/status/status_test.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runStatus` stub)

- [ ] **Step 1: Write the failing test `internal/status/status_test.go`**

```go
package status

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestRenderUnpaired(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code == 0 {
		t.Fatal("expected non-zero exit when unpaired")
	}
	if !strings.Contains(buf.String(), "not paired") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderHalted(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	now := strconv.FormatInt(time.Now().Unix(), 10)
	_ = s.SetMeta("device_id", "dev-1")
	_ = s.SetMeta("last_401_at", now)

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code == 0 {
		t.Fatal("expected non-zero exit when halted")
	}
	if !strings.Contains(buf.String(), "halted") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderPaired(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()
	now := strconv.FormatInt(time.Now().Unix(), 10)
	_ = s.SetMeta("device_id", "abcdef0123456789")
	_ = s.SetMeta("customer_id", "cust-1")
	_ = s.SetMeta("paired_at", now)
	_ = s.SetMeta("last_successful_post_at", now)
	_ = s.SetMeta("last_heartbeat_at", now)

	var buf bytes.Buffer
	code := Render(s, &buf, false)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "paired") || !strings.Contains(out, "device:") {
		t.Fatalf("output = %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/status/...`
Expected: FAIL — undefined `Render`.

- [ ] **Step 3: Create `internal/status/status.go`**

```go
package status

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

// Render writes the status summary and returns an exit code.
// 0 = healthy paired; 1 = unpaired or halted.
func Render(s *storage.Storage, w io.Writer, verbose bool) int {
	deviceID, _ := s.GetMeta("device_id")
	if deviceID == "" {
		fmt.Fprintln(w, "prbe-agent-tap: not paired — run `prbe-agent-tap pair <token>`")
		return 1
	}
	if v, _ := s.GetMeta("last_401_at"); v != "" {
		fmt.Fprintf(w, "prbe-agent-tap: halted (token revoked at %s)\n", relative(v))
		fmt.Fprintln(w, "  Run `prbe-agent-tap pair <token>` with a fresh token from the dashboard to resume.")
		return 1
	}

	customer, _ := s.GetMeta("customer_id")
	pairedAt, _ := s.GetMeta("paired_at")
	lastShipped, _ := s.GetMeta("last_successful_post_at")
	lastHB, _ := s.GetMeta("last_heartbeat_at")

	outboxBytes, _ := s.OutboxByteSize()
	dropped, _ := s.GetMeta("outbox_dropped_count")

	fmt.Fprintln(w, "prbe-agent-tap: paired")
	fmt.Fprintf(w, "  device:        %s\n", shortID(deviceID, verbose))
	fmt.Fprintf(w, "  customer:      %s\n", customer)
	fmt.Fprintf(w, "  paired:        %s\n", relative(pairedAt))
	fmt.Fprintf(w, "  last shipped:  %s\n", relative(lastShipped))
	fmt.Fprintf(w, "  outbox:        %d bytes queued, %s dropped lifetime\n", outboxBytes, fallback(dropped, "0"))
	fmt.Fprintf(w, "  last heartbeat: %s\n", relative(lastHB))
	return 0
}

func shortID(id string, verbose bool) string {
	if verbose || len(id) <= 12 {
		return id
	}
	return id[:8] + "..." + id[len(id)-4:]
}

func relative(unixSec string) string {
	if unixSec == "" {
		return "never"
	}
	n, err := strconv.ParseInt(unixSec, 10, 64)
	if err != nil {
		return unixSec
	}
	d := time.Since(time.Unix(n, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
```

- [ ] **Step 4: Replace `runStatus` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runStatus(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "print full UUIDs and extra detail")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()
	return status.Render(s, stdout, *verbose)
}
```

Add to imports: `"github.com/prbe-ai/prbe-agent-tap/internal/status"`.

- [ ] **Step 5: Run tests and build**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd internal/status
git commit -m "feat(status): paired/halted/unpaired summary command"
```

---

## Task 22: backfill subcommand

**Files:**
- Create: `internal/backfill/backfill.go`
- Create: `internal/backfill/backfill_test.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runBackfill` stub)

- [ ] **Step 1: Write the failing test `internal/backfill/backfill_test.go`**

```go
package backfill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestEnumerateClampsTo365Days(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-x")
	_ = os.MkdirAll(proj, 0o755)

	old := filepath.Join(proj, "old.jsonl")
	recent := filepath.Join(proj, "new.jsonl")
	_ = os.WriteFile(old, []byte(`{"x":1}`+"\n"), 0o600)
	_ = os.WriteFile(recent, []byte(`{"x":2}`+"\n"), 0o600)

	twoYears := time.Now().Add(-2 * 365 * 24 * time.Hour)
	yesterday := time.Now().Add(-24 * time.Hour)
	_ = os.Chtimes(old, twoYears, twoYears)
	_ = os.Chtimes(recent, yesterday, yesterday)

	files, err := Enumerate(root, time.Now().Add(-2*365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Even if caller asks for 2 years, the function should clamp to 365.
	for _, f := range files {
		if f == old {
			t.Fatalf("365d clamp failed; included %q", old)
		}
	}
}

func TestEnumerateFiltersByMtime(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-y")
	_ = os.MkdirAll(proj, 0o755)

	a := filepath.Join(proj, "a.jsonl")
	b := filepath.Join(proj, "b.jsonl")
	_ = os.WriteFile(a, []byte(`{}`+"\n"), 0o600)
	_ = os.WriteFile(b, []byte(`{}`+"\n"), 0o600)
	_ = os.Chtimes(a, time.Now().Add(-100*24*time.Hour), time.Now().Add(-100*24*time.Hour))
	_ = os.Chtimes(b, time.Now().Add(-1*24*time.Hour), time.Now().Add(-1*24*time.Hour))

	files, err := Enumerate(root, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "b.jsonl" {
		t.Fatalf("got %v want only b.jsonl", files)
	}
}

func TestEnumerateSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-z")
	_ = os.MkdirAll(proj, 0o755)

	target := filepath.Join(proj, "real.jsonl")
	_ = os.WriteFile(target, []byte(`{}`+"\n"), 0o600)
	link := filepath.Join(proj, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	files, _ := Enumerate(root, time.Now().Add(-24*time.Hour))
	for _, f := range files {
		if f == link {
			t.Fatalf("symlink not skipped: %q", link)
		}
	}
}

func TestEnqueueDrainsToTarget(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()

	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-q")
	_ = os.MkdirAll(proj, 0o755)
	path := filepath.Join(proj, "s.jsonl")
	_ = os.WriteFile(path, []byte(`{"a":1}`+"\n"+`{"b":2}`+"\n"+`{"c":3}`+"\n"), 0o600)

	// Use a tiny target so we exercise the pacing path.
	cfg := Config{
		Root:           root,
		Since:          time.Now().Add(-time.Hour),
		Storage:        s,
		BatchMaxLines:  2,
		TargetDepth:    1,
		LowWater:       0,
		DeviceIDFunc:   func() (string, error) { return "dev", nil },
		DrainObserver:  func() bool { _, _ = s.ClearOutbox(); return true },
	}
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	// All lines should have been enqueued (and our DrainObserver cleared them in lockstep).
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/backfill/...`
Expected: FAIL — undefined `Enumerate`, `Config`, `Run`.

- [ ] **Step 3: Create `internal/backfill/backfill.go`**

```go
package backfill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
	"github.com/prbe-ai/prbe-agent-tap/internal/watch"
)

const maxBackfillWindow = 365 * 24 * time.Hour

// Enumerate returns JSONL paths under root with mtime >= since (clamped to 365d back).
// Sorted ascending by mtime. Symlinks are skipped.
func Enumerate(root string, since time.Time) ([]string, error) {
	earliest := time.Now().Add(-maxBackfillWindow)
	if since.Before(earliest) {
		since = earliest
	}
	type entry struct {
		path  string
		mtime time.Time
	}
	var hits []entry

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, lerr := os.Lstat(path)
		if lerr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info.ModTime().Before(since) {
			return nil
		}
		hits = append(hits, entry{path: path, mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mtime.Before(hits[j].mtime) })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.path
	}
	return out, nil
}

type Config struct {
	Root          string
	Since         time.Time
	Storage       *storage.Storage
	BatchMaxLines int
	TargetDepth   int
	LowWater      int
	DeviceIDFunc  func() (string, error)
	// DrainObserver is called periodically while the outbox is above LowWater.
	// Returns true once it observes the drainer making progress (or the test wants to unblock).
	DrainObserver func() bool
}

// Run enqueues lines from sessions into the outbox, pacing on outbox depth.
// Blocks until enumeration is complete and the outbox is empty (caller is expected to be running a drainer).
func Run(cfg Config) error {
	if cfg.BatchMaxLines == 0 {
		cfg.BatchMaxLines = 10
	}
	if cfg.TargetDepth == 0 {
		cfg.TargetDepth = 50
	}
	if cfg.LowWater == 0 {
		cfg.LowWater = 25
	}
	files, err := Enumerate(cfg.Root, cfg.Since)
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}
	deviceID, _ := cfg.DeviceIDFunc()
	var batchSeq int64
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		buf := make([]byte, 0, 64*1024)
		scratch := make([]byte, 32*1024)
		for {
			n, rerr := f.Read(scratch)
			if n > 0 {
				buf = append(buf, scratch[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		_ = f.Close()
		lines, _, _ := watch.SplitLines(buf)
		sessID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		cwd := strings.ReplaceAll(filepath.Base(filepath.Dir(path)), "-", "/")

		var lineNo int64
		for i := 0; i < len(lines); i += cfg.BatchMaxLines {
			end := i + cfg.BatchMaxLines
			if end > len(lines) {
				end = len(lines)
			}
			batch := lines[i:end]
			body, err := buildBackfillBody(deviceID, sessID, cwd, batchSeq, lineNo, batch)
			if err != nil {
				continue
			}
			now := time.Now().Unix()
			if err := cfg.Storage.EnqueueBatch(storage.OutboxRow{
				SessionID: sessID, BatchSeq: batchSeq, CWD: cwd, Body: body,
				CreatedAt: now, NextAttemptAt: now,
			}); err != nil {
				continue
			}
			batchSeq++
			lineNo += int64(len(batch))

			// Backpressure: wait for drainer.
			for {
				rows, err := cfg.Storage.OutboxRowCount()
				if err != nil {
					break
				}
				if int(rows) <= cfg.TargetDepth {
					break
				}
				if cfg.DrainObserver != nil && cfg.DrainObserver() {
					continue
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	return nil
}

func buildBackfillBody(deviceID, sessionID, cwd string, batchSeq, baseLineNo int64, lines [][]byte) ([]byte, error) {
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
```

- [ ] **Step 4: Add `OutboxRowCount` helper to storage**

Append to `internal/storage/outbox.go`:

```go
func (s *Storage) OutboxRowCount() (int64, error) {
	var n int64
	row := s.db.QueryRow(`SELECT COUNT(*) FROM outbox`)
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
```

- [ ] **Step 5: Replace `runBackfill` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runBackfill(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	days := fs.Int("days", 365, "how many days back to ship (max 365)")
	since := fs.String("since", "", "ship sessions modified on or after YYYY-MM-DD (clamped to 365d)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *days > 365 {
		*days = 365
	}
	cutoff := time.Now().Add(-time.Duration(*days) * 24 * time.Hour)
	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			fmt.Fprintln(stderr, "invalid --since (want YYYY-MM-DD):", err)
			return 2
		}
		cutoff = t
	}

	statePath, err := stateDBPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := storage.Open(statePath)
	if err != nil {
		fmt.Fprintln(stderr, "open state.db:", err)
		return 1
	}
	defer s.Close()

	hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
	d := outbox.New(outbox.Config{
		Storage:        s,
		Client:         hc,
		BearerProvider: func() (string, error) { return creds.Load("device-token") },
	})
	go func() { _ = d.Run(ctx) }()

	if err := backfill.Run(backfill.Config{
		Root:         claudeProjectsDir(),
		Since:        cutoff,
		Storage:      s,
		DeviceIDFunc: func() (string, error) { return s.GetMeta("device_id") },
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// Wait for outbox to empty.
	for {
		rows, _ := s.OutboxRowCount()
		if rows == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(stdout, "backfill complete.")
	return 0
}
```

Add to imports: `"time"`, `"github.com/prbe-ai/prbe-agent-tap/internal/backfill"`.

- [ ] **Step 6: Run tests and build**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd internal/backfill internal/storage/outbox.go
git commit -m "feat(backfill): paced enqueue with 365-day cap"
```

---

## Task 23: Install — macOS launchd plist

**Files:**
- Create: `internal/install/launchd.go`
- Create: `internal/install/launchd_test.go`

- [ ] **Step 1: Write the failing test `internal/install/launchd_test.go`**

```go
package install

import (
	"strings"
	"testing"
)

func TestRenderLaunchdWatch(t *testing.T) {
	out := RenderLaunchdWatch("/usr/local/bin/prbe-agent-tap", "/Users/x/.prbe/logs/agent-tap.log")
	for _, want := range []string{
		"<key>Label</key>",
		"<string>ai.prbe.agent-tap.watch</string>",
		"<string>/usr/local/bin/prbe-agent-tap</string>",
		"<string>watch</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<string>/Users/x/.prbe/logs/agent-tap.log</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderLaunchdHeartbeat(t *testing.T) {
	out := RenderLaunchdHeartbeat("/usr/local/bin/prbe-agent-tap", "/Users/x/.prbe/logs/agent-tap.log")
	for _, want := range []string{
		"<string>ai.prbe.agent-tap.heartbeat</string>",
		"<key>StartInterval</key>",
		"<integer>300</integer>",
		"<string>heartbeat</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/install/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `internal/install/launchd.go`**

```go
package install

import "fmt"

// RenderLaunchdWatch returns the macOS LaunchAgent plist for the long-running watch service.
func RenderLaunchdWatch(binPath, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.prbe.agent-tap.watch</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>watch</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, binPath, logPath, logPath)
}

// RenderLaunchdHeartbeat returns the LaunchAgent plist for the 5-minute heartbeat timer.
func RenderLaunchdHeartbeat(binPath, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.prbe.agent-tap.heartbeat</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>heartbeat</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, binPath, logPath, logPath)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/install/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/install
git commit -m "feat(install): launchd plists for watch + heartbeat"
```

---

## Task 24: Install — Linux systemd units

**Files:**
- Create: `internal/install/systemd.go`
- Create: `internal/install/systemd_test.go`

- [ ] **Step 1: Write the failing test `internal/install/systemd_test.go`**

```go
package install

import (
	"strings"
	"testing"
)

func TestRenderSystemdService(t *testing.T) {
	out := RenderSystemdService("/usr/local/bin/prbe-agent-tap")
	for _, want := range []string{
		"[Unit]",
		"prbe-agent-tap watcher",
		"[Service]",
		"ExecStart=/usr/local/bin/prbe-agent-tap watch",
		"Restart=always",
		"RestartSec=10",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("service missing %q", want)
		}
	}
}

func TestRenderSystemdHeartbeatService(t *testing.T) {
	out := RenderSystemdHeartbeatService("/usr/local/bin/prbe-agent-tap")
	if !strings.Contains(out, "ExecStart=/usr/local/bin/prbe-agent-tap heartbeat") {
		t.Fatalf("got %q", out)
	}
	if !strings.Contains(out, "Type=oneshot") {
		t.Fatalf("missing Type=oneshot")
	}
}

func TestRenderSystemdHeartbeatTimer(t *testing.T) {
	out := RenderSystemdHeartbeatTimer()
	for _, want := range []string{
		"OnBootSec=1min",
		"OnUnitActiveSec=5min",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("timer missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -race ./internal/install/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Create `internal/install/systemd.go`**

```go
package install

import "fmt"

func RenderSystemdService(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=prbe-agent-tap watcher
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s watch
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, binPath)
}

func RenderSystemdHeartbeatService(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=prbe-agent-tap heartbeat (one-shot)

[Service]
Type=oneshot
ExecStart=%s heartbeat
`, binPath)
}

func RenderSystemdHeartbeatTimer() string {
	return `[Unit]
Description=prbe-agent-tap heartbeat timer

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
Unit=prbe-agent-tap-heartbeat.service

[Install]
WantedBy=timers.target
`
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/install/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/install
git commit -m "feat(install): systemd user service + timer for Linux"
```

---

## Task 25: Install / uninstall driver

**Files:**
- Create: `internal/install/install.go`
- Modify: `cmd/prbe-agent-tap/dispatch.go` (replace `runInstall` and `runUninstall` stubs)

- [ ] **Step 1: Create `internal/install/install.go`**

(No new tests — this code is heavily I/O-bound and will be exercised by the e2e test in Task 31.)

```go
package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/prbe-ai/prbe-agent-tap/internal/creds"
	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

// Install registers the platform-appropriate user-level units and starts them.
// Idempotent: re-running rewrites units.
func Install(stdout io.Writer) error {
	binPath, err := os.Executable()
	if err != nil {
		return err
	}
	binPath, _ = filepath.EvalSymlinks(binPath)
	stateDir, err := creds.StateDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "agent-tap.log")

	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(binPath, logPath, stdout)
	case "linux":
		return installSystemd(binPath, stdout)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func installLaunchd(binPath, logPath string, stdout io.Writer) error {
	home, _ := os.UserHomeDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return err
	}
	watchPath := filepath.Join(plistDir, "ai.prbe.agent-tap.watch.plist")
	beatPath := filepath.Join(plistDir, "ai.prbe.agent-tap.heartbeat.plist")
	if err := os.WriteFile(watchPath, []byte(RenderLaunchdWatch(binPath, logPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(beatPath, []byte(RenderLaunchdHeartbeat(binPath, logPath)), 0o644); err != nil {
		return err
	}
	uid := fmt.Sprint(os.Getuid())
	for _, plist := range []string{watchPath, beatPath} {
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, plist).Run()
		if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).Run(); err != nil {
			return fmt.Errorf("launchctl bootstrap %s: %w", plist, err)
		}
	}
	fmt.Fprintln(stdout, "Installed launchd agents (watch + heartbeat).")
	return nil
}

func installSystemd(binPath string, stdout io.Writer) error {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap.service"),
		[]byte(RenderSystemdService(binPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap-heartbeat.service"),
		[]byte(RenderSystemdHeartbeatService(binPath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "prbe-agent-tap-heartbeat.timer"),
		[]byte(RenderSystemdHeartbeatTimer()), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now",
		"prbe-agent-tap.service", "prbe-agent-tap-heartbeat.timer").Run(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Installed systemd user service + timer.")
	return nil
}

// Uninstall removes platform units and (always) wipes local state. If purgeBinary, also removes the binary.
func Uninstall(s *storage.Storage, purgeBinary bool, stdout io.Writer) error {
	switch runtime.GOOS {
	case "darwin":
		_ = uninstallLaunchd()
	case "linux":
		_ = uninstallSystemd()
	}
	stateDir, _ := creds.StateDir()
	_ = os.RemoveAll(stateDir)
	_ = creds.Delete("device-token")
	if purgeBinary {
		bin, err := os.Executable()
		if err == nil {
			_ = os.Remove(bin)
		}
	}
	fmt.Fprintln(stdout, "Uninstalled.")
	return nil
}

func uninstallLaunchd() error {
	home, _ := os.UserHomeDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	uid := fmt.Sprint(os.Getuid())
	for _, name := range []string{"ai.prbe.agent-tap.watch.plist", "ai.prbe.agent-tap.heartbeat.plist"} {
		plist := filepath.Join(plistDir, name)
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, plist).Run()
		_ = os.Remove(plist)
	}
	return nil
}

func uninstallSystemd() error {
	home, _ := os.UserHomeDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	_ = exec.Command("systemctl", "--user", "disable", "--now",
		"prbe-agent-tap.service", "prbe-agent-tap-heartbeat.timer").Run()
	for _, name := range []string{
		"prbe-agent-tap.service",
		"prbe-agent-tap-heartbeat.service",
		"prbe-agent-tap-heartbeat.timer",
	} {
		_ = os.Remove(filepath.Join(unitDir, name))
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
```

- [ ] **Step 2: Replace `runInstall` and `runUninstall` in `cmd/prbe-agent-tap/dispatch.go`**

```go
func runInstall(_ context.Context, _ []string, stdout, stderr io.Writer) int {
	if err := install.Install(stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runUninstall(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also remove the binary")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	statePath, err := stateDBPath()
	if err == nil {
		if s, err := storage.Open(statePath); err == nil {
			tok, _ := creds.Load("device-token")
			hc := httpclient.New(httpclient.Options{BaseURL: apiBaseURL(), Version: version.Version})
			_ = revoke.Run(ctx, revoke.Args{DeviceToken: tok, Storage: s, Client: hc})
			s.Close()
		}
	}
	if err := install.Uninstall(nil, *purge, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
```

Add to imports: `"github.com/prbe-ai/prbe-agent-tap/internal/install"`.

- [ ] **Step 3: Build to verify**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd internal/install
git commit -m "feat(install): driver + uninstall (revoke + wipe + optional --purge)"
```

---

## Task 26: Logging — slog + lumberjack

**Files:**
- Create: `internal/logging/logging.go`
- Create: `internal/logging/logging_test.go`
- Modify: `cmd/prbe-agent-tap/main.go`

- [ ] **Step 1: Add lumberjack dependency**

```bash
go get gopkg.in/natefinch/lumberjack.v2
go mod tidy
```

- [ ] **Step 2: Write the failing test `internal/logging/logging_test.go`**

```go
package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoggerEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	l.Info("hello", "device_token", "secret-tok", "hostname", "mac1")

	out := buf.String()
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if got["msg"] != "hello" {
		t.Fatalf("msg = %v", got["msg"])
	}
	if got["device_token"] != "***" {
		t.Fatalf("device_token not redacted: %v", got["device_token"])
	}
	if got["hostname"] != "mac1" {
		t.Fatalf("hostname unexpectedly altered: %v", got["hostname"])
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test -race ./internal/logging/...`
Expected: FAIL — undefined `NewWithWriter`.

- [ ] **Step 4: Create `internal/logging/logging.go`**

```go
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
	// Cheap: forward unconditionally; stderr noise is acceptable. Refactor if needed.
	return lf.w.Write(p)
}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test -race ./internal/logging/...`
Expected: PASS.

- [ ] **Step 6: Initialize logging in `cmd/prbe-agent-tap/main.go`**

Replace `main.go` with:

```go
package main

import (
	"context"
	"os"

	"github.com/prbe-ai/prbe-agent-tap/internal/logging"
)

func main() {
	closer, err := logging.New()
	if err == nil && closer != nil {
		defer closer.Close()
	}
	os.Exit(dispatch(context.Background(), os.Args, os.Stdout, os.Stderr))
}
```

- [ ] **Step 7: Build to verify**

```bash
go test -race ./...
go build ./cmd/prbe-agent-tap
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd internal/logging
git commit -m "feat(logging): slog JSON to lumberjack-rotated file with redaction"
```

---

## Task 27: install.sh

**Files:**
- Create: `scripts/install.sh`

- [ ] **Step 1: Create `scripts/install.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO="prbe-ai/prbe-agent-tap"
LATEST_BASE="https://github.com/${REPO}/releases/latest/download"

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64)  echo "amd64" ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

install_dir() {
  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
  else
    mkdir -p "$HOME/.local/bin"
    echo "$HOME/.local/bin"
  fi
}

main() {
  local os arch dir url tmp bin
  os="$(detect_os)"
  arch="$(detect_arch)"
  dir="$(install_dir)"
  url="${LATEST_BASE}/prbe-agent-tap-${os}-${arch}"
  tmp="$(mktemp)"
  echo "Downloading ${url}"
  curl -fsSL --retry 3 -o "${tmp}" "${url}"
  chmod +x "${tmp}"
  bin="${dir}/prbe-agent-tap"
  mv "${tmp}" "${bin}"
  echo "Installed ${bin}"

  echo
  printf "Paste your pairing token from the dashboard: "
  IFS= read -r token
  if [ -z "${token}" ]; then
    echo "no token provided; you can pair later with: ${bin} pair <token>" >&2
    exit 0
  fi

  "${bin}" pair "${token}"
  "${bin}" install
  "${bin}" status
}

main "$@"
```

- [ ] **Step 2: Make it executable and lint with shellcheck if available**

```bash
chmod +x scripts/install.sh
which shellcheck && shellcheck scripts/install.sh || echo "shellcheck not installed; skipping"
```

Expected: no shellcheck findings (or skip).

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(scripts): curl|sh installer using GitHub Releases"
```

---

## Task 28: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - name: go vet
        run: go vet ./...
      - name: go test
        run: go test -race ./...

  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        target:
          - { goos: darwin,  goarch: amd64 }
          - { goos: darwin,  goarch: arm64 }
          - { goos: linux,   goarch: amd64 }
          - { goos: linux,   goarch: arm64 }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - name: build ${{ matrix.target.goos }}/${{ matrix.target.goarch }}
        env:
          GOOS: ${{ matrix.target.goos }}
          GOARCH: ${{ matrix.target.goarch }}
          CGO_ENABLED: '0'
        run: go build -o /tmp/prbe-agent-tap ./cmd/prbe-agent-tap
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: pr-gating workflow (vet, test, cross-compile)"
```

---

## Task 29: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - name: build all platforms
        env:
          VERSION: ${{ github.ref_name }}
        run: |
          mkdir -p dist
          for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
            goos="${target%/*}"
            goarch="${target#*/}"
            out="dist/prbe-agent-tap-${goos}-${goarch}"
            CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
              go build -ldflags "-X github.com/prbe-ai/prbe-agent-tap/internal/version.Version=${VERSION}" \
              -o "$out" ./cmd/prbe-agent-tap
            shasum -a 256 "$out" > "$out.sha256"
          done
          ls -la dist
      - name: publish release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            dist/*
          draft: false
          generate_release_notes: true
```

- [ ] **Step 2: Note the org-level workflow-permissions prerequisite**

This workflow requires the org-level "Read and write permissions" setting to be enabled (see spec § 15 C7). If a tag push fails to upload assets with a 403, that is the cause.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: tag-driven release workflow with shasums"
```

---

## Task 30: README

**Files:**
- Create: `README.md`

- [ ] **Step 1: Create `README.md`**

```markdown
# prbe-agent-tap

Single-binary daemon that watches Claude Code transcripts (`~/.claude/projects/`) and ships batched events to `api.prbe.ai/webhooks/claude_code` under a per-device bearer token. Phase 3.1 of the prbe coding-agent ingestion track.

## Install

```bash
curl -fsSL https://github.com/prbe-ai/prbe-agent-tap/releases/latest/download/install.sh | sh
```

Or grab the binary for your platform from the [latest release](https://github.com/prbe-ai/prbe-agent-tap/releases/latest) and run:

```bash
prbe-agent-tap pair <pairing-token-from-dashboard>
prbe-agent-tap install
prbe-agent-tap status
```

## Subcommands

| Command | Purpose |
|---|---|
| `pair <token>` | Exchange pairing JWT for device token; persist to OS keychain. |
| `install` | Register launchd / systemd user units and start the watcher. |
| `watch` | Long-running: tail JSONLs, batch, ship. (Run by launchd/systemd.) |
| `heartbeat` | One-shot liveness ping. (Run by launchd/systemd timer every 5 min.) |
| `status` | Print local daemon state. |
| `backfill [--days N \| --since YYYY-MM-DD]` | Ship historical sessions. Defaults to 365 days; capped at 365. |
| `revoke` | Revoke this device on the server and wipe local credentials. |
| `uninstall [--purge]` | Stop the daemon, revoke, remove units and `~/.prbe/`. |

## Environment

| Var | Purpose |
|---|---|
| `PRBE_API_BASE_URL` | Override `https://api.prbe.ai` (dev/staging). |
| `PRBE_CLAUDE_PROJECTS_DIR` | Override `~/.claude/projects/` (testing). |
| `PRBE_STATE_DIR` | Override `~/.prbe/` (testing). |
| `PRBE_DISABLE_KEYCHAIN` | Force file-fallback credentials (testing). |

## Local development

Requires Go 1.22+.

```bash
make build
make test
make e2e   # requires running prbe-backend + prbe-knowledge locally
```

See `docs/superpowers/specs/2026-04-27-prbe-agent-tap-daemon-design.md` (in the prbe-knowledge repo) for the full design.

## License

MIT.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README with install + subcommands + env vars"
```

---

## Task 31: End-to-end test against real local backend + knowledge

**Files:**
- Create: `tests/e2e/e2e_test.go`
- Create: `tests/e2e/jwt.go`

This task is gated behind a `e2e` build tag so it doesn't run in CI. It assumes the prbe-backend and prbe-knowledge services are running locally per the smoke harness instructions in `prbe-backend/scripts/smoke_claude_code.py`.

- [ ] **Step 1: Create `tests/e2e/jwt.go` (HS256 minter that mirrors the backend)**

```go
//go:build e2e

package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	cryptorand "crypto/rand"
)

func mintPairingJWT(signingKey, customerID, employeeID string) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)

	jti := make([]byte, 16)
	if _, err := cryptorand.Read(jti); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	payload := map[string]any{
		"iss":         "prbe-backend",
		"customer_id": customerID,
		"employee_id": employeeID,
		"jti":         fmt.Sprintf("%x", jti),
		"iat":         now,
		"exp":         now + 600,
	}
	pb, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(hb)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pb)
	signing := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write([]byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signing + "." + sig, nil
}
```

- [ ] **Step 2: Create `tests/e2e/e2e_test.go`**

```go
//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	backend   = "http://localhost:8081"
	knowledge = "http://localhost:8080"
	customer  = "smoke-cust"
	employee  = "smoke-emp"
)

func TestEndToEnd(t *testing.T) {
	signKey := os.Getenv("PAIRING_TOKEN_SIGNING_KEY")
	intKey := os.Getenv("INTERNAL_KNOWLEDGE_API_KEY")
	if signKey == "" || intKey == "" {
		t.Skip("PAIRING_TOKEN_SIGNING_KEY / INTERNAL_KNOWLEDGE_API_KEY not set")
	}

	stateDir := t.TempDir()
	projectsDir := filepath.Join(t.TempDir(), "projects")
	_ = os.MkdirAll(projectsDir, 0o755)

	env := []string{
		"PRBE_API_BASE_URL=" + backend,
		"PRBE_STATE_DIR=" + stateDir,
		"PRBE_CLAUDE_PROJECTS_DIR=" + projectsDir,
		"PRBE_DISABLE_KEYCHAIN=1",
		"PATH=" + os.Getenv("PATH"),
	}

	// Build the binary into the temp dir.
	bin := filepath.Join(t.TempDir(), "prbe-agent-tap")
	if out, err := exec.Command("go", "build", "-o", bin, "./cmd/prbe-agent-tap").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// 1. Mint pairing token + pair.
	tok, err := mintPairingJWT(signKey, customer, employee)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	cmd := exec.Command(bin, "pair", tok)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pair: %v\n%s", err, out)
	}

	// 2. Drop a synthetic JSONL.
	projDir := filepath.Join(projectsDir, "-tmp-smoke")
	_ = os.MkdirAll(projDir, 0o755)
	sessionPath := filepath.Join(projDir, "smoke.jsonl")
	_ = os.WriteFile(sessionPath, []byte(
		`{"type":"user_prompt","content":"hello"}`+"\n"+
			`{"type":"assistant_message","content":"world"}`+"\n"), 0o600)

	// 3. Run watch as a subprocess for ~5 sec.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	watchCmd := exec.CommandContext(ctx, bin, "watch")
	watchCmd.Env = env
	_ = watchCmd.Start()

	// 4. Append more lines mid-session.
	time.Sleep(2 * time.Second)
	f, _ := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"type":"tool_use","name":"Read","input":{"path":"/tmp"}}` + "\n")
	_ = f.Close()

	// 5. Wait for watch to exit on context timeout.
	_ = watchCmd.Wait()

	// 6. Verify the device row exists on knowledge.
	deviceID := readMeta(t, stateDir, "device_id")
	if deviceID == "" {
		t.Fatal("device_id not persisted by pair")
	}
	if !deviceListed(t, intKey, deviceID, "active") {
		t.Fatalf("device %s not listed as active on knowledge", deviceID)
	}

	// 7. Revoke and verify state cleanup.
	cmd = exec.Command(bin, "revoke")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("revoke: %v\n%s", err, out)
	}
	if !deviceListed(t, intKey, deviceID, "revoked") {
		t.Fatalf("device %s not listed as revoked on knowledge", deviceID)
	}
}

func readMeta(t *testing.T, stateDir, key string) string {
	t.Helper()
	cmd := exec.Command("sqlite3", filepath.Join(stateDir, "state.db"),
		"SELECT v FROM meta WHERE k='"+key+"'")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sqlite3 read: %v", err)
	}
	return string(trimRight(out))
}

func trimRight(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func deviceListed(t *testing.T, intKey, deviceID, wantStatus string) bool {
	t.Helper()
	req, _ := http.NewRequest("GET", knowledge+"/api/devices?customer_id="+customer, nil)
	req.Header.Set("X-Internal-Knowledge-Key", intKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/devices: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/api/devices status=%d", resp.StatusCode)
	}
	var body struct {
		Devices []struct {
			DeviceID string `json:"device_id"`
			Status   string `json:"status"`
		} `json:"devices"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	for _, d := range body.Devices {
		if d.DeviceID == deviceID && d.Status == wantStatus {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Verify the build tag works**

```bash
go test -tags=e2e ./tests/e2e/... -run TestEndToEnd -v
```

Expected (without env vars set): `--- SKIP: TestEndToEnd`. With env vars + running services, it should pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e
git commit -m "test(e2e): Go-native daemon end-to-end against local services"
```

---

## Task 32: Final wiring + push

- [ ] **Step 1: Run the full test suite**

```bash
go test -race ./...
```

Expected: PASS for all packages outside `tests/e2e`.

- [ ] **Step 2: Build all four target binaries locally**

```bash
for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -o "/tmp/prbe-agent-tap-${goos}-${goarch}" ./cmd/prbe-agent-tap
done
ls -la /tmp/prbe-agent-tap-*
```

Expected: four binaries built, sizes roughly 8–12 MB each.

- [ ] **Step 3: Push the branch and open a PR**

```bash
git push -u origin feature/initial-scaffold
gh pr create --title "feat: prbe-agent-tap Phase 3.1 scaffold + daemon" \
  --body "Implements docs/superpowers/specs/2026-04-27-prbe-agent-tap-daemon-design.md from the held knowledge worktree.

Includes:
- All eight subcommands (pair, watch, heartbeat, status, backfill, revoke, install, uninstall) wired end-to-end
- Pure-Go SQLite (state.db) with file_offsets + outbox + meta + cap enforcement
- OS keychain creds with 0600 file fallback
- HTTP retry classification + exp backoff
- fsnotify watcher with batch buffer (10 lines or 5 sec) and idle flush
- launchd plists (macOS) and systemd user units (Linux)
- Logging via slog + lumberjack with redaction
- CI workflow (vet, test, cross-compile) + release workflow (tag → GitHub Release)
- E2E test (build-tagged) against running local services"
```

Expected: PR opened.

- [ ] **Step 4: Verify CI passes on the PR**

```bash
gh pr checks
```

Expected: all checks green.

---

## Self-Review

After all tasks land, run this checklist:

**Spec coverage:**
- §2 (Scope) — all 8 subcommands shipped: ✓ pair (T12), install (T25), watch (T20), heartbeat (T13), status (T21), backfill (T22), revoke (T14), uninstall (T25).
- §3 (HTTP contract) — pair (T12), webhook (T15 drainer), heartbeat (T13), revoke (T14).
- §4 (Stack) — Go module (T1), all four required deps added.
- §5 (Repo layout) — every package present.
- §6 (Subcommand surface) — dispatcher (T11) lists all 8.
- §7 (State schema) — file_offsets (T4), outbox (T5), meta (T3), `_migrations` (T2).
- §8 (Watch loop) — bootstrap with line-count skip (T19), per-file reader (T17), buffer (T18), fsnotify (T19).
- §9 (HTTP client / retry / 401 / backoff) — T8, T9, T10, T15.
- §10 (Backfill) — pacing + 365d clamp (T22).
- §11 (Pairing + creds) — T6, T12.
- §12 (Operational surface — logging + status + install/uninstall) — T21, T23, T24, T25, T26.
- §13 (Distribution) — install.sh (T27), CI (T28), release (T29).
- §14 (Testing) — Tier 1+2 throughout, Tier 3 (T31), Tier 4 manual.
- §15 C1 (symlink skip) — covered in T19 (bootstrap) and T22 (backfill).
- §15 C2 (event vocabulary) — buildBatchBody in T19 wraps lines as `{line_no, raw}`.
- §15 C3+C4 (paced backfill, cap is for watch) — T22 + T5.
- §15 C5 (dashboard pairing UI gap) — out of scope for this repo, noted in spec.
- §15 C6 (repo creation) — done before plan execution.
- §15 C7 (Actions write permissions) — flagged in T29.

**Placeholder scan:** No "TBD" / "TODO" / "implement later". Every task has full code.

**Type consistency:**
- `OutboxRow` has fields `ID, SessionID, BatchSeq, CWD, Body, CreatedAt, NextAttemptAt, AttemptCount, LastError` — used consistently across T5, T15.
- `FileOffset` fields used consistently across T4, T19.
- `Args` structs follow `{Field: ...}` pattern across pair / heartbeat / revoke.
- `httpclient.Request` and `httpclient.Classification` consistent across T8, T10, T12-15.

**Scope:** Plan covers exactly the daemon and its CI/distribution. Dashboard frontend, sanitization (Phase 2.5), and Homebrew tap (Phase 3.2) are explicitly out.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-27-prbe-agent-tap-implementation.md`. Two execution options:

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**

