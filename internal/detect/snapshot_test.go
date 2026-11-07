package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotReadsKeyFilesAndNeverSecrets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"start":"node server.js"}}`)
	write(t, dir, ".env", "SENHA=nao-deve-aparecer")
	write(t, dir, ".env.example", "DATABASE_URL=")
	write(t, dir, "deploy/id_rsa", "-----BEGIN PRIVATE KEY-----")
	write(t, dir, "node_modules/x/index.js", "lixo")
	write(t, dir, "src/server.js", "console.log(1)")
	write(t, dir, "README.md", strings.Repeat("a", 20000))

	got := Snapshot(dir)
	for _, want := range []string{"package.json", `"start":"node server.js"`, "DATABASE_URL=", "src/server.js", "(arquivo cortado)"} {
		if !strings.Contains(got, want) {
			t.Errorf("o snapshot deveria conter %q", want)
		}
	}
	for _, bad := range []string{"nao-deve-aparecer", "BEGIN PRIVATE KEY", "node_modules", "lixo"} {
		if strings.Contains(got, bad) {
			t.Errorf("o snapshot não pode conter %q", bad)
		}
	}
	if len(got) > snapshotTotalCap+4<<10 {
		t.Errorf("snapshot grande demais: %d bytes", len(got))
	}
}

func TestSnapshotSkipsLinks(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	write(t, out, "package.json", `{"name":"fora"}`)
	if err := os.Symlink(out, filepath.Join(dir, "atalho")); err != nil {
		t.Skip("sem permissão para criar links neste sistema:", err)
	}
	if got := Snapshot(dir); strings.Contains(got, "fora") || strings.Contains(got, "atalho") {
		t.Errorf("links não podem ser seguidos nem listados:\n%s", got)
	}
}

func TestTreeIsBounded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		write(t, dir, filepath.Join("d", string(rune('a'+i%26)), "f"+string(rune('a'+i/26))+".txt"), "x")
	}
	lines, _, cut := Tree(dir, 10, 3)
	if len(lines) != 10 || !cut {
		t.Errorf("a lista deveria parar em 10 linhas e avisar que cortou: %d %v", len(lines), cut)
	}
}
