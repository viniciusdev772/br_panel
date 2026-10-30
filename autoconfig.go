package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// listenHost is where the app should bind: loopback behind Nginx, all interfaces otherwise.
func listenHost(a App) string {
	if a.Domain != "" {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

const (
	firstAutoPort = 3001
	lastAutoPort  = 3999
)

// pickPort returns a free port not used by another app and not currently listening.
func (s *Server) pickPort(except string) int {
	used := map[int]bool{}
	for _, a := range s.store.List() {
		if a.Name != except {
			used[a.Port] = true
		}
	}
	for p := firstAutoPort; p <= lastAutoPort; p++ {
		if used[p] {
			continue
		}
		if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			l.Close()
			return p
		}
	}
	return 0
}

// autoConfigure fills in missing commands by inspecting the checkout. An empty start
// command means "detect"; the result is saved so it shows up (and can be edited) in the UI.
func (s *Server) autoConfigure(ctx context.Context, a App, src string, log io.Writer) (App, error) {
	if a.StartCmd != "" {
		return a, nil
	}
	d := detectProject(src)
	if d == nil {
		return a, fmt.Errorf("não consegui detectar o tipo do projeto (procurei package.json, go.mod, requirements.txt, Cargo.toml, Gemfile, composer.json, index.html…). Informe o comando de início ou adicione um Procfile")
	}
	fmt.Fprintf(log, "detectado: %s\n", d.Kind)
	for _, n := range d.Notes {
		fmt.Fprintf(log, "  nota: %s\n", n)
	}
	if d.Start == "" {
		return a, fmt.Errorf("projeto %s detectado, mas sem comando de início conhecido; informe-o ou adicione um Procfile", d.Kind)
	}
	if missing := missingRequirements(d, exec.LookPath); len(missing) > 0 {
		return a, fmt.Errorf("faltam programas no servidor: %s", strings.Join(missing, ", "))
	}
	a.StartCmd = d.Start
	if a.BuildCmd == "" {
		a.BuildCmd = d.Build
	}
	if a.Port == 0 {
		a.Port = s.pickPort(a.Name)
	}
	fmt.Fprintf(log, "  build:  %s\n  início: %s\n  porta:  %d\n", orDash(a.BuildCmd), a.StartCmd, a.Port)
	if err := s.store.UpdateConfig(a); err != nil {
		return a, err
	}
	return a, nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---- "Detectar automaticamente" in the new-app form ----

var detectMu sync.Mutex // one throw-away clone at a time

type detectResponse struct {
	*Detection
	Name    string   `json:"name"`
	Missing []string `json:"missing,omitempty"`
	Error   string   `json:"error,omitempty"`
}

var nameSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// suggestName derives an app name from the repository URL.
func suggestName(repo string) string {
	base := repo[strings.LastIndexAny(repo, "/:")+1:]
	base = strings.TrimSuffix(base, ".git")
	n := strings.Trim(nameSlugRe.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(n) > 32 {
		n = strings.Trim(n[:32], "-")
	}
	if validateName(n) != nil {
		return ""
	}
	return n
}

// handleDetect makes a shallow, temporary clone and reports what detectProject finds.
func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.PostFormValue("repo"))
	branch := strings.TrimSpace(r.PostFormValue("branch"))
	if branch == "" {
		branch = "main"
	}
	reply := func(code int, v detectResponse) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	if err := validateRepo(repo); err != nil {
		reply(http.StatusUnprocessableEntity, detectResponse{Error: err.Error()})
		return
	}
	if err := validateBranch(branch); err != nil {
		reply(http.StatusUnprocessableEntity, detectResponse{Error: err.Error()})
		return
	}
	detectMu.Lock()
	defer detectMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	tmp, err := os.MkdirTemp(filepath.Join(s.cfg.DataDir, "tmp"), "detect-")
	if err != nil {
		reply(http.StatusInternalServerError, detectResponse{Error: err.Error()})
		return
	}
	defer os.RemoveAll(tmp)
	env := []string{"PATH=" + execPath(), "HOME=" + tmp, "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"}
	if u := s.settings().AppUser; u != "" {
		if err := s.run(ctx, io.Discard, "/", env, "chown", u+":", tmp); err != nil {
			reply(http.StatusInternalServerError, detectResponse{Error: err.Error()})
			return
		}
	}
	gitEnv := s.gitAuthEnv(ctx, repo, env, io.Discard)
	var out strings.Builder
	src := filepath.Join(tmp, "src")
	if err := s.run(ctx, &out, tmp, gitEnv, "git", "clone", "--depth", "1", "--branch", branch, "--", repo, src); err != nil {
		reply(http.StatusBadGateway, detectResponse{Error: "não consegui clonar (confira a URL, a branch e o acesso): " + lastLine(out.String())})
		return
	}
	d := detectProject(src)
	if d == nil {
		reply(http.StatusOK, detectResponse{Name: suggestName(repo), Error: "não reconheci o tipo do projeto; informe os comandos manualmente"})
		return
	}
	reply(http.StatusOK, detectResponse{Detection: d, Name: suggestName(repo), Missing: missingRequirements(d, exec.LookPath)})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
