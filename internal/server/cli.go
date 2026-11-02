package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/viniciusdev772/br_panel/internal/cli"
	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

type cliKey struct{}

// viaCLI is true only for requests that arrived on the CLI socket: serveCLI is the one place that
// sets it, so a browser request can never claim it.
func viaCLI(r *http.Request) bool { return r.Context().Value(cliKey{}) != nil }

// cliAuth authenticates requests from the CLI socket as a throw-away session. The socket is a file
// with mode 0600 owned by root, so being able to open it is the authentication; everything else
// (validation, CSRF check, flash messages) is the same code the web pages use.
func (s *Server) cliAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if viaCLI(r) {
			defer s.auth.Grant(r)()
		}
		next.ServeHTTP(w, r)
	})
}

// cliHandler is the whole panel as seen from the CLI socket.
func (s *Server) cliHandler() http.Handler {
	h := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cliKey{}, true)))
	})
}

// serveCLI listens on DataDir/brpanel.sock until ctx ends.
func (s *Server) serveCLI(ctx context.Context) {
	path := filepath.Join(s.cfg.DataDir, cli.SocketName)
	_ = os.Remove(path) // left by a previous run
	ln, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("CLI desativada (socket %s): %v", path, err)
		return
	}
	// Between Listen and Chmod the file is still created by root with the umask applied (no write
	// bit for other users, which is what connecting needs), and the data directory is not listable.
	if err := os.Chmod(path, 0o600); err != nil {
		log.Printf("CLI desativada (chmod %s): %v", path, err)
		_ = ln.Close()
		return
	}
	hs := &http.Server{Handler: s.cliHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		_ = hs.Close()
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("CLI: %v", err)
	}
	_ = os.Remove(path)
}

// ---- JSON read routes (also used by the CLI) ----

func (s *Server) appInfo(a store.App) cli.AppInfo {
	units := systemd.ReplicaUnits(a.Name, a.Replicas)
	info := cli.AppInfo{
		Name: a.Name, Repo: a.Repo, Branch: a.Branch, BuildCmd: a.BuildCmd, StartCmd: a.StartCmd, Port: a.Port,
		Domain: a.Domain, AutoDeploy: a.AutoDeploy, Env: a.Env, Replicas: a.Replicas, MemoryMB: a.MemoryMB,
		CPUPercent: a.CPUPercent, RateLimit: a.RateLimit, Kind: a.Kind, Status: a.Status,
		State: systemd.StateUnits(units), Deploying: a.Deploying(),
		Domains: []cli.DomainInfo{}, AuthUsers: []string{},
		Maintenance: a.Web.Maintenance, MaintTitle: a.Web.MaintTitle, MaintText: a.Web.MaintText,
		UploadMB: a.Web.MaxBodyMB, ReadTimeout: a.Web.ReadTimeout,
		AutoScale: cli.ScaleInfo{Enabled: a.AutoScale.Enabled, Min: a.AutoScale.Min, Max: a.AutoScale.Max, CPU: a.AutoScale.TargetCPU,
			Mem: a.AutoScale.TargetMem, OutCD: a.AutoScale.OutCooldown, InCD: a.AutoScale.InCooldown},
	}
	if a.Domain != "" {
		info.URL = "https://" + a.Domain
	}
	if !a.LastDeploy.IsZero() {
		info.LastDeploy = a.LastDeploy.Format(time.RFC3339)
	}
	for _, d := range a.Web.Domains {
		info.Domains = append(info.Domains, cli.DomainInfo{Name: d.Name, To: d.To, Permanent: d.Permanent})
	}
	for _, u := range a.Web.Users {
		info.AuthUsers = append(info.AuthUsers, u.Name) // names only, never the hash
	}
	return info
}

func (s *Server) handleAPIApps(w http.ResponseWriter, r *http.Request) {
	out := []cli.AppInfo{}
	for _, a := range s.store.List() {
		out = append(out, s.appInfo(a))
	}
	writeJSON(w, out)
}

func (s *Server) handleAPIApp(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		writeJSON(w, s.appInfo(a))
	}
}

func (s *Server) handleAPIDatabases(w http.ResponseWriter, r *http.Request) {
	out := []cli.DBInfo{}
	for _, d := range s.store.ListDatabases() {
		info := cli.DBInfo{Name: d.Name, Kind: d.Kind, Port: d.Port, Status: d.Status, State: systemd.StateUnit(systemd.DBUnitName(d.Name)),
			Public: d.Public, Schedule: d.BackupSchedule, Created: d.CreatedAt.Format(time.RFC3339), UsedBy: s.appsUsing(d.Name), Refs: []string{}}
		for _, n := range dbengine.VarNames(d) {
			info.Refs = append(info.Refs, "${{"+d.Name+"."+n+"}}")
		}
		if info.UsedBy == nil {
			info.UsedBy = []string{}
		}
		out = append(out, info)
	}
	writeJSON(w, out)
}

// ---- the /cli page ----

func (s *Server) handleCLIPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "cli", page{Title: "CLI", Nav: "cli", CLIGroups: cli.Groups(), CLIPrompt: cli.Prompt, CLICount: len(cli.Catalog)})
}

func (s *Server) handleCLIGuide(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="brpanel-guia-para-agentes.txt"`)
	}
	_, _ = w.Write([]byte(cli.Guide()))
}
