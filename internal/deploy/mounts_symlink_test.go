package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink committed to the repository must not redirect the mount point (root creates it).
func TestNoSymlinks(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "storage")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	if err := noSymlinks(base, filepath.Join(base, "storage", "uploads")); err == nil {
		t.Fatal("o atalho storage deveria ser recusado")
	}
	if err := noSymlinks(base, filepath.Join(base, "novo", "uploads")); err != nil {
		t.Fatalf("caminho que não existe: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := noSymlinks(base, filepath.Join(base, "ok", "uploads")); err != nil {
		t.Fatalf("pasta real: %v", err)
	}
}
