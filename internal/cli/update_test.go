package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.7.0", "v0.7.1", true}, {"v0.7.0", "v0.7.0", false}, {"v0.10.0", "v0.9.0", false},
		{"v0.9.0", "v0.10.0", true}, {"dev", "v0.1.0", true}, {"v1.0.0", "lixo", false}, {"v0.7.0-3-gabc", "v0.7.0", false},
	} {
		if got := newer(c.cur, c.latest); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.cur, c.latest, got)
		}
	}
}

// fakeRelease serves the "latest" answer and one asset for this platform; badSum corrupts the checksum.
func fakeRelease(t *testing.T, tag, body string, badSum bool) {
	t.Helper()
	name, err := assetName()
	if err != nil {
		t.Skip(err)
	}
	sum := sha256.Sum256([]byte(body))
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"tag_name":%q}`, tag) })
	mux.HandleFunc("/dl/"+tag+"/"+name, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
	mux.HandleFunc("/dl/"+tag+"/"+name+".sha256", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, "%s  %s\n", hexSum, name) })
	srv := httptest.NewServer(mux)
	oldAPI, oldBase, oldVer, oldExe, oldCfg := latestAPI, downloadBase, Version, exePath, configDir
	latestAPI, downloadBase, Version = srv.URL+"/latest", srv.URL+"/dl", "v0.1.0"
	cfg := t.TempDir()
	configDir = func() (string, error) { return cfg, nil }
	t.Cleanup(func() {
		srv.Close()
		latestAPI, downloadBase, Version, exePath, configDir = oldAPI, oldBase, oldVer, oldExe, oldCfg
	})
}

func runUpdate(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := run(nil, append([]string{"update"}, args...), strings.NewReader(""), &out, &errw)
	return code, out.String(), errw.String()
}

func TestUpdateCheckAndAsk(t *testing.T) {
	fakeRelease(t, "v0.2.0", "novo", false)
	exe := filepath.Join(t.TempDir(), "brpanel")
	os.WriteFile(exe, []byte("velho"), 0o755)
	exePath = func() (string, error) { return exe, nil }

	if code, out, _ := runUpdate("--check"); code != 0 || !strings.Contains(out, "v0.2.0") {
		t.Fatalf("--check: %d %q", code, out)
	}
	// Not a terminal and no --yes: it must only tell, never replace the program.
	if code, out, _ := runUpdate(); code != 0 || !strings.Contains(out, "--yes") {
		t.Fatalf("sem terminal: %d %q", code, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "velho" {
		t.Fatal("atualizou sem o usuário aceitar")
	}
}

func TestUpdateYesReplacesAndKeepsBackup(t *testing.T) {
	fakeRelease(t, "v0.2.0", "novo", false)
	exe := filepath.Join(t.TempDir(), "brpanel")
	os.WriteFile(exe, []byte("velho"), 0o755)
	exePath = func() (string, error) { return exe, nil }

	if code, out, errw := runUpdate("--yes"); code != 0 {
		t.Fatalf("code %d: %s%s", code, out, errw)
	}
	if b, _ := os.ReadFile(exe); string(b) != "novo" {
		t.Errorf("binário = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".bak"); string(b) != "velho" {
		t.Errorf(".bak = %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".brpanel-novo-*")); len(left) != 0 {
		t.Errorf("sobrou arquivo temporário: %v", left)
	}
}

func TestUpdateRefusesBadChecksum(t *testing.T) {
	fakeRelease(t, "v0.2.0", "adulterado", true)
	exe := filepath.Join(t.TempDir(), "brpanel")
	os.WriteFile(exe, []byte("velho"), 0o755)
	exePath = func() (string, error) { return exe, nil }

	if code, _, _ := runUpdate("--yes"); code == 0 {
		t.Fatal("aceitou um arquivo com soma errada")
	}
	if b, _ := os.ReadFile(exe); string(b) != "velho" {
		t.Errorf("alterou o programa mesmo com soma errada: %q", b)
	}
}

func TestUpdateUpToDateAndNoticeToggle(t *testing.T) {
	fakeRelease(t, "v0.1.0", "x", false)
	if code, out, _ := runUpdate(); code != 0 || !strings.Contains(out, "última versão") {
		t.Fatalf("já atualizado: %d %q", code, out)
	}
	if code, _, _ := runUpdate("--notice", "off"); code != 0 || !loadUpdateState().NoticeOff {
		t.Fatal("--notice off não gravou a preferência")
	}
	if code, _, _ := runUpdate("--notice", "talvez"); code != 2 {
		t.Fatalf("valor inválido deveria dar 2, deu %d", code)
	}
}
