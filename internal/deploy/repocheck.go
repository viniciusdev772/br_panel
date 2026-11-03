package deploy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/detect"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// RepoScan is what the last deploy learned about the checkout: whether the repository holds
// several projects and which variables the code reads. The app page and the CLI show it.
type RepoScan struct {
	Monorepo bool               `json:"monorepo"`
	Apps     []detect.Candidate `json:"apps,omitempty"`
	EnvVars  []detect.EnvVar    `json:"env_vars,omitempty"`
}

func (p Paths) scanFile(app string) string { return filepath.Join(p.AppDir(app), "scan.json") }

// checkRepo tells the deploy log about a monorepo and about variables the project reads that the
// app does not define, and saves the result for RepoScan. It never fails the deploy: a missing
// variable is only a warning, the app may have a default the scan cannot see.
func (d *Deployer) checkRepo(a store.App, src string, log io.Writer) {
	l := detect.Classify(src)
	scan := RepoScan{Monorepo: l.Monorepo, Apps: l.Apps, EnvVars: detect.EnvVars(d.ProjectDir(a), src)}
	if b, err := json.Marshal(scan); err == nil {
		// The app user owns this folder: a link planted there must not make root write elsewhere.
		_ = os.Remove(d.Paths.scanFile(a.Name))
		_ = writeFile(d.Paths.scanFile(a.Name), string(b), 0o644)
	}
	if l.Monorepo && a.RootDir == "" {
		var names []string
		for _, c := range l.Apps {
			names = append(names, c.Path)
		}
		fmt.Fprintf(log, "monorepo: este repositório tem mais de um projeto (%s). Esta aplicação publica a raiz; para publicar uma pasta, crie outra aplicação com a pasta do projeto (brpanel apps create … --root PASTA).\n", strings.Join(names, ", "))
	}
	req, opt := d.MissingEnv(a)
	if len(req)+len(opt) == 0 {
		return
	}
	if len(req) > 0 {
		fmt.Fprintln(log, "AVISO: o projeto usa variáveis de ambiente que a aplicação não define (aba Variáveis ou brpanel apps env-set):")
		for _, v := range req {
			fmt.Fprintf(log, "  - %s (obrigatória; %s)\n", v.Name, v.Source)
		}
	}
	if len(opt) > 0 {
		fmt.Fprintf(log, "  opcionais (parecem ter valor padrão): %s\n", envNames(opt))
	}
}

func envNames(vs []detect.EnvVar) string {
	n := make([]string, len(vs))
	for i, v := range vs {
		n[i] = v.Name
	}
	return strings.Join(n, ", ")
}

// RepoScan returns what the last deploy of the app saw (the zero value before the first deploy).
func (d *Deployer) RepoScan(name string) RepoScan {
	var s RepoScan
	p := d.Paths.scanFile(name)
	if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return s
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// MissingEnv lists, from the last deploy's scan, the variables the code reads that the app does
// not set: required ones (no default seen) and optional ones. It compares with the app's current
// variables, so saving the missing ones makes them disappear without another deploy.
func (d *Deployer) MissingEnv(a store.App) (required, optional []detect.EnvVar) {
	vars := d.RepoScan(a.Name).EnvVars
	if len(vars) == 0 {
		return nil, nil
	}
	defined := detect.CmdAssigns(a.BuildCmd, a.StartCmd)
	kv, _ := validate.ParseEnv(a.Env)
	for _, p := range kv {
		defined[p[0]] = true
	}
	for _, v := range detect.Undefined(vars, defined) {
		if v.Required {
			required = append(required, v)
		} else {
			optional = append(optional, v)
		}
	}
	return required, optional
}
