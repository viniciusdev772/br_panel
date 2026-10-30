// Package store is the panel's SQLite database: apps, settings and monitoring samples.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/viniciusdev772/br_panel/internal/settings"
)

const (
	StatusIdle      = "idle"
	StatusDeploying = "deploying"
	StatusDeployed  = "deployed"
	StatusFailed    = "failed"
)

var ErrExists = errors.New("já existe")

type App struct {
	Name       string
	Repo       string
	Branch     string
	BuildCmd   string
	StartCmd   string
	Port       int
	Domain     string
	AutoDeploy bool
	Env        string
	Status     string
	LastDeploy time.Time
}

// PortField is the port as shown in the form: empty when automatic.
func (a App) PortField() string {
	if a.Port == 0 {
		return ""
	}
	return strconv.Itoa(a.Port)
}

func (a App) Deploying() bool { return a.Status == StatusDeploying }

// migrations are applied in order; PRAGMA user_version records how many ran.
var migrations = []string{`
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE apps (
	name        TEXT PRIMARY KEY,
	repo        TEXT NOT NULL,
	branch      TEXT NOT NULL,
	build_cmd   TEXT NOT NULL DEFAULT '',
	start_cmd   TEXT NOT NULL,
	port        INTEGER NOT NULL DEFAULT 0,
	domain      TEXT NOT NULL DEFAULT '',
	auto_deploy INTEGER NOT NULL DEFAULT 0,
	env         TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'idle',
	last_deploy INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX apps_domain ON apps(domain) WHERE domain != '';
`, `
CREATE TABLE metrics (
	app TEXT NOT NULL,
	ts  INTEGER NOT NULL,
	cpu REAL NOT NULL,
	mem INTEGER NOT NULL,
	PRIMARY KEY (app, ts)
) WITHOUT ROWID;
`}

// Store is the panel's SQLite database: settings and apps.
type Store struct{ db *sql.DB }

