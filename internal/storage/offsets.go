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
