package cli

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/viniciusdev772/br_panel/skills"
)

func init() { handlers["skills install"] = cmdSkillsInstall }

// cmdSkillsInstall copies the embedded skills into the agent's skills folder (default
// ~/.claude/skills). It overwrites only the brpanel-* folders it owns, so updating is re-running it.
func cmdSkillsInstall(x *call) error {
	dir := x.s("dir")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fail("não achei a sua pasta pessoal", "DICA: diga a pasta com --dir PASTA")
		}
		dir = filepath.Join(home, ".claude", "skills")
	}
	n := 0
	err := fs.WalkDir(skills.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := skills.FS.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if path.Base(path.Dir(p)) == "scripts" {
			mode = 0o755
		}
		n++
		return os.WriteFile(dst, b, mode)
	})
	if err != nil {
		return fail("não consegui gravar os skills em "+dir+": "+err.Error(), "DICA: confira a permissão da pasta ou use --dir PASTA")
	}
	x.ok("Skills instalados em " + dir + " (" + strconv.Itoa(n) + " arquivos). Abra o brpanel-cli primeiro.")
	return nil
}