// Open opens (creating, 0600) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Create the file first so it is never world-readable; -wal/-shm inherit its mode.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()

	u := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"_pragma": {
		"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)",
	}}.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	// One connection: SQLite has a single writer anyway and this avoids lock contention.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	// A deploy interrupted by a restart can never finish.
	_, err = db.Exec(`UPDATE apps SET status = ? WHERE status = ?`, StatusFailed, StatusDeploying)
	return s, err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return err
		}
		// PRAGMA does not accept bound parameters.
		if _, err := tx.Exec(`PRAGMA user_version = ` + strconv.Itoa(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const appCols = `name, repo, branch, build_cmd, start_cmd, port, domain, auto_deploy, env, status, last_deploy`

type rowScanner interface{ Scan(...any) error }

func scanApp(r rowScanner) (App, error) {
	var a App
	var auto, last int64
	err := r.Scan(&a.Name, &a.Repo, &a.Branch, &a.BuildCmd, &a.StartCmd, &a.Port, &a.Domain, &auto, &a.Env, &a.Status, &last)
	a.AutoDeploy = auto != 0
	if last > 0 {
		a.LastDeploy = time.Unix(last, 0)
	}
	return a, err
}

func (s *Store) List() []App {
	rows, err := s.db.Query(`SELECT ` + appCols + ` FROM apps ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var apps []App
	for rows.Next() {
		if a, err := scanApp(rows); err == nil {
			apps = append(apps, a)
		}
	}
	return apps
}

func (s *Store) Get(name string) (App, bool) {
	a, err := scanApp(s.db.QueryRow(`SELECT `+appCols+` FROM apps WHERE name = ?`, name))
	return a, err == nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create inserts a new app; ErrExists if the name or domain is already taken.
func (s *Store) Create(a App) error {
	_, err := s.db.Exec(`INSERT INTO apps (name, repo, branch, build_cmd, start_cmd, port, domain, auto_deploy, env, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Repo, a.Branch, a.BuildCmd, a.StartCmd, a.Port, a.Domain, b2i(a.AutoDeploy), a.Env, StatusIdle, time.Now().Unix())
	if isConstraint(err) {
		return ErrExists
	}
	return err
}

// UpdateConfig changes an app's settings without touching its deploy status.
func (s *Store) UpdateConfig(a App) error {
	_, err := s.db.Exec(`UPDATE apps SET repo=?, branch=?, build_cmd=?, start_cmd=?, port=?, domain=?, auto_deploy=?, env=? WHERE name=?`,
		a.Repo, a.Branch, a.BuildCmd, a.StartCmd, a.Port, a.Domain, b2i(a.AutoDeploy), a.Env, a.Name)
	if isConstraint(err) {
		return ErrExists
	}
	return err
}

func (s *Store) Delete(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM metrics WHERE app = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM apps WHERE name = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// BeginDeploy atomically marks the app as deploying; false if missing or already deploying.
func (s *Store) BeginDeploy(name string) (App, bool) {
	res, err := s.db.Exec(`UPDATE apps SET status = ? WHERE name = ? AND status != ?`, StatusDeploying, name, StatusDeploying)
	if err != nil {
		return App{}, false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return App{}, false
	}
	return s.Get(name)
}

func (s *Store) FinishDeploy(name string, ok bool) {
	st := StatusFailed
	if ok {
		st = StatusDeployed
	}
	_, _ = s.db.Exec(`UPDATE apps SET status = ?, last_deploy = ? WHERE name = ?`, st, time.Now().Unix(), name)
}

// DomainOwner returns the app using domain, if any.
func (s *Store) DomainOwner(domain string) (string, bool) {
	var n string
	err := s.db.QueryRow(`SELECT name FROM apps WHERE domain = ?`, domain).Scan(&n)
	return n, err == nil
}

func (s *Store) CountApps() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&n)
	return n
}

func isConstraint(err error) bool {
	return err != nil && strings.Contains(err.Error(), "constraint failed")
}

// ImportLegacy loads apps from the pre-SQLite apps.json (once), then renames it.
func (s *Store) ImportLegacy(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) || s.CountApps() > 0 {
		return nil
	}
	if err != nil {
		return err
	}
	var list []struct {
		Name       string `json:"name"`
		Repo       string `json:"repo"`
		Branch     string `json:"branch"`
		BuildCmd   string `json:"build_cmd"`
		StartCmd   string `json:"start_cmd"`
		Port       int    `json:"port"`
		Domain     string `json:"domain"`
		AutoDeploy bool   `json:"auto_deploy"`
		Env        string `json:"env"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	for _, l := range list {
		a := App{Name: l.Name, Repo: l.Repo, Branch: l.Branch, BuildCmd: l.BuildCmd, StartCmd: l.StartCmd,
			Port: l.Port, Domain: l.Domain, AutoDeploy: l.AutoDeploy, Env: l.Env}
		if err := s.Create(a); err != nil {
			return err
		}
	}
	return os.Rename(path, path+".migrated")
}

// Point is one monitoring sample: CPU in percent of one core, memory in bytes.
type Point struct {
	TS  int64   `json:"ts"`
	CPU float64 `json:"cpu"`
	Mem int64   `json:"mem"`
}

func (s *Store) InsertMetrics(app string, pts []Point) error {
	if len(pts) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range pts {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO metrics (app, ts, cpu, mem) VALUES (?, ?, ?, ?)`, app, p.TS, p.CPU, p.Mem); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Metrics returns samples since `since`, averaged (CPU) / maxed (memory) into buckets of `bucket` seconds.
func (s *Store) Metrics(app string, since time.Time, bucket int64) []Point {
	if bucket < 1 {
		bucket = 1
	}
	rows, err := s.db.Query(`SELECT (ts / ?) * ?, AVG(cpu), MAX(mem) FROM metrics
		WHERE app = ? AND ts >= ? GROUP BY ts / ? ORDER BY 1`, bucket, bucket, app, since.Unix(), bucket)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var pts []Point
	for rows.Next() {
		var p Point
		if rows.Scan(&p.TS, &p.CPU, &p.Mem) == nil {
			pts = append(pts, p)
		}
	}
	return pts
}

// LatestMetrics returns each app's newest sample taken at or after `since`.
func (s *Store) LatestMetrics(since time.Time) map[string]Point {
	out := map[string]Point{}
	rows, err := s.db.Query(`SELECT m.app, m.ts, m.cpu, m.mem FROM metrics m
		JOIN (SELECT app, MAX(ts) AS ts FROM metrics WHERE ts >= ? GROUP BY app) l ON l.app = m.app AND l.ts = m.ts`, since.Unix())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var app string
		var p Point
		if rows.Scan(&app, &p.TS, &p.CPU, &p.Mem) == nil {
			out[app] = p
		}
	}
	return out
}

func (s *Store) PruneMetrics(before time.Time) {
	_, _ = s.db.Exec(`DELETE FROM metrics WHERE ts < ?`, before.Unix())
}

// ---- settings ----

func (s *Store) LoadSettings() (settings.Settings, error) {
	set := settings.Default()
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return set, err
	}
	defer rows.Close()
	f := set.Fields()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return set, err
		}
		if p, ok := f[k]; ok {
			*p = v
		}
	}
	return set, rows.Err()
}

// SaveSettings writes all settings atomically.
func (s *Store) SaveSettings(set settings.Settings) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, p := range set.Fields() {
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, *p); err != nil {
			return err
		}
	}
	return tx.Commit()
}
