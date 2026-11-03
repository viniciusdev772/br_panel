package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/validate"
	"github.com/viniciusdev772/br_panel/internal/volume"
)

const (
	// maxVolumeBackups is how many backups of each volume are kept, like the databases' 14.
	maxVolumeBackups = 7
	// restoreSuffix names the staging folder used while a volume is restored.
	restoreSuffix = ".restore"
	// volumeBackupTimeout bounds one automatic backup or restore.
	volumeBackupTimeout = 30 * time.Minute
)

var errVolumeBusy = errors.New("já existe um backup ou uma restauração de volume em andamento")

// mountSetup is what the mounts of an app contribute to every unit and to its environment.
type mountSetup struct {
	Bind     []string    // BindPaths= lines, "source:target"
	ReadOnly []string    // BindReadOnlyPaths= lines (file mounts)
	Env      [][2]string // VOLUME_<NAME> = real path
}

// prepareMounts creates the folders, files and mount points an app needs and returns what the
// units must mount. It runs on every deploy and on every scale-out: the instances share the same
// storage, so all of them get the same list.
func (d *Deployer) prepareMounts(ctx context.Context, a store.App) (mountSetup, error) {
	var m mountSetup
	src := d.Paths.SrcDir(a.Name)
	for _, mo := range d.Store.ListMounts(a.Name) {
		// Validated again on purpose: the row may predate a rule change, and everything here
		// ends up in a unit file rendered by systemd.
		if err := validate.Mount(mo.Kind, mo.Name, mo.Source, mo.Target, mo.Content, d.DataDir); err != nil {
			return m, fmt.Errorf("armazenamento %q: %w", mo.Name, err)
		}
		var source string
		switch mo.Kind {
		case store.MountVolume:
			source = d.Paths.VolumeDir(a.Name, mo.Name)
			if err := os.MkdirAll(source, 0o750); err != nil {
				return m, err
			}
			// MkdirAll made the parent (volumes/<app>) root-only too, but the app reaches its folder
			// by this real path ($VOLUME_<NAME>): the parent must be traversable, not listable.
			if err := os.Chmod(d.Paths.VolumesDir(a.Name), 0o711); err != nil {
				return m, err
			}
			if err := d.chownMount(ctx, source); err != nil {
				return m, err
			}
		case store.MountBind:
			fi, err := os.Stat(mo.Source)
			if err != nil {
				return m, fmt.Errorf("a pasta %q da montagem %q não existe", mo.Source, mo.Name)
			}
			if !fi.IsDir() {
				return m, fmt.Errorf("%q não é uma pasta", mo.Source)
			}
			source = mo.Source
		case store.MountFile:
			source = d.Paths.FileMount(a.Name, mo.ID)
			if err := writeFile(source, mo.Content, 0o600); err != nil {
				return m, err
			}
			// Read-only for the service, but it still has to be able to read a file that may
			// hold a secret.
			if err := d.chownMount(ctx, source); err != nil {
				return m, err
			}
		}
		target := mo.Target
		if !filepath.IsAbs(target) {
			target = filepath.Join(src, target)
			// The checkout belongs to the repository: a committed symlink would make root
			// create the mount point somewhere else.
			if err := noSymlinks(src, target); err != nil {
				return m, fmt.Errorf("montagem %q: %w", mo.Name, err)
			}
		}
		if err := makeMountTarget(target, mo.Kind == store.MountFile); err != nil {
			return m, fmt.Errorf("montagem %q: %w", mo.Name, err)
		}
		if mo.Kind == store.MountFile {
			m.ReadOnly = append(m.ReadOnly, source+":"+target)
		} else {
			m.Bind = append(m.Bind, source+":"+target)
		}
		m.Env = append(m.Env, [2]string{validate.MountEnvName(mo.Name), source})
	}
	return m, nil
}

// noSymlinks refuses target when any component below base (target included) is a symbolic link.
func noSymlinks(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	p := base
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		p = filepath.Join(p, seg)
		fi, err := os.Lstat(p)
		if os.IsNotExist(err) {
			return nil // nothing below exists yet, so nothing can redirect
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%q é um atalho no repositório; o destino da montagem não pode passar por ele", seg)
		}
	}
	return nil
}

