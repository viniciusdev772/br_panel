package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsInstall(t *testing.T) {
	dir := t.TempDir()
	var out, errw bytes.Buffer
	if code := run(nil, []string{"skills", "install", "--dir", dir}, strings.NewReader(""), &out, &errw); code != 0 {
		t.Fatalf("code %d: %s%s", code, out.String(), errw.String())
	}
	for _, f := range []string{"brpanel-cli/SKILL.md", "brpanel-cli/references/comandos.md", "brpanel-deploy/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("faltou %s: %v", f, err)
		}
	}
	// A second run overwrites in place (that is how an update works).
	skill := filepath.Join(dir, "brpanel-cli", "SKILL.md")
	if err := os.WriteFile(skill, []byte("velho"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run(nil, []string{"skills", "install", "--dir", dir}, strings.NewReader(""), &out, &errw); code != 0 {
		t.Fatalf("segunda execução: code %d: %s", code, errw.String())
	}
	if b, _ := os.ReadFile(skill); string(b) == "velho" {
		t.Error("a segunda execução não atualizou o SKILL.md")
	}
}
