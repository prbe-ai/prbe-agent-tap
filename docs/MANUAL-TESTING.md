# Manual testing — prbe-agent-tap

This document walks you through verifying that the daemon actually works against your real running services (prbe-backend + prbe-knowledge), not just unit tests. Total time: about 20 minutes.

The plan: bring up the local services → mint a pairing JWT in-process → pair → run the watcher against a synthetic JSONL → check that the device + a document landed in knowledge → revoke → verify revocation.

---

## 0. Prerequisites

- macOS or Linux (Windows binary builds but isn't a supported target).
- Go 1.25+ (`go version` should report 1.25 or newer; `brew install go` if not).
- The backend + knowledge services running locally (terminals A and B below).
- `psql`, `sqlite3`, and `jq` on your PATH (most macOS dev machines have them).
- The customer row `smoke-cust` already exists in your dev Neon DB. If unsure:
  ```sql
  INSERT INTO customers(customer_id, display_name, api_key_hash)
  VALUES ('smoke-cust', 'smoke', 'smoke-hash')
  ON CONFLICT DO NOTHING;
  ```

---

## 1. Bring up the services (terminals A + B)

These are the same incantations as the existing Python smoke harness — copy/paste verbatim.

**Terminal A — prbe-knowledge:**

```bash
cd ~/Desktop/prbe/prbe-knowledge-worktrees/coding-agent-ingestion-v2
INTERNAL_KNOWLEDGE_API_KEY=test-internal-key \
  .venv/bin/uvicorn services.ingestion.main:app --port 8080
```

**Terminal B — prbe-backend:**

```bash
cd ~/Desktop/prbe/prbe-backend-worktrees/coding-agent-gateway

# Generate a signing key once and reuse for the rest of the session.
export PAIRING_TOKEN_SIGNING_KEY=$(python -c 'import secrets; print(secrets.token_hex(32))')
echo "PAIRING_TOKEN_SIGNING_KEY=$PAIRING_TOKEN_SIGNING_KEY"

INTERNAL_KNOWLEDGE_API_KEY=test-internal-key \
  KNOWLEDGE_INGESTION_BASE_URL=http://localhost:8080 \
  NEON_DATABASE_URL=postgresql://prbe:prbe@localhost:5432/prbe_knowledge \
  NEON_AUTH_BASE_URL=https://example.invalid/auth \
  PAIRING_TOKEN_SIGNING_KEY=$PAIRING_TOKEN_SIGNING_KEY \
  .venv/bin/uvicorn app.main:app --port 8081
```

**Sanity check** in a third terminal:

```bash
curl -sS http://localhost:8080/health
curl -sS http://localhost:8081/health
```

Both should respond with 200 and a JSON body. If not, fix the services before continuing.

---

## 2. Run the existing Python smoke first (optional but recommended)

Before testing the new daemon, confirm the server side itself is healthy by running the canonical smoke harness:

```bash
cd ~/Desktop/prbe/prbe-backend-worktrees/coding-agent-gateway
PAIRING_TOKEN_SIGNING_KEY=$PAIRING_TOKEN_SIGNING_KEY \
  INTERNAL_KNOWLEDGE_API_KEY=test-internal-key \
  .venv/bin/python scripts/smoke_claude_code.py
```

Expected last line: `ALL STEPS PASSED. End-to-end smoke OK.`

If this fails, the daemon won't fix it — investigate the services first.

---

## 3. Build the daemon

```bash
cd ~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold
make build
./prbe-agent-tap version
./prbe-agent-tap help
```

Expected: `prbe-agent-tap dev (darwin/arm64)` (or whatever your OS/arch is) and a list of all 8 subcommands.

---

## 4. Run the Go end-to-end test (the fastest way to verify everything wired correctly)

This is the equivalent of the Python smoke, but driven through the daemon binary:

```bash
cd ~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold
PAIRING_TOKEN_SIGNING_KEY=$PAIRING_TOKEN_SIGNING_KEY \
  INTERNAL_KNOWLEDGE_API_KEY=test-internal-key \
  make e2e
```

Expected: `--- PASS: TestEndToEnd` and exit 0. The test does the full pair → watch → revoke loop, mints the JWT in-process using the same signing key the backend uses, drops a synthetic JSONL into a tmpdir, asserts the device row landed in knowledge with `status=active`, then revokes and asserts `status=revoked`.

If this passes, the daemon is wire-compatible with the gateway. The remaining manual steps are about verifying the behaviors you can't easily assert from a test (notifications surface visibility, install/uninstall side effects, restart resume, etc.).

---

## 5. Manual smoke — the part you'd do in a real install

This walks through what an internal employee would actually do. We use the dev backend (port 8081) and a tmpdir for `~/.claude/projects/` so we don't pollute your real Claude Code sessions.

```bash
cd ~/Desktop/prbe/prbe-agent-tap-worktrees/initial-scaffold

# Isolated state + projects dir (so this doesn't touch your real ~/.claude or ~/.prbe).
export PRBE_API_BASE_URL=http://localhost:8081
export PRBE_STATE_DIR=$(mktemp -d)
export PRBE_CLAUDE_PROJECTS_DIR=$(mktemp -d)
export PRBE_DISABLE_KEYCHAIN=1   # store creds in ~/.prbe/credentials instead of macOS Keychain

echo "STATE: $PRBE_STATE_DIR"
echo "PROJECTS: $PRBE_CLAUDE_PROJECTS_DIR"
```

### 5a. Mint a pairing JWT (using the backend's helper)

The pairing JWT is normally minted by the dashboard. Until that UI exists, mint one in-process the same way the Python smoke does:

```bash
PAIRING_TOKEN=$(cd ~/Desktop/prbe/prbe-backend-worktrees/coding-agent-gateway && \
  PAIRING_TOKEN_SIGNING_KEY=$PAIRING_TOKEN_SIGNING_KEY \
  .venv/bin/python -c "from app.auth.pairing_tokens import mint; print(mint(customer_id='smoke-cust', employee_id='smoke-emp'))")

echo "$PAIRING_TOKEN" | head -c 40 ; echo "..."
```

### 5b. Pair

```bash
./prbe-agent-tap pair "$PAIRING_TOKEN"
```

Expected output:
```
Paired. device_id=<uuid>
Run `prbe-agent-tap install` to start watching.
```

### 5c. Inspect what was persisted

```bash
./prbe-agent-tap status
```

Expected:
```
prbe-agent-tap: paired
  device:        <truncated-uuid> (<your-hostname> · <macOS|linux>)
  customer:      smoke-cust
  paired:        just now
  ...
```

Local files:
```bash
ls -la "$PRBE_STATE_DIR"
sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT k, substr(v, 1, 30) FROM meta;"
cat "$PRBE_STATE_DIR/credentials" | jq .
```

You should see `device-token` in `credentials` and `device_id`, `customer_id`, `paired_at` in `meta`.

### 5d. Drop a synthetic Claude Code session

```bash
PROJ_DIR="$PRBE_CLAUDE_PROJECTS_DIR/-Users-smoke-test"
mkdir -p "$PROJ_DIR"
SESSION_ID="manual-$(uuidgen | tr '[:upper:]' '[:lower:]')"
SESSION_FILE="$PROJ_DIR/$SESSION_ID.jsonl"
touch "$SESSION_FILE"
echo "Session: $SESSION_ID"
echo "File:    $SESSION_FILE"
```

### 5e. Run the watcher in the foreground

In a separate terminal (or as a background process):

```bash
./prbe-agent-tap watch
```

You should see no output in the foreground (logs go to `$PRBE_STATE_DIR/logs/agent-tap.log`).

### 5f. Append events to the session file

Back in your shell, append a few lines and watch them get shipped:

```bash
echo '{"type":"user_prompt","content":"hello world"}' >> "$SESSION_FILE"
sleep 1
echo '{"type":"assistant_message","content":"hi back"}' >> "$SESSION_FILE"
sleep 1
echo '{"type":"tool_use","name":"Read","input":{"path":"/tmp"}}' >> "$SESSION_FILE"

# Within ~5-30 seconds the watcher should batch and ship these lines.
sleep 8

# Check the outbox is empty (drained) and last_successful_post_at is recent.
sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT COUNT(*) AS queued FROM outbox;"
sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT k, v FROM meta WHERE k IN ('last_successful_post_at', 'last_heartbeat_at');"
```

Expected: `queued = 0`, `last_successful_post_at` shows a recent unix timestamp.

### 5g. Verify the device + events landed in knowledge

```bash
DEVICE_ID=$(sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT v FROM meta WHERE k='device_id';")
echo "Looking for device_id: $DEVICE_ID"

# Device list
curl -sS http://localhost:8080/api/devices?customer_id=smoke-cust \
  -H "X-Internal-Knowledge-Key: test-internal-key" | jq '.devices[] | select(.device_id=="'"$DEVICE_ID"'")'

# Documents (the connector creates one claude_code.session document per session)
curl -sS http://localhost:8080/api/documents?customer_id=smoke-cust\&source_system=claude_code \
  -H "X-Internal-Knowledge-Key: test-internal-key" | jq '.documents[] | select(.metadata.session_id=="'"$SESSION_ID"'")'
```

The first query should show the device with `status: "active"` and metadata containing your hostname, OS, and `last_heartbeat_at` (if you also ran `heartbeat`, see step 5h).

The second query should show at least one document whose `metadata.session_id` matches your `$SESSION_ID`.

### 5h. Heartbeat manually (optional)

```bash
./prbe-agent-tap heartbeat
sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT k, v FROM meta WHERE k='last_heartbeat_at';"
```

Expected: `last_heartbeat_at` is a unix timestamp within the last few seconds.

### 5i. Stop the watcher (verify clean shutdown)

`Ctrl-C` the foreground `watch` process. The daemon should print no errors and exit cleanly. The C1 fix earlier in this PR ensures any in-flight buffered lines flush before the process dies.

### 5j. Verify restart resumes correctly (the C1 regression test in the wild)

This is the bug that the second-pass review caught: before the fix, restarts dropped any lines added while the daemon was down. Verify it works end-to-end:

```bash
# Append more lines while NO watcher is running.
echo '{"type":"user_prompt","content":"line written while down 1"}' >> "$SESSION_FILE"
echo '{"type":"user_prompt","content":"line written while down 2"}' >> "$SESSION_FILE"

# Restart the watcher (in a separate terminal again).
./prbe-agent-tap watch
```

After ~10 seconds, check that the new lines made it through:

```bash
sleep 10
sqlite3 "$PRBE_STATE_DIR/state.db" "SELECT COUNT(*) AS queued FROM outbox;"
# Should be 0 — the restart shipped the offline lines and they drained.

# In the events for that session, you should see line_no values for the offline-period lines.
curl -sS http://localhost:8080/api/documents?customer_id=smoke-cust\&source_system=claude_code \
  -H "X-Internal-Knowledge-Key: test-internal-key" | jq '.documents[] | select(.metadata.session_id=="'"$SESSION_ID"'")'
```

### 5k. Revoke

```bash
./prbe-agent-tap revoke
./prbe-agent-tap status
```

Expected: status returns "not paired" (or "halted") with exit 1; the credentials file no longer contains a `device-token`.

Confirm the gateway sees the revocation:

```bash
curl -sS http://localhost:8080/api/devices?customer_id=smoke-cust \
  -H "X-Internal-Knowledge-Key: test-internal-key" | jq '.devices[] | select(.device_id=="'"$DEVICE_ID"'") | .status'
```

Expected: `"revoked"`.

---

## 6. Edge cases worth poking

These aren't required for "does it work?" but they're the bugs that bite in production. Each takes about 30 seconds.

### Edge 1: 401 mid-stream

While the watcher is running and shipping happily, manually flip the device to revoked on the server side:

```bash
# Mark the device as revoked in knowledge directly.
curl -sS -X POST http://localhost:8080/api/devices/$DEVICE_ID/revoke \
  -H "X-Internal-Knowledge-Key: test-internal-key"

# The next webhook POST from the daemon will get 401.
# Append more events and watch them get rejected:
echo '{"type":"user_prompt","content":"after revoke"}' >> "$SESSION_FILE"
sleep 8
```

Expected: the watcher's logs show "halted: device token revoked", `last_401_at` is set in `state.db`, and the outbox has been cleared. `prbe-agent-tap status` reports halted.

### Edge 2: outbox cap hit

Take down the gateway and let the watcher buffer:

```bash
# In Terminal B, Ctrl-C the prbe-backend uvicorn process.
# Pour synthetic lines into the session file faster than ~100MB / line size.
# Easier: temporarily set the cap to a few KB and verify behavior.
```

This one is fiddly to test manually; the unit test `TestOutboxCapDropsOldest` in `internal/storage/outbox_test.go` covers the logic directly.

### Edge 3: install + launchd (macOS only, real installation)

If you want to test the install path that puts a real LaunchAgent in `~/Library/LaunchAgents/`:

**Important:** this installs under your user account and points launchd at the binary at its current path. Make sure you've moved the binary somewhere stable first (e.g. `/usr/local/bin/prbe-agent-tap`) — otherwise the plist points into your worktree, which gets confusing.

```bash
sudo cp ./prbe-agent-tap /usr/local/bin/
unset PRBE_API_BASE_URL PRBE_STATE_DIR PRBE_CLAUDE_PROJECTS_DIR PRBE_DISABLE_KEYCHAIN

# Now run pair against staging or a real backend if you want.
# Or stay local but expect the daemon to use real ~/.prbe/ and ~/.claude/projects/.
prbe-agent-tap install
launchctl list | grep prbe
ls -la ~/Library/LaunchAgents/ai.prbe.agent-tap.*
cat ~/.prbe/logs/agent-tap.log

# Cleanup:
prbe-agent-tap uninstall --purge
```

`uninstall --purge` removes the binary, the launchd plists, the keychain entry, `~/.prbe/`, and revokes the device server-side.

---

## 7. What "passing" looks like

The daemon is shippable for internal dogfood when:

- ✅ All four cross-compile targets build (CI verifies this on every push).
- ✅ `make test` all green (CI verifies).
- ✅ `make e2e` passes against running services (step 4 above).
- ✅ Steps 5b through 5k all behave as described.
- ✅ Step 5j confirms restart resume (the C1 fix's real-world verification).
- ✅ Edge 1 (401 mid-stream) gracefully halts the daemon.

---

## 8. Known gaps tracked as follow-ups

These came out of the second-pass code review and are documented for honesty, not blockers for internal dogfood. Each should become a GitHub issue:

- **C2 — Pair-time scan of in-progress sessions.** Spec §2 says the daemon should ship sessions whose `mtime ≥ pair-time`, but spec §8 says `watch` should not auto-ship existing-file content. The current code follows §8. Workaround: run `prbe-agent-tap backfill --days 1` after pairing to pick up any session you started before pairing.
- **I3 — Buffer byte cap missing.** The spec calls for a 5MB per-file cap; only line count is enforced today. Bound is fine in practice (10 lines × ~5KB = 50KB) but a runaway tool-output line could grow it.
- **I4 — `file_offsets` GC missing.** Stale offsets accumulate as sessions come and go. Not an immediate problem at any reasonable scale.
- **I5 — fsnotify Remove/Rename ignored.** File rotation is handled via inode/size reset on next read, but fast rotations could miss events.
- **I6 — `flushLocked` + `persistOffset` not transactional.** Possible duplicate events on power-loss between the two writes; server-side dedup absorbs them.
- **I8 — backfill `LowWater` field unused** (no hysteresis); cleanup-only.
- **I9 — backfill progress-to-stderr missing.** Long backfills give no progress signal.
- **I10 — `--since` not clamped at dispatch layer** (works in practice via `Enumerate` clamp).
- **I11 — heartbeat 401 doesn't proactively notify the running watcher** (drainer's own next attempt catches it within 500ms).
- **I12 — Line-number drift on malformed lines** (cosmetic in dashboard `line_no` display; server dedup unaffected).
- **Various Minors** (M1–M14): style/cleanup; see the full review in commit history if curious.
