package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/dbengine"
	"github.com/viniciusdev772/br_panel/internal/deploy"
	"github.com/viniciusdev772/br_panel/internal/monitor"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// varGroup lists the reference variables of one database for the app "Variáveis" tab.
type varGroup struct {
	DB    string
	Kind  string
	Badge string
	Vars  []varRef
}

type varRef struct{ Name, Ref string }

// dbFor loads the database named in the path, replying 404 if absent.
func (s *Server) dbFor(w http.ResponseWriter, r *http.Request) (store.Database, bool) {
	name := r.PathValue("name")
	if validate.Name(name) == nil {
		if d, ok := s.store.GetDatabase(name); ok {
			return d, true
		}
	}
	http.NotFound(w, r)
	return store.Database{}, false
}

// appsUsing returns the apps whose variables reference the database.
func (s *Server) appsUsing(db string) []string {
	var out []string
	for _, a := range s.store.List() {
		for _, ref := range dbengine.References(a.Env) {
			if ref == db {
				out = append(out, a.Name)
			}
		}
	}
	return out
}

func (s *Server) varGroups() []varGroup {
	var out []varGroup
	for _, d := range s.store.ListDatabases() {
		sp, _ := dbengine.SpecOf(dbengine.Kind(d.Kind))
		g := varGroup{DB: d.Name, Kind: d.Kind, Badge: sp.Badge}
		for _, n := range dbengine.VarNames(d) {
			g.Vars = append(g.Vars, varRef{Name: n, Ref: "${{" + d.Name + "." + n + "}}"})
		}
		out = append(out, g)
	}
	return out
}

func (s *Server) handleDBNew(w http.ResponseWriter, r *http.Request) {
	kinds := s.dbm.Kinds()
	sel := kinds[0]
	for _, k := range kinds {
		if string(k.Kind) == r.URL.Query().Get("kind") {
			sel = k
		}
	}
	running, ierr := s.dbm.InstallState(sel.Kind)
	s.render(w, r, "database_new", page{Title: "Novo banco de dados", Kinds: kinds, KindSel: sel, DefaultName: s.freeName(string(sel.Kind)),
		InstallRunning: running, InstallErr: ierr, DBVersion: s.dbm.Version(sel.Kind)})
}

