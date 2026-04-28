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

func (s *Storage) EnforceOutboxCap(maxBytes int64) (int, error) {
	total, err := s.OutboxByteSize()
	if err != nil {
		return 0, err
	}
	if total <= maxBytes {
		return 0, nil
	}
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
