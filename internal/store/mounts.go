package store

// Kinds of storage an app can have (see validate.Mount).
const (
	MountVolume = "volume" // folder managed by the panel, kept across deploys
	MountBind   = "bind"   // existing folder of the server
	MountFile   = "file"   // text kept in the database, written to a file and mounted read-only
)

// Mount is one storage entry of an app. Target is relative to the project directory or absolute.
type Mount struct {
	ID      int64
	App     string
	Name    string
	Kind    string
	Source  string // bind only
	Target  string
	Content string // file only
}

const mountCols = `id, app, name, kind, source, target, content`

// ListMounts returns the storage of an app, in creation order.
func (s *Store) ListMounts(app string) []Mount {
	rows, err := s.db.Query(`SELECT `+mountCols+` FROM mounts WHERE app = ? ORDER BY id`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Mount
	for rows.Next() {
		var m Mount
		if rows.Scan(&m.ID, &m.App, &m.Name, &m.Kind, &m.Source, &m.Target, &m.Content) == nil {
			out = append(out, m)
		}
	}
	return out
}

// AddMount inserts a mount; ErrExists when the app already has that name or target.
func (s *Store) AddMount(m Mount) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO mounts (app, name, kind, source, target, content) VALUES (?, ?, ?, ?, ?, ?)`,
		m.App, m.Name, m.Kind, m.Source, m.Target, m.Content)
	if isConstraint(err) {
		return 0, ErrExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteMount removes one mount of an app (the folder on disk is not touched).
func (s *Store) DeleteMount(app string, id int64) error {
	_, err := s.db.Exec(`DELETE FROM mounts WHERE app = ? AND id = ?`, app, id)
	return err
}

// VolumeBackup is the schedule of the volume backups of an app: off, daily or weekly.
func (s *Store) VolumeBackup(app string) string {
	var v string
	if s.db.QueryRow(`SELECT volume_backup FROM apps WHERE name = ?`, app).Scan(&v) != nil || v == "" {
		return "off"
	}
	return v
}

// SetVolumeBackup saves the volume backup schedule of an app.
func (s *Store) SetVolumeBackup(app, schedule string) error {
	_, err := s.db.Exec(`UPDATE apps SET volume_backup = ? WHERE name = ?`, schedule, app)
	return err
}
