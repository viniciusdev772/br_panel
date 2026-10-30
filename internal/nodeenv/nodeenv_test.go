package nodeenv

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeWorld puts fake git/bash/curl/runuser first in PATH (no network, no real nvm) and
// returns an Installer wired to temp directories.
func fakeWorld(t *testing.T, scripts map[string]string) *Installer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("usa scripts sh")
	}
	bin := t.TempDir()
	defaults := map[string]string{
		// git clone ... -- <url> <dir>: the destination is the last argument.
		"git": `for last; do :; done; mkdir -p "$last"; : > "$last/nvm.sh"`,
		// First call installs node/npm; the second (dirname ...) prints the bin directory.
		"bash": `case "$2" in
*dirname*) echo "$NVM_DIR/versions/node/v22.0.0/bin" ;;
*) d="$NVM_DIR/versions/node/v22.0.0/bin"; mkdir -p "$d"
   for n in node npm; do printf '#!/bin/sh\necho 22.0.0\n' > "$d/$n"; chmod +x "$d/$n"; done ;;
esac`,
		"curl": `exit 0`,
		// runuser -u <user> -- <cmd> args...
		"runuser": `shift 3; exec "$@"`,
	}
	for name, body := range scripts {
		defaults[name] = body
	}
	for name, body := range defaults {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	root := t.TempDir()
	return &Installer{Dir: filepath.Join(root, "nvm"), LinkDir: filepath.Join(root, "link"), User: func() string { return "brapps" }}
}

func TestInstallLinksNodeAndIsRepeatable(t *testing.T) {
	i := fakeWorld(t, nil)
	var log strings.Builder
	for round := 1; round <= 2; round++ { // the second run replaces its own links
		if err := i.install(context.Background(), &log); err != nil {
			t.Fatalf("rodada %d: %v\n%s", round, err, log.String())
		}
	}
	for _, name := range []string{"node", "npm"} {
		target, err := os.Readlink(filepath.Join(i.LinkDir, name))
		if err != nil || !strings.HasPrefix(target, i.Dir) {
			t.Errorf("%s -> %q %v", name, target, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(i.LinkDir, "pnpm")); err == nil {
		t.Error("não deveria ligar o que o Node.js não trouxe")
	}
	if !strings.Contains(log.String(), "Node.js pronto.") || !strings.Contains(log.String(), "node 22.0.0") {
		t.Errorf("log: %s", log.String())
	}
}

func TestInstallKeepsRealFiles(t *testing.T) {
	i := fakeWorld(t, nil)
	if err := os.MkdirAll(i.LinkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(i.LinkDir, "node")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho do-sistema\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := i.install(context.Background(), &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if fi, _ := os.Lstat(real); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("um node real (pacote da distro) não pode ser trocado")
	}
	if !strings.Contains(log.String(), "não é do nvm") {
		t.Errorf("log: %s", log.String())
	}
}

func TestInstallExplainsMissingPrograms(t *testing.T) {
	i := fakeWorld(t, nil)
	t.Setenv("PATH", "/nonexistent")
	err := i.install(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "apt install") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallFailureNamesTheStep(t *testing.T) {
	i := fakeWorld(t, map[string]string{"git": `echo "fatal: unable to access" >&2; exit 128`})
	err := i.install(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "baixar o nvm") {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyCatchesBinaryTheAppUserCannotRun(t *testing.T) {
	i := fakeWorld(t, map[string]string{"runuser": `echo "Permission denied" >&2; exit 126`})
	err := i.install(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "não executa como brapps") {
		t.Errorf("err = %v", err)
	}
}

func TestEnsureSkipsWhenNodeIsAlreadyThere(t *testing.T) {
	i := fakeWorld(t, map[string]string{"node": `exit 0`, "npm": `exit 0`, "git": `exit 99`})
	if err := i.Ensure(context.Background(), io.Discard); err != nil {
		t.Errorf("com node e npm no PATH não deveria instalar: %v", err)
	}
}

func TestStartRunsInTheBackgroundOnce(t *testing.T) {
	i := fakeWorld(t, nil)
	if !i.Start() {
		t.Fatal("primeira chamada deveria iniciar")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		running, errMsg, log := i.State()
		if !running {
			if errMsg != "" || !strings.Contains(log, "Node.js pronto.") {
				t.Fatalf("err=%q log=%q", errMsg, log)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("não terminou")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Lstat(filepath.Join(i.LinkDir, "node")); err != nil {
		t.Error(err)
	}
}

func TestValidVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"24": true, "22.12": true, "20.18.1": true, "lts/*": true, "node": true,
		"": false, "v24": false, "24;rm -rf /": false, "$(id)": false, "24 && x": false, "../24": false, "lts/iron": false, "123": false, "24.1.2.3": false,
	} {
		if got := ValidVersion(v); got != want {
			t.Errorf("ValidVersion(%q) = %v", v, got)
		}
	}
	if !ValidVersion(DefaultVersion) {
		t.Error("o padrão precisa ser válido")
	}
}

func TestInstallUsesTheConfiguredVersionThroughTheEnvironment(t *testing.T) {
	i := fakeWorld(t, map[string]string{"bash": `case "$2" in
*dirname*) echo "$NVM_DIR/versions/node/v22.0.0/bin" ;;
*) echo "$NODE_VERSION" > "$NVM_DIR/versao"; case "$2" in *'"$NODE_VERSION"'*) : ;; *) echo "script sem a variavel" >&2; exit 9 ;; esac
   d="$NVM_DIR/versions/node/v22.0.0/bin"; mkdir -p "$d"
   for n in node npm; do printf '#!/bin/sh\necho 22.0.0\n' > "$d/$n"; chmod +x "$d/$n"; done ;;
esac`})
	i.Version = func() string { return "22.12" }
	if err := i.install(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(i.Dir, "versao"))
	if strings.TrimSpace(string(got)) != "22.12" {
		t.Errorf("NODE_VERSION = %q", got)
	}
	// An invalid configured value never reaches the script: the default is used instead.
	i.Version = func() string { return "24; touch /tmp/invadido" }
	if i.version() != DefaultVersion {
		t.Errorf("versão inválida deveria cair no padrão, veio %q", i.version())
	}
}
