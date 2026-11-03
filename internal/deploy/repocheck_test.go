package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestCheckRepoWarnsAboutMissingEnvAndMonorepo(t *testing.T) {
	d := &Deployer{Paths: Paths{Data: t.TempDir()}}
	a := store.App{Name: "ia", StartCmd: "NODE_ENV=production pnpm start", Env: "MONGODB_URI=mongodb://x"}
	src := d.Paths.SrcDir(a.Name)
	for name, body := range map[string]string{
		"package.json":     `{"scripts":{"start":"node a.js"}}`,
		"a.js":             "process.env.JWT_PRIVATE_KEY; process.env.MONGODB_URI; process.env.MODEL || 'x'; process.env.NODE_ENV",
		"web/package.json": `{"dependencies":{"vite":"1"}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(src, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var log strings.Builder
	d.checkRepo(a, src, &log)
	out := log.String()
	for _, want := range []string{"monorepo:", "web", "JWT_PRIVATE_KEY (obrigatória", "opcionais", "MODEL"} {
		if !strings.Contains(out, want) {
			t.Errorf("o log deveria conter %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"MONGODB_URI", "NODE_ENV"} { // set by the app / by the start command
		if strings.Contains(out, not) {
			t.Errorf("%s já está definida e não deve ser cobrada:\n%s", not, out)
		}
	}
	req, opt := d.MissingEnv(a)
	if len(req) != 1 || req[0].Name != "JWT_PRIVATE_KEY" || len(opt) != 1 || opt[0].Name != "MODEL" {
		t.Errorf("faltando: %+v / %+v", req, opt)
	}
	// Saving the variable settles it without another deploy.
	a.Env += "\nJWT_PRIVATE_KEY=x"
	if req, _ = d.MissingEnv(a); len(req) != 0 {
		t.Errorf("ainda cobra %+v", req)
	}
	if !d.RepoScan(a.Name).Monorepo {
		t.Error("o resultado salvo deveria dizer que é monorepo")
	}
	if got := d.RepoScan("nunca-publicada"); got.Monorepo || len(got.EnvVars) != 0 {
		t.Errorf("sem deploy anterior: %+v", got)
	}
}
