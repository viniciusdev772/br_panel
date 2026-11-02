package dbengine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// MongoDB is not in the Debian/Ubuntu archives, so installing it means adding the vendor's apt
// repository first. The repository is per release line (8.0, 8.2…); prepareMongoRepo probes the
// vendor's server for the newest line that publishes packages for this distribution.
var (
	mongoRepoBase = "https://repo.mongodb.org"
	mongoKeyBase  = "https://pgp.mongodb.com"
	osReleasePath = "/etc/os-release"
	cpuinfoPath   = "/proc/cpuinfo"
	mongoKeyDir   = "/usr/share/keyrings"
	mongoListPath = "/etc/apt/sources.list.d/mongodb-org.list"
)

// readOSRelease returns ID and VERSION_CODENAME from /etc/os-release.
func readOSRelease() (id, codename string, err error) {
	f, err := os.Open(osReleasePath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		v = strings.Trim(v, `"'`)
		switch {
		case ok && k == "ID":
			id = v
		case ok && k == "VERSION_CODENAME":
			codename = v
		}
	}
	return id, codename, sc.Err()
}

// hasAVX reports whether the CPU can run MongoDB 5.0 or newer (they need AVX on x86-64).
func hasAVX() bool {
	if runtime.GOARCH != "amd64" {
		return true // arm64 builds do not need AVX
	}
	raw, err := os.ReadFile(cpuinfoPath)
	if err != nil {
		return true // cannot tell: let the install proceed
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "flags") {
			for _, fl := range strings.Fields(l) {
				if fl == "avx" {
					return true
				}
			}
			return false
		}
	}
	return true
}

// mongoVersionCandidates lists the major releases (X.0) from newest to oldest; 6.0 is the oldest
// the vendor still supports. The in-between X.1-X.3 lines are rapid releases that the vendor does
// not recommend for production servers, so they are skipped. It goes well past today's newest so a
// future major is found without a panel update.
func mongoVersionCandidates() []string {
	var v []string
	for major := 14; major >= 6; major-- {
		v = append(v, fmt.Sprintf("%d.0", major))
	}
	return v
}

// mongoKeyNames lists the file names that may hold the signing key of a release line: older lines
// publish server-X.Y.asc, newer majors publish server-X.asc.
func mongoKeyNames(version string) []string {
	major, _, _ := strings.Cut(version, ".")
	return []string{"server-" + version + ".asc", "server-" + major + ".asc"}
}

// fetchMongoKey downloads the armored signing key of a release line.
func fetchMongoKey(ctx context.Context, client *http.Client, version string) ([]byte, error) {
	var last error
	for _, name := range mongoKeyNames(version) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mongoKeyBase+"/"+name, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("falha ao baixar a chave do repositório: %w", err)
		}
		key, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case resp.StatusCode != http.StatusOK:
			last = fmt.Errorf("falha ao baixar a chave do repositório: HTTP %d", resp.StatusCode)
		case !strings.Contains(string(key), "BEGIN PGP PUBLIC KEY BLOCK"):
			last = errors.New("a chave baixada não é uma chave PGP")
		default:
			return key, nil
		}
	}
	return nil, last
}

func mongoDistroPath(id string) (repoPath, component string, err error) {
	switch id {
	case "ubuntu":
		return "ubuntu", "multiverse", nil
	case "debian":
		return "debian", "main", nil
	}
	return "", "", fmt.Errorf("o MongoDB só tem repositório oficial para Ubuntu e Debian (esta distribuição é %q); instale manualmente", id)
}

// latestMongoVersion returns the newest release line whose repository exists for the codename.
func latestMongoVersion(ctx context.Context, client *http.Client, repoPath, codename string) (string, error) {
	for _, v := range mongoVersionCandidates() {
		url := fmt.Sprintf("%s/apt/%s/dists/%s/mongodb-org/%s/Release", mongoRepoBase, repoPath, codename, v)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("não foi possível consultar o repositório do MongoDB: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return v, nil
		}
	}
	return "", fmt.Errorf("o MongoDB não publica pacotes para %s %s", repoPath, codename)
}

// mongoSourceLine is the apt source entry for a release line.
func mongoSourceLine(repoPath, component, codename, version, keyPath string) string {
	return fmt.Sprintf("deb [ arch=amd64,arm64 signed-by=%s ] %s/apt/%s %s/mongodb-org/%s %s\n",
		keyPath, mongoRepoBase, repoPath, codename, version, component)
}

// prepareMongoRepo adds the vendor's apt repository for the newest compatible release line, so the
// following apt-get install mongodb-org gets it. Running it again moves the repository to a newer
// line when one has appeared.
func prepareMongoRepo(ctx context.Context, log io.Writer) error {
	id, codename, err := readOSRelease()
	if err != nil || codename == "" {
		return errors.New("não foi possível identificar a distribuição (/etc/os-release)")
	}
	repoPath, component, err := mongoDistroPath(id)
	if err != nil {
		return err
	}
	if !hasAVX() {
		return errors.New("a CPU deste servidor não tem AVX, exigido pelo MongoDB 5.0 ou mais novo; use outro tipo de banco ou outro servidor")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	ver, err := latestMongoVersion(ctx, client, repoPath, codename)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "MongoDB %s é a última versão disponível para %s %s\n", ver, id, codename)

	key, err := fetchMongoKey(ctx, client, ver)
	if err != nil {
		return err
	}
	keyPath := fmt.Sprintf("%s/mongodb-server-%s.asc", mongoKeyDir, ver) // apt reads armored .asc keys directly
	if err := os.WriteFile(keyPath, key, 0o644); err != nil {
		return err
	}
	return os.WriteFile(mongoListPath, []byte(mongoSourceLine(repoPath, component, codename, ver, keyPath)), 0o644)
}
