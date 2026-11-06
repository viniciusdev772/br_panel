package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version is the release this binary was built from ("vX.Y.Z"); the release script sets it with
// -ldflags. "dev" means a local build, which never shows the update notice.
var Version = "dev"

// Server is true in the `brpanel` server binary (set by cmd/brpanel) and false in brpanel-cli: it
// picks which release asset an update downloads and whether the panel service is restarted.
var Server bool

// Variables so tests can point the update at a local server and a temporary executable.
var (
	latestAPI    = "https://api.github.com/repos/viniciusdev772/br_panel/releases/latest"
	downloadBase = "https://github.com/viniciusdev772/br_panel/releases/download"
	exePath      = os.Executable
)

func init() { handlers["version"], handlers["update"] = cmdVersion, cmdUpdate }

func cmdVersion(x *call) error {
	if x.json {
		writeJSON(x.out, map[string]string{"version": Version, "os": runtime.GOOS, "arch": runtime.GOARCH})
		return nil
	}
	fmt.Fprintf(x.out, "brpanel %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
	return nil
}

// parseVer reads "vX.Y.Z" (anything after a "-" or "+" is ignored).
func parseVer(s string) (v [3]int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "-")
	s, _, _ = strings.Cut(s, "+")
	p := strings.Split(s, ".")
	if len(p) != 3 {
		return v, false
	}
	for i := range p {
		n, err := strconv.Atoi(p[i])
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// newer reports whether latest is a later release than current; a build that is not a release
// ("dev") counts as older than any release.
func newer(current, latest string) bool {
	l, ok := parseVer(latest)
	if !ok {
		return false
	}
	c, ok := parseVer(current)
	if !ok {
		return true
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func latestTag(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, latestAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("o GitHub respondeu %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	if _, ok := parseVer(r.Tag); !ok {
		return "", fmt.Errorf("versão inesperada %q", r.Tag)
	}
	return r.Tag, nil
}

// assetName is the release file for this binary and platform.
func assetName() (string, error) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("arquitetura sem build publicado: %s", runtime.GOARCH)
	}
	if Server {
		if runtime.GOOS != "linux" {
			return "", fmt.Errorf("o servidor só roda em Linux")
		}
		return "brpanel-linux-" + runtime.GOARCH, nil
	}
	name := "brpanel-cli-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

func download(url string, max int64, w io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu %s", url, resp.Status)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, max))
	return err
}

// install downloads the asset of tag, checks its SHA-256 and swaps it in for the running binary.
// The new file is written next to the old one so the final rename is atomic.
func install(tag string) (string, error) {
	name, err := assetName()
	if err != nil {
		return "", err
	}
	exe, err := exePath()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	var sum strings.Builder
	if err := download(downloadBase+"/"+tag+"/"+name+".sha256", 4096, &sum); err != nil {
		return "", fmt.Errorf("não consegui baixar a soma de verificação: %w", err)
	}
	want, _, _ := strings.Cut(strings.TrimSpace(sum.String()), " ")
	if len(want) != 64 {
		return "", fmt.Errorf("soma de verificação inválida")
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".brpanel-novo-*")
	if err != nil {
		return "", fmt.Errorf("sem permissão para gravar em %s: %w", filepath.Dir(exe), err)
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	err = download(downloadBase+"/"+tag+"/"+name, 200<<20, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("não consegui baixar o binário: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return "", fmt.Errorf("a soma de verificação do arquivo baixado não confere; nada foi alterado")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if b, err := os.ReadFile(exe); err == nil { // like install.sh: keep the previous binary
		_ = os.WriteFile(exe+".bak", b, 0o755)
	}
	if runtime.GOOS == "windows" { // a running .exe cannot be overwritten, but it can be renamed
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			return "", err
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			_ = os.Rename(exe+".old", exe)
			return "", err
		}
		return exe, nil
	}
	return exe, os.Rename(tmp.Name(), exe)
}

func cmdUpdate(x *call) error {
	if x.set("notice") {
		v := x.s("notice")
		if v != "on" && v != "off" {
			return usageErr(x.cmd, "--notice aceita on ou off")
		}
		on := v == "on"
		st := loadUpdateState()
		st.NoticeOff = !on
		saveUpdateState(st)
		if on {
			x.ok("Aviso de versão nova ligado.")
		} else {
			x.ok("Aviso de versão nova desligado. Para ver se há versão nova: brpanel update --check")
		}
		return nil
	}
	tag, err := latestTag(15 * time.Second)
	if err != nil {
		return fail("não consegui consultar a última versão: "+err.Error(), "DICA: confira a conexão com a internet e tente de novo")
	}
	st := loadUpdateState()
	st.Checked, st.Latest = time.Now().Unix(), tag
	saveUpdateState(st)
	avail := newer(Version, tag)
	if x.json && x.on("check") {
		writeJSON(x.out, map[string]any{"current": Version, "latest": tag, "update_available": avail})
		return nil
	}
	if !avail {
		x.ok("Você já está na última versão (" + Version + ").")
		return nil
	}
	fmt.Fprintf(x.out, "Há uma versão nova: %s (a sua é %s).\n", tag, Version)
	if x.on("check") {
		fmt.Fprintln(x.out, "DICA: para atualizar, rode: brpanel update")
		return nil
	}
	if !x.on("yes") {
		if !isTerminal(x.in) {
			fmt.Fprintln(x.out, "DICA: para atualizar sem perguntar, rode: brpanel update --yes")
			return nil
		}
		q := "Atualizar agora? [s/N]: "
		if Server {
			q = "Atualizar agora? O painel reinicia em seguida. [s/N]: "
		}
		switch strings.ToLower(prompt(bufio.NewReader(x.in), x.in, q, false)) {
		case "s", "sim", "y", "yes":
		default:
			fmt.Fprintln(x.out, "Nada foi alterado. Para ver de novo: brpanel update")
			return nil
		}
	}
	exe, err := install(tag)
	if err != nil {
		return fail("a atualização falhou: "+err.Error(), "DICA: o programa atual não foi alterado; no servidor rode com sudo")
	}
	if Server && runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemctl"); err == nil {
			if out, err := exec.Command("systemctl", "restart", "brpanel").CombinedOutput(); err != nil {
				return fail("binário atualizado, mas o painel não reiniciou: "+strings.TrimSpace(string(out)), "DICA: reinicie com: sudo systemctl restart brpanel")
			}
		}
	}
	x.ok("Atualizado para " + tag + " em " + exe + ". O programa anterior ficou em " + exe + ".bak")
	return nil
}

// ---- passive notice ----

// updateState is kept in the user's config folder: when we last asked GitHub and what it said.
type updateState struct {
	Checked   int64  `json:"checked"`
	Latest    string `json:"latest"`
	NoticeOff bool   `json:"notice_off"`
}

func updateStatePath() string {
	d, err := configDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "brpanel", "update.json")
}

func loadUpdateState() (s updateState) {
	if p := updateStatePath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

func saveUpdateState(s updateState) {
	p := updateStatePath()
	if p == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o700) == nil {
		b, _ := json.Marshal(s)
		_ = os.WriteFile(p, b, 0o600) // best effort: a notice is never worth a failure
	}
}

// notice prints one line on stderr when a newer release exists. It asks GitHub at most once a day
// (2 s timeout), only in an interactive terminal and never for JSON output, so scripts and agents
// see nothing extra.
func notice(x *call, errw io.Writer) {
	if Version == "dev" || x.json || x.cmd.Name == "update" || x.cmd.Name == "version" || !isTerminal(x.in) {
		return
	}
	st := loadUpdateState()
	if st.NoticeOff {
		return
	}
	if time.Since(time.Unix(st.Checked, 0)) > 24*time.Hour {
		st.Checked = time.Now().Unix()
		if tag, err := latestTag(2 * time.Second); err == nil {
			st.Latest = tag
		}
		saveUpdateState(st)
	}
	if st.Latest != "" && newer(Version, st.Latest) {
		fmt.Fprintf(errw, "AVISO: há uma versão nova do brpanel (%s; a sua é %s). Atualize com: brpanel update (silenciar: brpanel update --notice off)\n", st.Latest, Version)
	}
}
