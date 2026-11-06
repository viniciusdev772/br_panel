package deploy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

// step is what the probe reports at one poll.
type step struct {
	state, sub string
	restarts   int
	ports      []int
}

// scripted plays its steps one per poll, repeating the last; the journal and the holders are fixed.
type scripted struct {
	steps []step
	i     int
	log   string
	held  map[int][]string
}

func (s *scripted) cur() step { return s.steps[min(s.i, len(s.steps)-1)] }
func (s *scripted) info(string) systemd.ServiceInfo {
	c := s.cur()
	return systemd.ServiceInfo{State: c.state, SubState: c.sub, Restarts: c.restarts}
}
func (s *scripted) ports(string) []int {
	c := s.cur()
	s.i++ // one poll = one info + one ports call
	return c.ports
}
func (s *scripted) journal(string) string  { return s.log }
func (s *scripted) holders(p int) []string { return s.held[p] }

var quick = portWatch{site: 60 * time.Millisecond, idle: 25 * time.Millisecond, poll: time.Millisecond}

var (
	up      = step{state: "active", sub: "running"}
	falling = step{state: "activating", sub: "auto-restart"}
)

func upOn(ports ...int) step { return step{state: "active", sub: "running", ports: ports} }

func TestWatch(t *testing.T) {
	cases := []struct {
		name    string
		steps   []step
		site    bool
		want    verdictKind
		minWait time.Duration
	}{
		{"escuta a porta esperada", []step{up, up, upOn(3001)}, true, vOK, 0},
		{"abre a porta tarde, depois de uma porta de depuração", []step{upOn(9229), upOn(9229), upOn(9229), upOn(9229, 3001)}, true, vOK, 0},
		{"só em outra porta: espera o prazo antes de concluir", []step{upOn(3090)}, true, vElsewhere, quick.site},
		{"nada abre, sem domínio: prazo curto", []step{up}, false, vSilent, quick.idle},
		{"nada abre, com domínio: prazo longo", []step{up}, true, vSilent, quick.site},
		{"caiu duas vezes sozinho", []step{up, falling, {state: "active", sub: "running", restarts: 1}, {state: "activating", sub: "auto-restart", restarts: 2}}, true, vCrash, 0},
		{"unit falhou", []step{up, {state: "failed", sub: "failed"}}, true, vCrash, 0},
		{"caiu uma vez e se recuperou", []step{falling, {state: "active", sub: "running", restarts: 1}, {state: "active", sub: "running", restarts: 1, ports: []int{3001}}}, true, vOK, 0},
		{"caiu uma vez e não voltou a abrir a porta", []step{falling, {state: "active", sub: "running", restarts: 1}}, true, vCrash, quick.site},
		{"saiu sem erro (um job)", []step{{state: "inactive", sub: "dead"}}, true, vGone, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := time.Now()
			got := watch(context.Background(), &scripted{steps: c.steps}, quick, "u", 3001, c.site)
			if got.kind != c.want {
				t.Fatalf("veredito %v, quero %v", got.kind, c.want)
			}
			if time.Since(start) < c.minWait {
				t.Errorf("concluiu em %s, antes do prazo de %s", time.Since(start), c.minWait)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := watch(ctx, &scripted{steps: []step{up}}, quick, "u", 3001, true); got.kind != vInterrupted {
		t.Errorf("contexto cancelado: %v", got.kind)
	}
}

func TestAddrInUse(t *testing.T) {
	for name, c := range map[string]struct {
		log   string
		port  int
		found bool
	}{
		"node (log do sv2)":    {"sh[1]: Error: listen EADDRINUSE: address already in use :::3090\nsh[1]:     at Server.setupListenHandle\nsh[1]:   code: 'EADDRINUSE',\nsh[1]:   port: 3090\nsh[1]: }", 3090, true},
		"node só com o objeto": {"x: node:events:505\nx: Error: listen EADDRINUSE\nx:   code: 'EADDRINUSE',\nx:   address: '::',\nx:   port: 4000\nx: }", 4000, true},
		"go":                   {"srv: listen tcp :8080: bind: address already in use", 8080, true},
		"python":               {"srv: OSError: [Errno 98] Address already in use", 0, true},
		"ipv4":                 {"x: Error: listen EADDRINUSE: address already in use 0.0.0.0:5000", 5000, true},
		"outro erro":           {"x: TypeError: cannot read properties of undefined\nx: at index.js:10", 0, false},
		"vazio":                {"", 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			if port, found := addrInUse(c.log); port != c.port || found != c.found {
				t.Errorf("addrInUse = (%d, %v), quero (%d, %v)", port, found, c.port, c.found)
			}
		})
	}
	// An old failure followed by a different, later one: the latest line wins.
	if _, found := addrInUse("a: EADDRINUSE :::3000\nb: all fine\nc: TypeError"); !found {
		t.Error("uma falha de porta anterior ainda conta")
	}
}

func TestStartPorts(t *testing.T) {
	got := startPorts("python3 -m http.server 39190 --bind 127.0.0.1 && node x.js 80 99999 1023 8080")
	if len(got) != 2 || got[0] != 39190 || got[1] != 8080 {
		t.Errorf("startPorts = %v", got)
	}
}

func TestFindPortInSource(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.js", "const express = require('express');\nconst port = 3090;\napp.listen(port);\n")
	write("config/server.py", "HOST = '0.0.0.0'\nPORT = 3090  # fixed\nDB = 'mysql://user:senha@db:3090/x'\n")
	write("node_modules/dep/index.js", "const port = 3090;\n")
	write(".git/hooks/x.js", "const port = 3090;\n")
	write("README.md", "use a porta 3090\n")
	write("big.js", strings.Repeat("// 3090\n", 80000)) // over the size limit
	write("version.js", "const v = '1.3090.2';\nconst n = 13090;\n")

	hits := findPortInSource(dir, 3090)
	joined := strings.Join(hits, "\n")
	if !strings.Contains(joined, "index.js:2: const port = 3090;") || !strings.Contains(joined, "config/server.py:2: PORT = 3090") {
		t.Errorf("deveria achar a declaração em index.js e server.py:\n%s", joined)
	}
	for _, bad := range []string{"node_modules", ".git", "README", "big.js", "version.js", "senha", "mysql://"} {
		if strings.Contains(joined, bad) {
			t.Errorf("%q não deveria aparecer:\n%s", bad, joined)
		}
	}
	if len(findPortInSource(dir, 4555)) != 0 || len(findPortInSource(filepath.Join(dir, "nao-existe"), 3090)) != 0 {
		t.Error("porta ausente ou pasta inexistente não acha nada")
	}
	// A link in the repository must not lead the scan outside of it.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "segredo.js"), []byte("const port = 7777;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err == nil {
		if len(findPortInSource(dir, 7777)) != 0 {
			t.Error("o link simbólico foi seguido para fora do projeto")
		}
	}
}

func TestHolderLabel(t *testing.T) {
	for in, want := range map[string]string{
		"brpanel-xvideoapis.service": "a aplicação xvideoapis",
		"brpanelr2-loja.service":     "uma instância extra de loja",
		"brpaneldb-pg.service":       "o banco pg",
		"nginx.service":              "nginx.service",
		"node (pid 12)":              "node (pid 12)",
	} {
		if got := holderLabel(in); got != want {
			t.Errorf("holderLabel(%q) = %q, quero %q", in, got, want)
		}
	}
}

// portApp is a deployer plus a configured app, with the probe scripted.
func portApp(t *testing.T, a store.App, p *scripted) (*Deployer, store.App) {
	t.Helper()
	d, st := mountDeployer(t)
	if err := st.Delete("web"); err != nil {
		t.Fatal(err)
	}
	a.Repo, a.Branch, a.Replicas = "https://github.com/o/r", "main", max(a.Replicas, 1)
	if err := st.Create(a); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(d.ProjectDir(a), 0o755); err != nil {
		t.Fatal(err)
	}
	d.probe, d.watchTimes = p, quick
	return d, a
}

func TestVerifyPortsHealthyLeavesNoTrace(t *testing.T) {
	a := store.App{Name: "ok", StartCmd: "node x", Port: 3001, Domain: "ok.example.com"}
	d, a := portApp(t, a, &scripted{steps: []step{up, upOn(3001)}})
	d.saveReport("ok", portReport{Note: "aviso antigo", Ignored: true}) // a previous deploy's warning
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)
	if log.Len() != 0 {
		t.Errorf("app saudável não gera mensagem: %q", log.String())
	}
	if d.PortNote("ok") != "" || d.IgnoresPort("ok") {
		t.Error("o aviso antigo deveria sumir depois de uma verificação limpa")
	}
}

