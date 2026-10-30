package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const deployTimeout = 20 * time.Minute

// command builds an exec.Cmd, optionally dropping privileges to the app user.
// Every argument is passed separately; nothing is interpreted by a shell
// unless the caller explicitly asks for `sh -c`.
func (s *Server) command(ctx context.Context, dir string, env []string, name string, args ...string) *exec.Cmd {
	if s.settings().AppUser != "" {
		args = append([]string{"-u", s.settings().AppUser, "--", name}, args...)
		name = "runuser"
	}
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = env
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
	return c
}

func (s *Server) run(ctx context.Context, log io.Writer, dir string, env []string, name string, args ...string) error {
	fmt.Fprintf(log, "$ %s %s\n", name, strings.Join(args, " "))
	c := s.command(ctx, dir, env, name, args...)
	c.Stdout, c.Stderr = log, log
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (s *Server) capture(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	var out bytes.Buffer
	c := s.command(ctx, dir, env, name, args...)
	c.Stdout = &out
	err := c.Run()
	return strings.TrimSpace(out.String()), err
}

func (s *Server) buildEnv(a App) ([]string, error) {
	kv, err := parseEnv(a.Env)
	if err != nil {
		return nil, err
	}
	env := []string{
		"PATH=" + execPath(),
		"HOME=" + s.paths.AppDir(a.Name),
		"LANG=C.UTF-8",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
	}
	if a.Port > 0 {
		env = append(env, fmt.Sprintf("PORT=%d", a.Port))
	}
	env = append(env, "HOST="+listenHost(a))
	for _, p := range kv {
		env = append(env, p[0]+"="+p[1])
	}
	return env, nil
}

// startDeploy launches a deploy in the background; false if one is already running.
func (s *Server) startDeploy(name, reason string) bool {
	a, ok := s.store.BeginDeploy(name)
	if !ok {
		return false
	}
	go func() {
		// A push that arrived during this deploy triggers one more run afterwards.
		defer func() {
			if s.takePending(name) {
				s.startDeploy(name, "push recebido durante o deploy anterior")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
		defer cancel()
		lf, err := os.OpenFile(s.paths.LogFile(name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			s.store.FinishDeploy(name, false)
			return
		}
		defer lf.Close()
		fmt.Fprintf(lf, "== deploy de %s iniciado em %s (%s) ==\n", name, time.Now().Format(time.RFC3339), reason)
		if err := s.deploy(ctx, a, lf); err != nil {
			fmt.Fprintf(lf, "\n== FALHOU: %v ==\n", err)
			s.store.FinishDeploy(name, false)
			return
		}
		fmt.Fprintf(lf, "\n== deploy concluído ==\n")
		s.store.FinishDeploy(name, true)
	}()
	return true
}

func (s *Server) deploy(ctx context.Context, a App, log io.Writer) error {
	env, err := s.buildEnv(a)
	if err != nil {
		return err
	}
	appDir, src := s.paths.AppDir(a.Name), s.paths.SrcDir(a.Name)
	gitEnv := s.gitAuthEnv(ctx, a.Repo, env, log)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	if s.settings().AppUser != "" {
		if err := s.run(ctx, log, "/", env, "chown", s.settings().AppUser+":", appDir); err != nil {
			return err
		}
	}

	// Reuse the existing checkout only if it points at the same repository.
	if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
		if url, _ := s.capture(ctx, src, gitEnv, "git", "config", "--get", "remote.origin.url"); url != a.Repo {
			fmt.Fprintln(log, "repositório mudou; clonando novamente")
			if err := os.RemoveAll(src); err != nil {
				return err
			}
		}
	} else if err := os.RemoveAll(src); err != nil {
		return err
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		if err := s.run(ctx, log, appDir, gitEnv, "git", "clone", "--depth", "1", "--branch", a.Branch, "--", a.Repo, src); err != nil {
			return err
		}
	} else {
		if err := s.run(ctx, log, src, gitEnv, "git", "fetch", "--depth", "1", "origin", a.Branch); err != nil {
			return err
		}
		if err := s.run(ctx, log, src, gitEnv, "git", "reset", "--hard", "FETCH_HEAD"); err != nil {
			return err
		}
	}

	var err2 error
	if a, err2 = s.autoConfigure(ctx, a, src, log); err2 != nil {
		return err2
	}
	if env, err = s.buildEnv(a); err != nil {
		return err
	}

	if a.BuildCmd != "" {
		if err := s.run(ctx, log, src, env, "/bin/sh", "-c", a.BuildCmd); err != nil {
			return err
		}
	}

	// Service definition.
	if err := writeFile(s.paths.EnvFile(a.Name), s.envFileContent(a), 0o600); err != nil {
		return err
	}
	unit := renderUnit(a, s.paths, s.settings().AppUser)
	if err := writeFile(s.unitFile(a.Name), unit, 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName(a.Name)}, {"restart", unitName(a.Name)}} {
		if err := sysRun(ctx, log, "systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}

	if a.Domain == "" {
		return s.removeSite(ctx, log, a.Name)
	}
	return s.ensureSite(ctx, log, Site{Key: a.Name, Domain: a.Domain, Upstream: fmt.Sprintf("127.0.0.1:%d", a.Port)})
}

// sysRun runs a host command (systemctl, nginx, certbot) as the panel user, logging its output.
func sysRun(ctx context.Context, log io.Writer, name string, args ...string) error {
	fmt.Fprintf(log, "$ %s %s\n", name, strings.Join(args, " "))
	c := exec.CommandContext(ctx, name, args...)
	c.Env, c.Stdout, c.Stderr = os.Environ(), log, log
	return c.Run()
}

func (s *Server) envFileContent(a App) string {
	var b strings.Builder
	if a.Port > 0 {
		b.WriteString(envFileLine("PORT", fmt.Sprint(a.Port)))
	}
	b.WriteString(envFileLine("HOST", listenHost(a)))
	kv, _ := parseEnv(a.Env) // validated on save
	for _, p := range kv {
		b.WriteString(envFileLine(p[0], p[1]))
	}
	return b.String()
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

// systemctl runs a systemctl action and returns trimmed combined output.
func systemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func serviceState(app string) string {
	out, _ := systemctl("is-active", unitName(app))
	if out == "" {
		return "unknown"
	}
	return strings.Fields(out)[0]
}

func (s *Server) removeApp(name string) error {
	unit := unitName(name)
	_, _ = systemctl("disable", "--now", unit)
	for _, p := range []string{s.unitFile(name), s.paths.EnvFile(name), s.paths.LogFile(name)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, _ = systemctl("daemon-reload")
	_ = s.removeSite(context.Background(), io.Discard, name)
	if err := os.RemoveAll(s.paths.AppDir(name)); err != nil {
		return err
	}
	return s.store.Delete(name)
}

func tailFile(path string, max int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > max {
		_, _ = f.Seek(-max, io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

func journal(app string, lines int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", unitName(app), "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "journalctl indisponível: " + err.Error()
	}
	return string(out)
}

// execPath is the PATH given to builds and git: the standard directories plus whatever the panel
// itself was started with, so that "program found by the readiness check" means "program runnable".
func execPath() string {
	seen := map[string]bool{}
	var parts []string
	for _, p := range append(strings.Split("/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", ":"), filepath.SplitList(os.Getenv("PATH"))...) {
		if p != "" && filepath.IsAbs(p) && !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ":")
}
