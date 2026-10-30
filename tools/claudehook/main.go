// Command claudehook implements the Claude Code hooks of this repository in Go, so they run
// the same on Linux, macOS and Windows (no bash, python, jq or gofmt binary required).
//
//	go run github.com/viniciusdev772/br_panel/tools/claudehook <session-start|guard|format|verify>
//
// Claude Code sends the hook event as JSON on stdin. Decisions are returned as JSON on stdout with
// exit code 0: `go run` turns any non-zero child exit into exit code 1, so exit code 2 could never
// reach Claude Code through it.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type event struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	StopHookActive bool `json:"stop_hook_active"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: claudehook <session-start|guard|format|verify>")
		os.Exit(1)
	}
	var ev event
	if data, err := io.ReadAll(os.Stdin); err == nil {
		_ = json.Unmarshal(data, &ev) // some events carry no JSON we care about
	}
	root := projectDir()
	switch os.Args[1] {
	case "session-start":
		sessionStart(root)
	case "guard":
		if msg := blocked(root, ev.ToolInput.FilePath); msg != "" {
			emit(map[string]any{"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": msg,
			}})
		}
	case "format":
		if strings.HasSuffix(ev.ToolInput.FilePath, ".go") {
			_ = formatFile(ev.ToolInput.FilePath)
		}
	case "verify":
		if ev.StopHookActive { // already continuing because of a previous failure: avoid loops
			return
		}
		if out := verify(root); out != "" {
			emit(map[string]any{"decision": "block", "reason": "go build/vet falhou — corrija antes de concluir:\n" + out})
		}
	default:
		fmt.Fprintln(os.Stderr, "modo desconhecido:", os.Args[1])
		os.Exit(1)
	}
}

// emit prints a hook decision as JSON on stdout.
func emit(v any) {
	_ = json.NewEncoder(os.Stdout).Encode(v)
}

// projectDir is $CLAUDE_PROJECT_DIR, or the nearest parent of the working directory holding go.mod.
func projectDir() string {
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" {
		return d
	}
	wd, _ := os.Getwd()
	for d := wd; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return wd
		}
		d = parent
	}
}

// blocked returns a message when file must not be hand-edited: go.sum, databases, private
// keys and the top-level data/ directory.
func blocked(root, file string) string {
	if file == "" {
		return ""
	}
	p := filepath.ToSlash(file)
	base := filepath.Base(p)
	rel := p
	if r, err := filepath.Rel(root, file); err == nil {
		rel = filepath.ToSlash(r)
	}
	switch {
	case base == "go.sum",
		strings.HasSuffix(base, ".db"), strings.HasSuffix(base, ".db-wal"), strings.HasSuffix(base, ".db-shm"),
		strings.HasSuffix(base, ".pem"),
		rel == "data" || strings.HasPrefix(rel, "data/"):
		return "Bloqueado: " + p + " é gerado ou sensível. Use 'go mod tidy' para go.sum; nunca grave dados/segredos à mão."
	}
	return ""
}

// formatFile rewrites a Go file with go/format (same output as gofmt). Files that do not
// parse yet (mid-edit) are left alone.
func formatFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := format.Source(src)
	if err != nil || bytes.Equal(src, out) {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, st.Mode().Perm())
}

func goEnv() []string {
	// The panel only builds for Linux (systemd, cgroups, syscalls): check that target even on Windows/macOS.
	return append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
}

func run(dir string, env []string, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir, c.Env = dir, env
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// verify runs go build and go vet when Go files changed; it returns the failure output or "".
func verify(root string) string {
	if _, err := exec.LookPath("go"); err != nil {
		return ""
	}
	status, err := run(root, os.Environ(), "git", "status", "--porcelain", "--", "*.go", "go.mod")
	if err != nil || status == "" {
		return ""
	}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		if out, err := run(root, goEnv(), "go", args...); err != nil {
			lines := strings.Split(out, "\n")
			if len(lines) > 40 {
				lines = lines[:40]
			}
			return strings.Join(lines, "\n")
		}
	}
	return ""
}

func sessionStart(root string) {
	if _, err := exec.LookPath("go"); err != nil {
		return
	}
	_, _ = run(root, os.Environ(), "go", "mod", "download")
	ver, _ := run(root, os.Environ(), "go", "env", "GOVERSION")
	need := ""
	if mod, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for _, l := range strings.Split(string(mod), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "go "); ok {
				need = v
				break
			}
		}
	}
	fmt.Printf("Go toolchain: %s — this repo requires %s+. The panel targets Linux only (use WSL2 or GOOS=linux on Windows). See CLAUDE.md and .claude/rules/.\n", ver, need)
}
