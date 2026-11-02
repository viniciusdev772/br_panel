package store

import (
	"errors"
	"time"
)

// ErrNotFound is returned when the row to change does not exist.
var ErrNotFound = errors.New("não encontrado")

// MaxTokens caps the access tokens, so a script that loops cannot fill the table.
const MaxTokens = 20

// Token is an access token of the remote CLI. Only the SHA-256 of the secret is stored; the secret
// itself is shown once, when it is created.
type Token struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"` // zero = never
}

func unixOrZero(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// CreateToken saves a token by the hash of its secret. ErrExists when the name is taken.
func (s *Store) CreateToken(name, hash string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO api_tokens (name, hash, created) VALUES (?, ?, ?)`, name, hash, time.Now().Unix())
	if isConstraint(err) {
		return 0, ErrExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListTokens returns the tokens, oldest first. The hash never leaves the store.
func (s *Store) ListTokens() []Token {
	rows, err := s.db.Query(`SELECT id, name, created, last_used FROM api_tokens ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var created, used int64
		if rows.Scan(&t.ID, &t.Name, &created, &used) == nil {
			t.Created, t.LastUsed = time.Unix(created, 0), unixOrZero(used)
			out = append(out, t)
		}
	}
	return out
}

// TokenByHash finds the token whose secret hashes to hash.
func (s *Store) TokenByHash(hash string) (Token, bool) {
	var t Token
	var created, used int64
	err := s.db.QueryRow(`SELECT id, name, created, last_used FROM api_tokens WHERE hash = ?`, hash).Scan(&t.ID, &t.Name, &created, &used)
	t.Created, t.LastUsed = time.Unix(created, 0), unixOrZero(used)
	return t, err == nil
}

// TouchToken records a use, at most once a minute to keep the database quiet.
func (s *Store) TouchToken(id int64) {
	now := time.Now().Unix()
	_, _ = s.db.Exec(`UPDATE api_tokens SET last_used = ? WHERE id = ? AND last_used < ?`, now, id, now-60)
}

// DeleteToken revokes a token; the next request that carries it is refused.
func (s *Store) DeleteToken(id int64) error {
	res, err := s.db.Exec(`DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
