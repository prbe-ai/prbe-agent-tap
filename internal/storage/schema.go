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