// freeName suggests base, base-2, base-3… that no app or database uses.
func (s *Server) freeName(base string) string {
	taken := map[string]bool{}
	for _, a := range s.store.List() {
		taken[a.Name] = true
	}
	for _, d := range s.store.ListDatabases() {
		taken[d.Name] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func (s *Server) handleDBCreate(w http.ResponseWriter, r *http.Request) {
	kind := dbengine.Kind(r.PostFormValue("kind"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	if _, err := s.dbm.Create(name, kind); err != nil {
		kinds := s.dbm.Kinds()
		sel := kinds[0]
		for _, k := range kinds {
			if k.Kind == kind {
				sel = k
			}
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "database_new", page{Title: "Novo banco de dados", Kinds: kinds, KindSel: sel, DefaultName: name, Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/databases/"+name+"#conectar", http.StatusSeeOther)
}

func (s *Server) handleDBInstall(w http.ResponseWriter, r *http.Request) {
	kind := dbengine.Kind(r.PostFormValue("kind"))
	msg := ""
	if err := s.dbm.Install(kind); err != nil {
		msg = "&msg=" + url.QueryEscape(err.Error())
	}
	http.Redirect(w, r, "/databases/new?kind="+url.QueryEscape(string(kind))+msg, http.StatusSeeOther)
}

func (s *Server) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	kind := dbengine.Kind(r.URL.Query().Get("kind"))
	running, errMsg := s.dbm.InstallState(kind)
	installed := false
	for _, k := range s.dbm.Kinds() {
		if k.Kind == kind {
			installed = k.Installed
		}
	}
	log := deploy.TailFile(s.dbm.InstallLogPath(kind), 32<<10)
	writeJSON(w, map[string]any{"running": running, "error": errMsg, "installed": installed, "log": log})
}

func (s *Server) handleDBPage(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	spec, _ := dbengine.SpecOf(dbengine.Kind(d.Kind))
	s.render(w, r, "database", page{
		Title: d.Name, DB: d, DBSpec: spec, DBVersion: s.dbm.Version(dbengine.Kind(d.Kind)),
		Info: systemd.InfoUnit(systemd.DBUnitName(d.Name)), State: systemd.StateUnit(systemd.DBUnitName(d.Name)),
		Fwd: s.dbm.ForwardState(d), Conns: s.dbm.Connections(d), Console: s.dbm.ConsoleCommand(d), Backups: s.dbm.Backups(d.Name), Busy: s.dbm.Busy(d.Name),
		UsedBy: s.appsUsing(d.Name), VarGroups: []varGroup{{DB: d.Name, Kind: d.Kind, Vars: func() []varRef {
			var v []varRef
			for _, n := range dbengine.VarNames(d) {
				v = append(v, varRef{Name: n, Ref: "${{" + d.Name + "." + n + "}}"})
			}
			return v
		}()}},
	})
}

func (s *Server) handleDBLog(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines < 10 || lines > 5000 {
		lines = 200
	}
	kind := r.URL.Query().Get("kind")
	if kind != "setup" {
		kind = "service"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Deploying", strconv.FormatBool(d.Status == store.DBProvisioning || s.dbm.Busy(d.Name) != ""))
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+d.Name+"-"+kind+`.log"`)
	}
	if kind == "service" {
		_, _ = w.Write([]byte(systemd.JournalUnit(systemd.DBUnitName(d.Name), lines)))
		return
	}
	_, _ = w.Write([]byte(deploy.LastLines(deploy.TailFile(s.dbm.LogPath(d.Name), 512<<10), lines)))
}

func (s *Server) handleDBMetrics(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	rg := r.URL.Query().Get("range")
	dur, known := chartRanges[rg]
	if !known {
		rg, dur = "1h", time.Hour
	}
	now := time.Now()
	pts := s.store.Metrics(monitor.DBKey(d.Name), now.Add(-dur), monitor.Bucket(dur))
	if pts == nil {
		pts = []store.Point{}
	}
	writeJSON(w, metricsResponse{Info: systemd.InfoUnit(systemd.DBUnitName(d.Name)), Points: pts, Range: rg, From: now.Add(-dur).Unix(), To: now.Unix()})
}

// handleDBData serves the read-only "Dados" tab: the list of tables/collections/keys, or a preview of one.
func (s *Server) handleDBData(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if item := r.URL.Query().Get("item"); item != "" {
		pv, err := s.dbm.Preview(ctx, d.Name, item)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, pv)
		return
	}
	items, err := s.dbm.Tables(ctx, d.Name)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

func (s *Server) handleDBBackupDownload(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	path, ok := s.dbm.BackupPath(d.Name, r.PathValue("file"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.Name+"-"+r.PathValue("file")+`"`)
	http.ServeFile(w, r, path)
}

func (s *Server) handleDBAction(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dbFor(w, r)
	if !ok {
		return
	}
	back := "/databases/" + d.Name
	flash := func(msg, tab string) {
		q := ""
		if msg != "" {
			q = "?msg=" + url.QueryEscape(msg)
		}
		http.Redirect(w, r, back+q+"#"+tab, http.StatusSeeOther)
	}
	fail := func(err error, tab string) { flash("Falha: "+err.Error(), tab) }
	switch action := r.PathValue("action"); action {
	case "start", "stop", "restart":
		if err := s.dbm.Control(d.Name, action); err != nil {
			fail(err, "configuracoes")
			return
		}
		flash("Ação executada: "+action, "configuracoes")
	case "create-item":
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := s.dbm.CreateItem(ctx, d.Name, r.PostFormValue("item"), r.PostFormValue("value")); err != nil {
			fail(err, "dados")
			return
		}
		flash("Item criado.", "dados")
	case "backup":
		if err := s.dbm.Backup(d.Name, "manual"); err != nil {
			fail(err, "backups")
			return
		}
		flash("Backup iniciado.", "backups")
	case "backup-delete":
		if err := s.dbm.DeleteBackup(d.Name, r.PostFormValue("file")); err != nil {
			fail(err, "backups")
			return
		}
		flash("Backup removido.", "backups")
	case "restore":
		if err := s.dbm.Restore(d.Name, r.PostFormValue("file")); err != nil {
			fail(err, "backups")
			return
		}
		flash("Restauração iniciada: os dados atuais serão substituídos.", "backups")
	case "schedule":
		v := r.PostFormValue("schedule")
		if v != "off" && v != "daily" && v != "weekly" {
			fail(errors.New("agendamento inválido"), "backups")
			return
		}
		if err := s.store.SetDatabaseSchedule(d.Name, v); err != nil {
			fail(err, "backups")
			return
		}
		flash("Agendamento salvo.", "backups")
	case "regenerate":
		if err := s.dbm.RegeneratePassword(d.Name); err != nil {
			fail(err, "conectar")
			return
		}
		msg := "Senha regenerada."
		if used := s.appsUsing(d.Name); len(used) > 0 {
			msg += " Faça um novo deploy de: " + strings.Join(used, ", ") + "."
		}
		flash(msg, "conectar")
	case "public":
		if err := s.dbm.SetPublic(d.Name, r.PostFormValue("public") == "on"); err != nil {
			fail(err, "configuracoes")
			return
		}
		flash("Acesso externo atualizado.", "configuracoes")
	case "forward-on":
		if err := s.dbm.EnableForward(d.Name); err != nil {
			fail(err, "configuracoes")
			return
		}
		flash("Repasse ativado.", "configuracoes")
	case "forward-off":
		if err := s.dbm.DisableForward(d.Name); err != nil {
			fail(err, "configuracoes")
			return
		}
		flash("Repasse desativado.", "configuracoes")
	case "delete":
		if err := s.dbm.Delete(d.Name); err != nil {
			fail(err, "configuracoes")
			return
		}
		http.Redirect(w, r, "/?msg="+url.QueryEscape("Banco excluído."), http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}
