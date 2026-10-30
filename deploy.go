package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const deployTimeout = 20 * time.Minute

// command builds an exec.Cmd, optionally dropping privileges to the app user.
// Every argument is passed separately; nothing is interpreted by a shell
// unless the caller explicitly asks for `sh -c`.
func (s *Server) command(ctx context.Context, dir string, env []string, name string, args ...string) *exec.Cmd {
	if s.cfg.AppUser != "" {
		args = append([]string{"-u", s.cfg.AppUser, "--", name}, args...)
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
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + s.paths.AppDir(a.Name),
		"LANG=C.UTF-8",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
	}
	if a.Port > 0 {
		env = append(env, fmt.Sprintf("PORT=%d", a.Port))
	}
	for _, p := range kv {
		env = append(env, p[0]+"="+p[1])
	}
	return env, nil
}

// startDeploy launches a deploy in the background; false if one is already running.
func (s *Server) startDeploy(name string) bool {
	a, ok := s.store.BeginDeploy(name)
	if !ok {
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
		defer cancel()
		lf, err := os.OpenFile(s.paths.LogFile(name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			s.store.FinishDeploy(name, false)
			return
		}
		defer lf.Close()
		fmt.Fprintf(lf, "== deploy de %s iniciado em %s ==\n", name, time.Now().Format(time.RFC3339))
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
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	if s.cfg.AppUser != "" {
		if err := s.run(ctx, log, "/", env, "chown", s.cfg.AppUser+":", appDir); err != nil {
			return err
		}
	}

	// Reuse the existing checkout only if it points at the same repository.
	if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
		if url, _ := s.capture(ctx, src, env, "git", "config", "--get", "remote.origin.url"); url != a.Repo {
			fmt.Fprintln(log, "repositório mudou; clonando novamente")
			if err := os.RemoveAll(src); err != nil {
				return err
			}
		}
	} else if err := os.RemoveAll(src); err != nil {
		return err
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		if err := s.run(ctx, log, appDir, env, "git", "clone", "--depth", "1", "--branch", a.Branch, "--", a.Repo, src); err != nil {
			return err
		}
	} else {
		if err := s.run(ctx, log, src, env, "git", "fetch", "--depth", "1", "origin", a.Branch); err != nil {
			return err
		}
		if err := s.run(ctx, log, src, env, "git", "reset", "--hard", "FETCH_HEAD"); err != nil {
			return err
		}
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
	unit := renderUnit(a, s.paths, s.cfg.AppUser)
	if err := writeFile(s.paths.UnitFile(a.Name), unit, 0o644); err != nil {
		return err
	}
	sys := os.Environ()
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName(a.Name)}, {"restart", unitName(a.Name)}} {
		c := exec.CommandContext(ctx, "systemctl", args...)
		fmt.Fprintf(log, "$ systemctl %s\n", strings.Join(args, " "))
		c.Env, c.Stdout, c.Stderr = sys, log, log
		if err := c.Run(); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}
	return nil
}

func (s *Server) envFileContent(a App) string {
	var b strings.Builder
	if a.Port > 0 {
		b.WriteString(envFileLine("PORT", fmt.Sprint(a.Port)))
	}
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
	for _, p := range []string{s.paths.UnitFile(name), s.paths.EnvFile(name), s.paths.LogFile(name)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, _ = systemctl("daemon-reload")
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

func journal(app string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", unitName(app), "-n", "200", "--no-pager", "-o", "short-iso").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "journalctl indisponível: " + err.Error()
	}
	return string(out)
}
