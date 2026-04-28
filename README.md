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

Requires Go 1.25+ (driven by the `modernc.org/sqlite` dependency).

```bash
make build
make test
make e2e   # requires running prbe-backend + prbe-knowledge locally
```

See the design spec for the daemon at `2026-04-27-prbe-agent-tap-daemon-design.md` in the prbe-knowledge repo.

## License

MIT.