// makeMountTarget creates the destination before the unit starts: systemd refuses to start when a
// BindPaths= destination is missing, and git does not create it either.
func makeMountTarget(path string, file bool) error {
	fi, err := os.Stat(path)
	if err == nil {
		if file && fi.IsDir() {
			return fmt.Errorf("%q é uma pasta; escolha um nome de arquivo", path)
		}
		if !file && !fi.IsDir() {
			return fmt.Errorf("%q já existe e não é uma pasta", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if file {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, nil, 0o600)
	}
	return os.MkdirAll(path, 0o755)
}

// chownMount hands one path to the app user (root owns nothing inside a volume).
func (d *Deployer) chownMount(ctx context.Context, path string) error {
	if user := d.Settings().AppUser; user != "" {
		return chownTo(ctx, user, path)
	}
	return nil
}

// ---- volume backups ----

func (d *Deployer) beginVolume(app, op string) bool {
	d.volMu.Lock()
	defer d.volMu.Unlock()
	if d.volBusy == nil { // a Deployer built without New, like in some tests
		d.volBusy = map[string]string{}
	}
	if d.volBusy[app] != "" {
		return false
	}
	d.volBusy[app] = op
	return true
}

func (d *Deployer) endVolume(app string) {
	d.volMu.Lock()
	delete(d.volBusy, app)
	d.volMu.Unlock()
}

// VolumeBusy returns the volume operation running on the app (for the page), if any.
func (d *Deployer) VolumeBusy(app string) string {
	d.volMu.Lock()
	defer d.volMu.Unlock()
	return d.volBusy[app]
}

// volumeMounts returns the app's mounts of kind volume.
func (d *Deployer) volumeMounts(app string) []store.Mount {
	var out []store.Mount
	for _, mo := range d.Store.ListMounts(app) {
		if mo.Kind == store.MountVolume {
			out = append(out, mo)
		}
	}
	return out
}

// VolumeBackups lists the app's volume backups, newest first.
func (d *Deployer) VolumeBackups(app string) []volume.Info {
	return volume.List(d.Paths.VolumeBackupDir(app))
}

// VolumeBackupPath returns the path of one backup of the app after validating the file name.
func (d *Deployer) VolumeBackupPath(app, file string) (string, bool) {
	if _, ok := volume.ParseFile(file); !ok {
		return "", false
	}
	p := filepath.Join(d.Paths.VolumeBackupDir(app), file)
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return p, true
}

// DeleteVolumeBackup removes one backup file of the app.
func (d *Deployer) DeleteVolumeBackup(app, file string) error {
	p, ok := d.VolumeBackupPath(app, file)
	if !ok {
		return errors.New("backup não encontrado")
	}
	return os.Remove(p)
}

// BackupVolumes copies every volume of the app to a .tar.gz in the app's backup folder, in the
// background. reason is "manual" or "agendado".
func (d *Deployer) BackupVolumes(app, reason string) error {
	vols := d.volumeMounts(app)
	if len(vols) == 0 {
		return errors.New("a aplicação não tem nenhum volume para copiar")
	}
	if !d.beginVolume(app, "backup") {
		return errVolumeBusy
	}
	go func() {
		defer d.endVolume(app)
		log, done := d.volumeLog(app)
		defer done()
		fmt.Fprintf(log, "\n== backup dos volumes (%s) em %s ==\n", reason, time.Now().Format(time.RFC3339))
		dir := d.Paths.VolumeBackupDir(app)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			fmt.Fprintf(log, "FALHOU: %v\n", err)
			d.record(app, "Backup dos volumes falhou")
			return
		}
		now := time.Now()
		for _, mo := range vols {
			src := d.Paths.VolumeDir(app, mo.Name)
			if err := archiveVolume(src, filepath.Join(dir, volume.FileName(mo.Name, now))); err != nil {
				fmt.Fprintf(log, "%s: FALHOU: %v\n", mo.Name, err)
				d.record(app, "Backup do volume "+mo.Name+" falhou")
				continue
			}
			volume.Prune(dir, mo.Name, maxVolumeBackups)
			fmt.Fprintf(log, "%s: backup concluído\n", mo.Name)
			d.record(app, "Backup do volume "+mo.Name+" concluído ("+reason+")")
		}
	}()
	return nil
}

// archiveVolume writes dir to dst through a temporary name, so a half-written file is never
// listed as a backup.
func archiveVolume(dir, dst string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return errors.New("a pasta ainda não existe; faça um deploy antes")
	}
	if !fi.IsDir() {
		return errors.New("o volume não é uma pasta")
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := volume.Archive(dir, f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := errors.Join(f.Close(), os.Chmod(tmp, 0o600)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// RestoreVolume replaces one volume of the app with a backup, in the background. The instances are
// stopped and started again: a running service keeps the old folder mounted.
func (d *Deployer) RestoreVolume(app, file string) error {
	path, ok := d.VolumeBackupPath(app, file)
	if !ok {
		return errors.New("backup não encontrado")
	}
	vol, _ := volume.ParseFile(file)
	found := false
	for _, mo := range d.volumeMounts(app) {
		if mo.Name == vol {
			found = true
		}
	}
	if !found {
		return errors.New("esse volume não existe mais na aplicação")
	}
	if !d.beginVolume(app, "restauração") {
		return errVolumeBusy
	}
	go func() {
		defer d.endVolume(app)
		log, done := d.volumeLog(app)
		defer done()
		ctx, cancel := context.WithTimeout(context.Background(), volumeBackupTimeout)
		defer cancel()
		fmt.Fprintf(log, "\n== restaurando o volume %s de %s ==\n", vol, file)
		if err := d.restoreVolume(ctx, app, vol, path, log); err != nil {
			fmt.Fprintf(log, "FALHOU: %v\n", err)
			d.record(app, "Restauração do volume "+vol+" falhou")
			return
		}
		d.record(app, "Volume "+vol+" restaurado de "+file)
	}()
	return nil
}

func (d *Deployer) restoreVolume(ctx context.Context, app, vol, backup string, log io.Writer) error {
	dir := d.Paths.VolumeDir(app, vol)
	tmp := dir + restoreSuffix
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return err
	}
	f, err := os.Open(backup)
	if err != nil {
		return err
	}
	err = volume.Extract(f, tmp)
	_ = f.Close()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if user := d.Settings().AppUser; user != "" {
		if err := volume.ChownTree(tmp, user); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	a, _ := d.Store.Get(app)
	units := systemd.ReplicaUnits(app, a.Replicas)
	_, _ = systemd.Ctl(append([]string{"stop"}, units...)...)
	fmt.Fprintln(log, "instâncias paradas para trocar a pasta")
	old := dir + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(dir, old); err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(tmp)
		_, _ = systemd.Ctl(append([]string{"start"}, units...)...)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.Rename(old, dir)
		_, _ = systemd.Ctl(append([]string{"start"}, units...)...)
		return err
	}
	_ = os.RemoveAll(old)
	_, err = systemd.Ctl(append([]string{"start"}, units...)...)
	fmt.Fprintln(log, "instâncias iniciadas de novo")
	return err
}

// RunVolumeScheduler takes the scheduled volume backups (daily/weekly) while ctx is alive.
func (d *Deployer) RunVolumeScheduler(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.scheduledVolumeBackups(time.Now())
		}
	}
}

func (d *Deployer) scheduledVolumeBackups(now time.Time) {
	for _, a := range d.Store.List() {
		interval := map[string]time.Duration{"daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour}[d.Store.VolumeBackup(a.Name)]
		if interval == 0 || d.VolumeBusy(a.Name) != "" || len(d.volumeMounts(a.Name)) == 0 {
			continue
		}
		if b := d.VolumeBackups(a.Name); len(b) > 0 && now.Sub(b[0].Time) < interval-time.Hour {
			continue
		}
		_ = d.BackupVolumes(a.Name, "agendado") // busy or nothing to copy: next tick
	}
}

func (d *Deployer) volumeLog(app string) (io.Writer, func()) {
	f, err := os.OpenFile(d.Paths.VolumeLogFile(app), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return os.Stderr, func() {}
	}
	return f, func() { _ = f.Close() }
}

func (d *Deployer) record(app, msg string) {
	if d.Store != nil {
		d.Store.AddEvent("backup", app, msg)
	}
}
