// Package buildcache is the package cache that every app build shares (<data>/cache): npm, pnpm,
// yarn, bun, pip, Go, Cargo and Composer download each package once for all apps instead of once
// per app. It only knows folders and sizes; the deploy package points the tools at it.
package buildcache

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// tool is one cache folder: the sub-directory name, the label shown on screen and the environment
// variable that makes the tool use it (pip and the Go build cache share XDG_CACHE_HOME).
type tool struct{ dir, label, env string }

var tools = []tool{
	{"npm", "npm", "npm_config_cache"},
	{"pnpm", "pnpm", "npm_config_store_dir"},
	{"yarn", "Yarn", "YARN_CACHE_FOLDER"},
	{"bun", "Bun", "BUN_INSTALL_CACHE_DIR"},
	{"xdg", "pip e build do Go", "XDG_CACHE_HOME"},
	{"gomod", "módulos do Go", "GOMODCACHE"},
	{"cargo", "Cargo (Rust)", "CARGO_HOME"},
	{"composer", "Composer (PHP)", "COMPOSER_CACHE_DIR"},
}

// Item is the disk use of one tool's cache.
type Item struct {
	Name  string `json:"name"`  // sub-directory, e.g. "npm"
	Label string `json:"label"` // shown to people
	Bytes int64  `json:"bytes"`
}

// Cache is the shared cache folder. The zero value is not usable: build one with New.
type Cache struct {
	Dir string

	mu     sync.Mutex
	items  []Item
	loaded time.Time
}

// sizeTTL keeps the screens and `brpanel status` (polled every few seconds) from walking
// gigabytes of small files each time.
const sizeTTL = 30 * time.Second

// New returns the cache that lives in <data>/cache.
func New(dataDir string) *Cache { return &Cache{Dir: filepath.Join(dataDir, "cache")} }

// Env lists the variables that point every supported tool at the shared cache. They are for the
// build only: a running app does not get them.
func (c *Cache) Env() []string {
	env := make([]string, 0, len(tools))
	for _, t := range tools {
		env = append(env, t.env+"="+filepath.Join(c.Dir, t.dir))
	}
	return env
}

// Ensure creates the cache folders and hands them to owner (an account name, "" to leave the
// owner alone), so the unprivileged build can write there. It runs as root, like the panel.
func (c *Cache) Ensure(owner string) error {
	uid, gid := -1, -1
	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			return err
		}
		uid, _ = strconv.Atoi(u.Uid)
		gid, _ = strconv.Atoi(u.Gid)
	}
	dirs := []string{c.Dir}
	for _, t := range tools {
		dirs = append(dirs, filepath.Join(c.Dir, t.dir))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		if uid >= 0 {
			if err := os.Lchown(d, uid, gid); err != nil {
				return err
			}
		}
	}
	return nil
}

// Items returns the size of each tool's cache (zero for folders that do not exist yet), largest
// first. The result is remembered for a few seconds.
func (c *Cache) Items() []Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items != nil && time.Since(c.loaded) < sizeTTL {
		return append([]Item(nil), c.items...)
	}
	items := make([]Item, 0, len(tools))
	for _, t := range tools {
		items = append(items, Item{Name: t.dir, Label: t.label, Bytes: dirSize(filepath.Join(c.Dir, t.dir))})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Bytes > items[j].Bytes })
	c.items, c.loaded = items, time.Now()
	return append([]Item(nil), items...)
}

// Total is the sum of Items.
func (c *Cache) Total() int64 {
	var n int64
	for _, it := range c.Items() {
		n += it.Bytes
	}
	return n
}

func (c *Cache) forget() {
	c.mu.Lock()
	c.items = nil
	c.mu.Unlock()
}

// Prune deletes the files nobody used for at least olderThan and the folders that became empty.
// "Used" is the later of the modification and the access time (relatime, the Linux default,
// refreshes the access time of a file that is read after a day), so a package an app still
// installs is kept. Whatever is deleted by mistake is only downloaded again. It returns the
// files removed and the bytes freed. Callers must not run it during a build.
//
// The panel runs as root while the app user owns this folder, so every deletion goes through an
// os.Root: a link planted (or swapped in mid-walk) to point outside the cache is never followed.
func (c *Cache) Prune(olderThan time.Duration) (files int, freed int64, err error) {
	defer c.forget()
	root, err := os.OpenRoot(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	defer root.Close()
	cutoff := time.Now().Add(-olderThan)
	fsys := root.FS()
	for _, t := range tools {
		_ = fs.WalkDir(fsys, t.dir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil || lastUsed(info).After(cutoff) {
				return nil
			}
			if root.Remove(p) == nil {
				files++
				freed += info.Size()
			}
			return nil
		})
		removeEmptyDirs(root, t.dir)
	}
	return files, freed, nil
}

// Clear deletes everything in the cache and returns the bytes freed. Callers must not run it
// during a build.
func (c *Cache) Clear() (int64, error) {
	defer c.forget()
	root, err := os.OpenRoot(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer root.Close()
	var freed int64
	for _, t := range tools {
		freed += dirSize(filepath.Join(c.Dir, t.dir))
		if err := root.RemoveAll(t.dir); err != nil {
			return freed, err
		}
	}
	return freed, nil
}

// lastUsed is the later of the modification and access times.
func lastUsed(info fs.FileInfo) time.Time {
	t := info.ModTime()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if a := time.Unix(int64(st.Atim.Sec), int64(st.Atim.Nsec)); a.After(t) {
			t = a
		}
	}
	return t
}

// removeEmptyDirs removes the empty directories under dir (relative to root), deepest first, but
// never dir itself.
func removeEmptyDirs(root *os.Root, dir string) {
	var dirs []string
	_ = fs.WalkDir(root.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = root.Remove(dirs[i]) // fails (and is kept) when it still has files
	}
}

// dirSize adds up the regular files under dir; symbolic links are not followed.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}