// The case that started it all: a second copy of an app that hardcodes the port.
func TestVerifyPortsNamesWhoHoldsTheHardcodedPort(t *testing.T) {
	a := store.App{Name: "sv2", StartCmd: "NODE_ENV=production npm start", Port: 3001, Domain: "sv2.example.com"}
	d, a := portApp(t, a, &scripted{
		steps: []step{up, falling, {state: "active", sub: "running", restarts: 1}, {state: "activating", sub: "auto-restart", restarts: 2}},
		log:   "sh[1]: Error: listen EADDRINUSE: address already in use :::3090\nsh[1]:   code: 'EADDRINUSE',\nsh[1]:   port: 3090",
		held:  map[int][]string{3090: {"brpanel-xvideoapis.service"}},
	})
	if err := os.WriteFile(filepath.Join(d.ProjectDir(a), "index.js"), []byte("const port = 3090;\napp.listen(port);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)

	out := log.String()
	for _, want := range []string{"não consegue subir", "a porta 3090 já está em uso (quem a usa: a aplicação xvideoapis)", "em loop",
		"index.js:1: const port = 3090;", "process.env.PORT"} {
		if !strings.Contains(out, want) {
			t.Errorf("o aviso deveria conter %q:\n%s", want, out)
		}
	}
	if d.IgnoresPort("sv2") {
		t.Error("colidir com OUTRO app não prova que este ignora PORT: não deve travar o autoscale")
	}
	if !strings.Contains(d.PortNote("sv2"), "porta 3090") {
		t.Errorf("a nota da tela: %q", d.PortNote("sv2"))
	}
}

func TestVerifyPortsReplicasFightingForTheSamePort(t *testing.T) {
	a := store.App{Name: "api", StartCmd: "node x", Port: 3001, Domain: "api.example.com", Replicas: 2}
	// Instance 0 listens on its port; instance 1 crashes because instance 0's unit holds the number it fixed.
	d, a := portApp(t, a, &scripted{steps: []step{upOn(3001)}})
	d.probe = &perUnit{units: map[string]*scripted{
		"brpanel-api.service":   {steps: []step{upOn(3001)}},
		"brpanelr1-api.service": {steps: []step{falling, {state: "failed"}}, log: "x: Error: listen EADDRINUSE: address already in use :::3001", held: map[int][]string{3001: {"brpanel-api.service"}}},
	}}
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)
	if !strings.Contains(log.String(), "quem a usa: outra instância deste mesmo app (brpanel-api.service)") {
		t.Errorf("deveria dizer que a outra instância segura a porta:\n%s", log.String())
	}
	if !d.IgnoresPort("api") {
		t.Error("duas instâncias do mesmo app brigando pela porta prova que o app ignora PORT")
	}

	// And the scaler must refuse to add instances to it, without touching systemd.
	a.AutoScale = store.AutoScale{Enabled: true, Min: 1, Max: 4, TargetCPU: 60}
	a.Replicas = 1
	if err := os.MkdirAll(filepath.Dir(d.unitFile("api", 0)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.unitFile("api", 0), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := d.Scale(context.Background(), a, 2, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "não escuta na porta PORT") {
		t.Errorf("Scale deveria recusar com a explicação, veio %v", err)
	}
	if d.Scale(context.Background(), a, 1, &bytes.Buffer{}) != nil {
		t.Error("o mesmo número de instâncias não é uma escala")
	}
}

// perUnit gives each unit its own script.
type perUnit struct{ units map[string]*scripted }

func (p *perUnit) of(u string) *scripted             { return p.units[u] }
func (p *perUnit) info(u string) systemd.ServiceInfo { return p.of(u).info(u) }
func (p *perUnit) ports(u string) []int              { return p.of(u).ports(u) }
func (p *perUnit) journal(u string) string           { return p.of(u).journal(u) }
func (p *perUnit) holders(port int) []string {
	for _, s := range p.units {
		if h := s.held[port]; h != nil {
			return h
		}
	}
	return nil
}

func TestVerifyPortsAppListeningElsewhere(t *testing.T) {
	a := store.App{Name: "legacy", StartCmd: "python3 -m http.server 3090", Port: 3001, Domain: "x.example.com"}
	d, a := portApp(t, a, &scripted{steps: []step{upOn(3090)}})
	if err := os.WriteFile(filepath.Join(d.ProjectDir(a), "server.py"), []byte("PORT = 3090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)
	out := log.String()
	for _, want := range []string{"escuta na porta 3090, mas o painel espera a 3001", "502", "comando de início", "server.py:1: PORT = 3090",
		"brpanel apps set legacy --port 3090"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q:\n%s", want, out)
		}
	}
	if !d.IgnoresPort("legacy") {
		t.Error("o app que só escuta em outra porta ignora PORT")
	}
	if note := d.PortNote("legacy"); !strings.Contains(note, "escuta na porta 3090") || strings.Contains(note, "Nginx") {
		t.Errorf("nota curta e sem detalhes do log: %q", note)
	}

	// With the number already taken by another app, the shortcut is not offered.
	st := d.Store
	if err := st.Create(store.App{Name: "other", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3090, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	log.Reset()
	d.probe.(*scripted).i = 0
	d.verifyPorts(context.Background(), a, &log)
	if strings.Contains(log.String(), "apps set legacy --port") {
		t.Errorf("a porta 3090 é de outro app: não sugerir adotá-la:\n%s", log.String())
	}
}

func TestVerifyPortsQuietCases(t *testing.T) {
	// A worker (no domain, no port) and a no-domain app on another port say nothing, and mark nothing.
	for name, st := range map[string]step{"worker": up, "porta própria sem domínio": upOn(8888)} {
		a := store.App{Name: "w", StartCmd: "sleep 100000", Port: 3001}
		d, a := portApp(t, a, &scripted{steps: []step{st}})
		var log bytes.Buffer
		d.verifyPorts(context.Background(), a, &log)
		if log.Len() != 0 || d.IgnoresPort("w") || d.PortNote("w") != "" {
			t.Errorf("%s: não deveria avisar nem marcar: %q", name, log.String())
		}
	}
	// A crash that is not about a port is reported as such, with the log.
	a := store.App{Name: "boom", StartCmd: "node x", Port: 3001}
	d, a := portApp(t, a, &scripted{steps: []step{falling, {state: "failed"}}, log: "x: TypeError: Cannot read properties of undefined\nx: at index.js:10"})
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)
	if !strings.Contains(log.String(), "não é de porta") || !strings.Contains(log.String(), "TypeError") {
		t.Errorf("erro comum de código:\n%s", log.String())
	}
	if note := d.PortNote("boom"); !strings.Contains(note, "não é de porta") || strings.HasSuffix(note, ":") || strings.Contains(note, "AVISO") || strings.Contains(note, "TypeError") {
		t.Errorf("a nota da tela é uma frase completa, sem o prefixo do log nem linhas dele: %q", note)
	}
	if d.IgnoresPort("boom") {
		t.Error("um erro comum não prova que o app ignora PORT")
	}
}

func TestAddrInUseWithoutPortUsesTheStartCommand(t *testing.T) {
	a := store.App{Name: "py", StartCmd: "python3 -m http.server 39190 --bind 127.0.0.1", Port: 3001, Domain: "py.example.com"}
	d, a := portApp(t, a, &scripted{
		steps: []step{falling, {state: "failed"}},
		log:   "x: OSError: [Errno 98] Address already in use",
		held:  map[int][]string{39190: {"brpanel-first.service"}},
	})
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log)
	if !strings.Contains(log.String(), "a porta 39190 já está em uso (quem a usa: a aplicação first)") {
		t.Errorf("deveria achar a porta pelo comando de início:\n%s", log.String())
	}
}

type panicProbe struct{ scripted }

func (panicProbe) info(string) systemd.ServiceInfo { panic("sonda quebrada") }

// The check is advisory: whatever goes wrong inside it must not reach the panel.
func TestVerifyPortsNeverPanics(t *testing.T) {
	a := store.App{Name: "p", StartCmd: "x", Port: 3001}
	d, a := portApp(t, a, &scripted{steps: []step{up}})
	d.probe = &panicProbe{}
	var log bytes.Buffer
	d.verifyPorts(context.Background(), a, &log) // would panic without the guard
	if !strings.Contains(log.String(), "falhou e foi ignorada") {
		t.Errorf("o pânico deveria virar um aviso no log: %q", log.String())
	}
	d.probe = nil
	d.verifyPorts(context.Background(), a, &log) // a deployer built by hand, without a probe: does nothing
}
