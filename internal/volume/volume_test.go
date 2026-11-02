package volume

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveExtractRoundTrip(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "um")
	write(t, filepath.Join(src, "sub", "deep", "b.txt"), "dois")
	if err := os.MkdirAll(filepath.Join(src, "vazia"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink("a.txt", filepath.Join(src, "atalho")) == nil // not allowed everywhere on Windows

	var buf bytes.Buffer
	if err := Archive(src, &buf); err != nil {
		t.Fatal(err)
	}
	if err := Extract(&buf, dst); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"a.txt": "um", "sub/deep/b.txt": "dois"} {
		if got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(p))); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", p, got, err)
		}
	}
	if st, err := os.Stat(filepath.Join(dst, "vazia")); err != nil || !st.IsDir() {
		t.Errorf("pasta vazia: %v", err)
	}
	if linked {
		if got, err := os.Readlink(filepath.Join(dst, "atalho")); err != nil || got != "a.txt" {
			t.Errorf("atalho = %q, %v", got, err)
		}
	}
}

// archiveOf builds a tar.gz by hand, the way a damaged or hostile backup could look.
func archiveOf(t *testing.T, entries ...tar.Header) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h.Mode = 0o644
		if h.Typeflag == tar.TypeReg {
			h.Size = 4
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte("evil"))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return &buf
}

func TestExtractRefusesHostileEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    []tar.Header
	}{
		{"sobe um nível", []tar.Header{{Name: "../evil", Typeflag: tar.TypeReg}}},
		{"sobe no meio", []tar.Header{{Name: "a/../../evil", Typeflag: tar.TypeReg}}},
		{"caminho absoluto", []tar.Header{{Name: "/tmp/evil", Typeflag: tar.TypeReg}}},
		{"barra invertida", []tar.Header{{Name: `\evil`, Typeflag: tar.TypeReg}}},
		{"link duro", []tar.Header{{Name: "x", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"}}},
		{"dispositivo", []tar.Header{{Name: "x", Typeflag: tar.TypeChar}}},
		{"pasta fora", []tar.Header{{Name: "../pasta/", Typeflag: tar.TypeDir}}},
		{"atravessa atalho", []tar.Header{
			{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "../fora"},
			{Name: "a/evil", Typeflag: tar.TypeReg},
		}},
	} {
		root := t.TempDir()
		dst := filepath.Join(root, "dst")
		if err := os.MkdirAll(filepath.Join(root, "fora"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = Extract(archiveOf(t, tc.h...), dst)
		for _, leak := range []string{"evil", filepath.Join("fora", "evil"), filepath.Join("..", "evil")} {
			if _, err := os.Stat(filepath.Join(root, leak)); err == nil {
				t.Errorf("%s: escreveu fora da pasta: %s", tc.name, leak)
			}
		}
		if tc.name != "atravessa atalho" {
			if err := Extract(archiveOf(t, tc.h...), t.TempDir()); err == nil {
				t.Errorf("%s: deveria recusar", tc.name)
			}
		}
	}
}

// A folder the app wrote can contain a symlink; restoring as root must not write through it.
func TestExtractRefusesPreExistingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "fora")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "atalho")); err != nil {
		t.Skip("sem atalhos neste sistema")
	}
	err := Extract(archiveOf(t, tar.Header{Name: "atalho/evil", Typeflag: tar.TypeReg}), dst)
	if err == nil {
		t.Error("deveria recusar escrever através de um atalho existente")
	}
	if _, err := os.Stat(filepath.Join(outside, "evil")); err == nil {
		t.Error("escreveu fora da pasta")
	}
}

func TestExtractRejectsGarbage(t *testing.T) {
	if err := Extract(bytes.NewBufferString("não é gzip"), t.TempDir()); err == nil {
		t.Error("lixo deveria falhar")
	}
}

func TestBackupFiles(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC)
	name := FileName("uploads", now)
	if name != "uploads-20261102-103000.tar.gz" {
		t.Fatal(name)
	}
	if v, ok := ParseFile(name); !ok || v != "uploads" {
		t.Errorf("ParseFile = %q %v", v, ok)
	}
	if v, ok := ParseFile(FileName("meus-dados-2", now)); !ok || v != "meus-dados-2" {
		t.Errorf("nome com hífen e número = %q %v", v, ok)
	}
	for _, bad := range []string{"", "x.tar.gz", "../uploads-20261102-103000.tar.gz", "a/b-20261102-103000.tar.gz",
		"Up-20261102-103000.tar.gz", "up-20261102-103000.tar.gz.sh", "up-2026-103000.tar.gz", "up-20261102-103000.tgz", "-20261102-103000.tar.gz"} {
		if _, ok := ParseFile(bad); ok {
			t.Errorf("%q não é um nome de backup", bad)
		}
	}

	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, FileName("a", now.Add(time.Duration(i)*time.Hour)))
		write(t, p, "x")
		_ = os.Chtimes(p, now.Add(time.Duration(i)*time.Hour), now.Add(time.Duration(i)*time.Hour))
	}
	write(t, filepath.Join(dir, FileName("b", now)), "y")
	write(t, filepath.Join(dir, "lixo.txt"), "z")

	if n := len(List(dir)); n != 5 {
		t.Fatalf("List = %d", n)
	}
	if b, ok := Newest(dir, "a"); !ok || b.File != FileName("a", now.Add(3*time.Hour)) {
		t.Errorf("Newest = %+v %v", b, ok)
	}
	Prune(dir, "a", 2)
	var a, b int
	for _, i := range List(dir) {
		if i.Volume == "a" {
			a++
		} else {
			b++
		}
	}
	if a != 2 || b != 1 {
		t.Errorf("depois de Prune: a=%d b=%d", a, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "lixo.txt")); err != nil {
		t.Error("Prune só mexe em arquivos de backup")
	}
}
