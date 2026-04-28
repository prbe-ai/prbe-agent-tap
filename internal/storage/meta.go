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
