// Package volume archives and restores the folders of app volumes as .tar.gz files and manages the
// backup files of an app. It uses the standard library only and no external program.
package volume

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Archive writes the contents of dir (not dir itself) to w as tar.gz. Directories, regular files
// and symbolic links are stored; links are never followed, and a file swapped for a link while the
// archive is being written is skipped, because the archive runs as root over a folder the app
// controls.
func Archive(dir string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		link := ""
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		case !info.Mode().IsRegular() && !info.IsDir():
			return nil // sockets, pipes, devices
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", "" // restored files go to the app user
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		// Closed per file, not deferred: a folder with thousands of files would run out of
		// descriptors before the walk ended.
		if st, err := f.Stat(); err != nil || !os.SameFile(info, st) {
			_ = f.Close()
			return fmt.Errorf("%s mudou durante o backup", rel)
		}
		_, cerr := io.CopyN(tw, f, hdr.Size)
		if err := errors.Join(cerr, f.Close()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Extract unpacks a tar.gz made by Archive into dir, which must already exist. Entries that try to
// leave dir, hard links and special files are refused. Symbolic links are created last, so no
// entry can be written through a link.
func Extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("não é um backup válido: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	type link struct{ path, target string }
	var links []link
	dirs := map[string]os.FileMode{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, err := safeName(hdr.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		p, err := safeJoin(dir, rel)
		if err != nil {
			return err
		}
		mode := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o700); err != nil {
				return err
			}
			dirs[p] = mode
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(f, tr)
			if err := errors.Join(cerr, f.Close(), os.Chmod(p, mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			links = append(links, link{p, hdr.Linkname})
		default:
			return fmt.Errorf("entrada não permitida no backup: %s", hdr.Name)
		}
	}
	for _, l := range links {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(l.target, l.path); err != nil {
			return err
		}
	}
	// Directory modes last: a read-only directory would block the files inside it.
	paths := make([]string, 0, len(dirs))
	for p := range dirs {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, p := range paths {
		if err := os.Chmod(p, dirs[p]); err != nil {
			return err
		}
	}
	return nil
}

// safeName turns an archive entry name into a clean relative path, or refuses it.
func safeName(name string) (string, error) {
	if strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return "", fmt.Errorf("nome não permitido no backup: %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", fmt.Errorf("nome não permitido no backup: %q", name)
		}
	}
	rel := filepath.FromSlash(strings.TrimSuffix(name, "/"))
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("nome não permitido no backup: %q", name)
	}
	if rel = filepath.Clean(rel); rel == "." {
		return "", nil
	}
	return rel, nil
}

// safeJoin returns dir/rel after checking that no component under dir is a symbolic link. A link
// planted in the folder (the app controls it) would otherwise send a write made by root outside it.
func safeJoin(dir, rel string) (string, error) {
	p := dir
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		p = filepath.Join(p, filepath.FromSlash(seg))
		fi, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("atalho no caminho do backup: %q", seg)
		}
	}
	return filepath.Join(dir, rel), nil
}

// ChownTree gives dir and everything inside it to the named system user, without following links.
// An empty user does nothing (apps running as root).
func ChownTree(dir, username string) error {
	if username == "" {
		return nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}

// ---- backup files ----

// Info describes one backup file of a volume.
type Info struct {
	File   string
	Volume string
	Size   int64
	Time   time.Time
}

const stamp = "20060102-150405"

var fileRe = regexp.MustCompile(`^([a-z][a-z0-9-]{0,31})-([0-9]{8}-[0-9]{6})\.tar\.gz$`)

// FileName is the name of a backup of volume made at t.
func FileName(vol string, t time.Time) string { return vol + "-" + t.Format(stamp) + ".tar.gz" }

// ParseFile returns the volume a backup file name belongs to, or false if it is not a backup name.
func ParseFile(name string) (vol string, ok bool) {
	m := fileRe.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// List returns the backups in dir, newest first.
func List(dir string) []Info {
	entries, _ := os.ReadDir(dir)
	var out []Info
	for _, en := range entries {
		vol, ok := ParseFile(en.Name())
		if !ok {
			continue
		}
		if fi, err := en.Info(); err == nil && fi.Mode().IsRegular() {
			out = append(out, Info{File: en.Name(), Volume: vol, Size: fi.Size(), Time: fi.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Time.After(out[j].Time) || (out[i].Time.Equal(out[j].Time) && out[i].File > out[j].File)
	})
	return out
}

// Prune keeps the newest keep backups of volume vol in dir and deletes the rest.
func Prune(dir, vol string, keep int) {
	n := 0
	for _, b := range List(dir) {
		if b.Volume != vol {
			continue
		}
		if n++; n > keep {
			_ = os.Remove(filepath.Join(dir, b.File))
		}
	}
}

// Newest returns the newest backup of vol, if any.
func Newest(dir, vol string) (Info, bool) {
	for _, b := range List(dir) {
		if b.Volume == vol {
			return b, true
		}
	}
	return Info{}, false
}
