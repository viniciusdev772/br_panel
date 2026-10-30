// Command brpanel is a lightweight web panel that deploys apps from Git as systemd services.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/viniciusdev772/br_panel/internal/server"
	"github.com/viniciusdev772/br_panel/internal/store"
)

func main() {
	var cfg server.Config
	flag.StringVar(&cfg.Addr, "addr", ":8080", "endereço de escuta do painel (ex.: 127.0.0.1:8080 após configurar o SSL)")
	flag.StringVar(&cfg.DataDir, "data", "/var/lib/brpanel", "diretório de dados (banco SQLite, logs e apps)")
	flag.Parse()

	// Apps and databases run as unprivileged users: the data directory and the directories that
	// lead to their files must be traversable (0711) without being listable. Everything else
	// (database file, env files, logs, backups) stays private to root.
	for d, mode := range map[string]os.FileMode{"": 0o711, "apps": 0o711, "databases": 0o711, "logs": 0o750, "env": 0o750, "tmp": 0o750} {
		p := filepath.Join(cfg.DataDir, d)
		if err := os.MkdirAll(p, mode); err != nil {
			log.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			log.Fatal(err)
		}
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "brpanel.db"))
	if err != nil {
		log.Fatal("banco de dados: ", err)
	}
	defer st.Close()
	if err := st.ImportLegacy(filepath.Join(cfg.DataDir, "apps.json")); err != nil {
		log.Fatal("migração do apps.json: ", err)
	}

	srv, err := server.New(cfg, st)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
