package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlocked(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"go.sum", "data/brpanel.db", "data", "brpanel.db", "keys/app.pem", "x.db-wal"} {
		if blocked(root, filepath.Join(root, filepath.FromSlash(f))) == "" {
			t.Errorf("%s deveria ser bloqueado", f)
		}
	}
	for _, f := range []string{"internal/store/store.go", "docs/data/notes.md", "web/static/app.js", "go.mod", ""} {
		if msg := blocked(root, filepath.Join(root, filepath.FromSlash(f))); msg != "" && f != "" {
			t.Errorf("%s não deveria ser bloqueado: %s", f, msg)
		}
	}
	if blocked(root, "") != "" {
		t.Error("caminho vazio não bloqueia")
	}
}

func TestFormatFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("package a\nfunc  F( ) {\nx:=1\n_=x}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := formatFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "func F() {\n\tx := 1\n\t_ = x\n}") {
		t.Errorf("não formatou:\n%s", got)
	}
	// a file that does not parse (mid-edit) is left untouched
	broken := filepath.Join(dir, "b.go")
	_ = os.WriteFile(broken, []byte("package b\nfunc {"), 0o644)
	if formatFile(broken) == nil {
		t.Error("sintaxe inválida deveria retornar erro sem alterar o arquivo")
	}
	if b, _ := os.ReadFile(broken); string(b) != "package b\nfunc {" {
		t.Error("arquivo inválido não pode ser alterado")
	}
}
