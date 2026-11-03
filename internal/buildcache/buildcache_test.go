package buildcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestEnvPointsEveryToolInsideTheCache(t *testing.T) {
	c := New("/data")
	got := strings.Join(c.Env(), "\n")
	for _, want := range []string{
		"npm_config_cache=/data/cache/npm", "npm_config_store_dir=/data/cache/pnpm", "YARN_CACHE_FOLDER=/data/cache/yarn",
		"XDG_CACHE_HOME=/data/cache/xdg", "GOMODCACHE=/data/cache/gomod", "CARGO_HOME=/data/cache/cargo",
		"COMPOSER_CACHE_DIR=/data/cache/composer", "BUN_INSTALL_CACHE_DIR=/data/cache/bun",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q em %s", want, got)
		}
	}
}

func TestEnsureCreatesTheFolders(t *testing.T) {
	c := New(t.TempDir())
	if err := c.Ensure(""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"npm", "xdg", "gomod", "cargo"} {
		if fi, err := os.Stat(filepath.Join(c.Dir, name)); err != nil || !fi.IsDir() {
			t.Errorf("%s não foi criada: %v", name, err)
		}
	}
	if err := c.Ensure("usuario-que-nao-existe-brpanel"); err == nil {
		t.Error("um dono inexistente deveria dar erro")
	}
}

func TestItemsSizesAndSorting(t *testing.T) {
	c := New(t.TempDir())
	write(t, filepath.Join(c.Dir, "npm", "a", "f1"), 1000, 0)
	write(t, filepath.Join(c.Dir, "npm", "b", "f2"), 500, 0)
	write(t, filepath.Join(c.Dir, "cargo", "x"), 4000, 0)
	items := c.Items()
	if items[0].Name != "cargo" || items[0].Bytes != 4000 || items[1].Name != "npm" || items[1].Bytes != 1500 {
		t.Fatalf("itens: %+v", items)
	}
	if c.Total() != 5500 {
		t.Errorf("total = %d", c.Total())
	}
	// remembered: a new file does not show until the cache is told it changed
	write(t, filepath.Join(c.Dir, "npm", "c"), 100, 0)
	if c.Total() != 5500 {
		t.Error("o tamanho deveria vir da memória por alguns segundos")
	}
	c.forget()
	if c.Total() != 5600 {
		t.Errorf("depois de esquecer, total = %d", c.Total())
	}
}

func TestPruneKeepsRecentFilesAndDropsEmptyDirs(t *testing.T) {
	c := New(t.TempDir())
	old, fresh := filepath.Join(c.Dir, "npm", "velho", "a"), filepath.Join(c.Dir, "npm", "novo", "b")
	write(t, old, 300, 90*24*time.Hour)
	write(t, fresh, 200, time.Hour)
	files, freed, err := c.Prune(30 * 24 * time.Hour)
	if err != nil || files != 1 || freed != 300 {
		t.Fatalf("files=%d freed=%d err=%v", files, freed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("o arquivo antigo deveria ter sido apagado")
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Error("a pasta que ficou vazia deveria ter sido apagada")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("o arquivo recente deveria ficar")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "npm")); err != nil {
		t.Error("a pasta raiz da ferramenta não pode ser apagada")
	}
}

func TestClearRemovesEverything(t *testing.T) {
	c := New(t.TempDir())
	write(t, filepath.Join(c.Dir, "gomod", "m", "f"), 700, 0)
	write(t, filepath.Join(c.Dir, "xdg", "pip", "g"), 300, 0)
	freed, err := c.Clear()
	if err != nil || freed != 1000 {
		t.Fatalf("freed=%d err=%v", freed, err)
	}
	if c.Total() != 0 {
		t.Errorf("sobrou %d bytes", c.Total())
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	c := New(t.TempDir())
	outside := filepath.Join(t.TempDir(), "grande")
	write(t, outside, 9999, 0)
	if err := os.MkdirAll(filepath.Join(c.Dir, "npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(c.Dir, "npm", "link")); err != nil {
		t.Fatal(err)
	}
	if got := c.Total(); got != 0 {
		t.Errorf("o link de fora do cache foi contado: %d", got)
	}
	if _, _, err := c.Prune(0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("a limpeza não pode seguir o link e apagar o alvo")
	}
}

// The cache folder belongs to the app user and the panel cleans it as root: a directory link that
// points outside must never lead the deletion out of the cache.
func TestPruneAndClearNeverLeaveTheCache(t *testing.T) {
	c := New(t.TempDir())
	outside := t.TempDir()
	victim := filepath.Join(outside, "dados", "importante")
	write(t, victim, 100, 365*24*time.Hour)
	write(t, filepath.Join(c.Dir, "npm", "ok"), 10, 365*24*time.Hour)
	if err := os.Symlink(outside, filepath.Join(c.Dir, "npm", "atalho")); err != nil {
		t.Fatal(err)
	}
	// even the tool folder itself can be swapped for a link by the app user
	if err := os.Symlink(outside, filepath.Join(c.Dir, "cargo")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Prune(0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("Prune seguiu um link para fora do cache: %v", err)
	}
	if _, err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("Clear seguiu um link para fora do cache: %v", err)
	}
}
