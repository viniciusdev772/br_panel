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

	for _, d := range []string{"", "logs", "env", "apps", "tmp"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o750); err != nil {
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
