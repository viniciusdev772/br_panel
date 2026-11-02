package store

import (
	"log"
	"time"
)

// Login sessions live here so a panel restart (every update) does not log the admin out. Only the
// SHA-256 of the session id is stored: a copy of the database cannot be turned into a valid cookie.

// SaveSession stores or replaces a session. Failures are logged, not returned: the session still
// works from memory until the next restart.
func (s *Store) SaveSession(idHash, csrf string, exp time.Time) {
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO sessions(id_hash, csrf, exp) VALUES(?,?,?)`, idHash, csrf, exp.Unix()); err != nil {
		log.Printf("sessão não gravada no banco: %v", err)
	}
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(idHash string) {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE id_hash = ?`, idHash)
}

// DeleteAllSessions removes every session (password change).
func (s *Store) DeleteAllSessions() {
	_, _ = s.db.Exec(`DELETE FROM sessions`)
}

// EachSession calls fn for every unexpired session and drops the expired ones.
func (s *Store) EachSession(fn func(idHash, csrf string, exp time.Time)) {
	type row struct {
		id, csrf string
		exp      int64
	}
	now := time.Now().Unix()
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE exp <= ?`, now)
	rows, err := s.db.Query(`SELECT id_hash, csrf, exp FROM sessions`)
	if err != nil {
		return
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.csrf, &r.exp) == nil {
			all = append(all, r)
		}
	}
	rows.Close() // single connection: close before fn can touch the database
	for _, r := range all {
		fn(r.id, r.csrf, time.Unix(r.exp, 0))
	}
}
