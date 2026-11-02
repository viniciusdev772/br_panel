package dbengine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestMongoVersionPicksNewestPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the vendor publishes 7.0 and 8.0 for noble, nothing else
		if r.Method == http.MethodHead && (strings.Contains(r.URL.Path, "/noble/mongodb-org/8.0/") || strings.Contains(r.URL.Path, "/noble/mongodb-org/7.0/")) {
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := mongoRepoBase
	mongoRepoBase = srv.URL
	defer func() { mongoRepoBase = old }()

	got, err := latestMongoVersion(context.Background(), srv.Client(), "ubuntu", "noble")
	if err != nil || got != "8.0" {
		t.Fatalf("got %q, %v; want 8.0", got, err)
	}
	if _, err := latestMongoVersion(context.Background(), srv.Client(), "ubuntu", "plucky"); err == nil {
		t.Error("codinome sem pacotes deveria falhar")
	}
}

func TestMongoDistroPathAndSourceLine(t *testing.T) {
	if p, c, err := mongoDistroPath("debian"); err != nil || p != "debian" || c != "main" {
		t.Errorf("debian: %q %q %v", p, c, err)
	}
	if _, _, err := mongoDistroPath("fedora"); err == nil {
		t.Error("fedora não tem repositório apt")
	}
	old := mongoRepoBase
	mongoRepoBase = "https://repo.mongodb.org"
	defer func() { mongoRepoBase = old }()
	want := "deb [ arch=amd64,arm64 signed-by=/k.asc ] https://repo.mongodb.org/apt/ubuntu noble/mongodb-org/8.0 multiverse\n"
	if got := mongoSourceLine("ubuntu", "multiverse", "noble", "8.0", "/k.asc"); got != want {
		t.Errorf("got %q", got)
	}
}

func TestFetchMongoKeyFallsBackToMajorFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server-9.asc" { // 9.x signs with the major key, there is no server-9.0.asc
			_, _ = w.Write([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nx\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := mongoKeyBase
	mongoKeyBase = srv.URL
	defer func() { mongoKeyBase = old }()
	if _, err := fetchMongoKey(context.Background(), srv.Client(), "9.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchMongoKey(context.Background(), srv.Client(), "8.0"); err == nil {
		t.Error("sem arquivo de chave deveria falhar")
	}
}
