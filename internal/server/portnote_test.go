package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// The port check's verdict, saved by the deploy, must reach every place a person or an agent looks.
func TestPortNoteReachesTheScreens(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	const note = "a instância 0 escuta na porta 3090, mas o painel espera a 3001 (variável PORT): o código fixa a porta"

	if body := do(h, "GET", "/apps/web", nil, c).Body.String(); strings.Contains(body, "Porta.") {
		t.Error("sem verificação salva a página não mostra aviso")
	}
	if _, out, _ := runCLI(t, s, "", "apps", "show", "web"); strings.Contains(out, "aviso de porta") {
		t.Errorf("apps show sem aviso: %q", out)
	}

	dir := s.dep.Paths.AppDir("web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"note": note, "ignores_port": true})
	if err := os.WriteFile(filepath.Join(dir, "portcheck.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if body := do(h, "GET", "/apps/web", nil, c).Body.String(); !strings.Contains(body, "escuta na porta 3090") || !strings.Contains(body, "Porta.") {
		t.Error("a página do app deveria mostrar o aviso")
	}
	code, out, _ := runCLI(t, s, "", "apps", "show", "web")
	if code != 0 || !strings.Contains(out, "aviso de porta:") || !strings.Contains(out, "escuta na porta 3090") || !strings.Contains(out, "apps logs web --kind deploy") {
		t.Errorf("apps show deveria trazer o aviso e onde ver os detalhes:\n%s", out)
	}
	var info struct {
		PortNote string `json:"port_note"`
	}
	_, js, _ := runCLI(t, s, "", "apps", "show", "web", "--json")
	if err := json.Unmarshal([]byte(js), &info); err != nil || info.PortNote != note {
		t.Errorf("--json: %v %q", err, info.PortNote)
	}
	// A broken or hand-edited file must not break the pages.
	if err := os.WriteFile(filepath.Join(dir, "portcheck.json"), []byte("{lixo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCLI(t, s, "", "apps", "show", "web"); code != 0 {
		t.Error("arquivo de verificação corrompido não pode quebrar o apps show")
	}
	if w := do(h, "GET", "/apps/web", nil, c); w.Code != 200 {
		t.Errorf("página com arquivo corrompido: %d", w.Code)
	}
}
