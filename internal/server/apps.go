package server

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/dbengine"

	"github.com/viniciusdev772/br_panel/internal/deploy"
	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	cards, edges := s.canvas()
	s.render(w, r, "apps", page{Title: "Servidor", Cards: cards, Edges: edges, Events: s.store.Events(30), Kinds: s.dbm.Kinds()})
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "app_form", page{Title: "Nova aplicação", App: store.App{Branch: "main"}, HasKey: s.githubApp() != nil})
}

func appFromForm(r *http.Request, name string) (store.App, error) {
	num := func(k string) int { n, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue(k))); return n }
	port, replicas := num("port"), num("replicas")
	if replicas == 0 {
		replicas = 1 // field left empty
	}
	as := store.AutoScale{
		Enabled: r.PostFormValue("autoscale") == "on",
		Min:     num("scale_min"), Max: num("scale_max"), TargetCPU: num("scale_cpu"), TargetMem: num("scale_mem"),
		OutCooldown: num("scale_out_cd"), InCooldown: num("scale_in_cd"),
	}
	if as.Enabled { // empty fields mean the defaults shown in the form
		as.Min, as.Max, as.TargetCPU = cmp.Or(as.Min, 1), cmp.Or(as.Max, 4), cmp.Or(as.TargetCPU, 60)
		replicas = min(max(replicas, as.Min), as.Max) // the instance count always sits inside the range
	}
	a := store.App{
		Replicas: replicas, MemoryMB: num("memory_mb"), CPUPercent: num("cpu_percent"), RateLimit: num("rate_limit"), AutoScale: as,
		Name:       name,
		Repo:       strings.TrimSpace(r.PostFormValue("repo")),
		Branch:     strings.TrimSpace(r.PostFormValue("branch")),
		BuildCmd:   strings.TrimSpace(r.PostFormValue("build_cmd")),
		StartCmd:   strings.TrimSpace(r.PostFormValue("start_cmd")),
		Port:       port,
		Domain:     strings.ToLower(strings.TrimSpace(r.PostFormValue("domain"))),
		AutoDeploy: r.PostFormValue("auto_deploy") == "on",
		Env:        r.PostFormValue("env"),
		RootDir:    strings.TrimSpace(r.PostFormValue("root_dir")),
		WatchPaths: r.PostFormValue("watch_paths"),
	}
	if a.Branch == "" {
		a.Branch = "main"
	}
	for _, err := range []error{
		validate.Name(a.Name), validate.Repo(a.Repo), validate.Branch(a.Branch),
		validate.Command("Comando de build", a.BuildCmd, false),
		validate.Command("Comando de início", a.StartCmd, false),
		validate.Port(a.Port), validate.Domain(a.Domain),
		validate.Scale(a.Replicas, a.MemoryMB, a.CPUPercent, a.RateLimit),
		validate.AutoScale(as.Enabled, as.Min, as.Max, as.TargetCPU, as.TargetMem, as.OutCooldown, as.InCooldown, a.MemoryMB),
	} {
		if err != nil {
			return a, err
		}
	}
	var rerr, werr error
	a.RootDir, rerr = validate.RootDir(a.RootDir) // normalized: "./apps/web/" becomes "apps/web"
	a.WatchPaths, werr = validate.WatchPaths(a.WatchPaths)
	if rerr != nil {
		return a, rerr
	}
	if werr != nil {
		return a, werr
	}
	if _, ok := githubapp.Slug(a.Repo); a.AutoDeploy && !ok {
		return a, errors.New("deploy automático exige um repositório do GitHub")
	}
	_, err := validate.ParseEnv(a.Env)
	return a, err
}

// domainTaken reports whether another app (or the panel) already uses the domain.
func (s *Server) domainTaken(domain, except string) bool {
	if domain == "" {
		return false
	}
	if domain == s.settings().PanelDomain {
		return true
	}
	owner, taken := s.store.DomainOwner(domain)
	return taken && owner != except
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		name = deploy.SuggestName(strings.TrimSpace(r.PostFormValue("repo")))
	}
	a, err := appFromForm(r, name)
	if err == nil {
		if _, exists := s.store.Get(a.Name); exists {
			err = errors.New("já existe uma aplicação com esse nome")
		} else if s.domainTaken(a.Domain, a.Name) {
			err = errors.New("domínio já usado por outra aplicação")
		} else if msg := s.dep.PortConflict(a); msg != "" {
			err = errors.New(msg)
		} else {
			err = dbengine.CheckRefs(a.Env, s.store.ListDatabases())
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "app_form", page{Title: "Nova aplicação", App: a, Error: err.Error(), HasKey: s.githubApp() != nil})
		return
	}
	if a.Port == 0 {
		a.Port = s.dep.PickPortRange("", a.Replicas)
	}
	if err := s.store.Create(a); err != nil {
		http.Error(w, "erro ao salvar", http.StatusInternalServerError)
		return
	}
	s.store.AddEvent("app", a.Name, "Aplicação criada")
	s.dep.Start(a.Name, "criação")
	http.Redirect(w, r, "/apps/"+a.Name, http.StatusSeeOther)
}

// appFor loads the app named in the path, replying 404 if absent.
func (s *Server) appFor(w http.ResponseWriter, r *http.Request) (store.App, bool) {
	name := r.PathValue("name")
	if validate.Name(name) == nil {
		if a, ok := s.store.Get(name); ok {
			return a, true
		}
	}
	http.NotFound(w, r)
	return store.App{}, false
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		missing, optional := s.dep.MissingEnv(a)
		s.render(w, r, "app", page{Title: a.Name, App: a, EnvMissing: missing, EnvOptional: optional, PortNote: s.dep.PortNote(a.Name), Mono: s.dep.RepoScan(a.Name), State: systemd.StateUnits(systemd.ReplicaUnits(a.Name, a.Replicas)), Info: systemd.InfoAll(systemd.ReplicaUnits(a.Name, a.Replicas)),
			SSL: a.Domain != "" && nginx.FromSettings(s.settings()).HasCert(a.Domain), VarGroups: s.varGroups(), Bdg: appBadge(a.Kind),
			Mounts: s.store.ListMounts(a.Name), VolumeBackups: s.dep.VolumeBackups(a.Name),
			VolumeSchedule: s.store.VolumeBackup(a.Name), VolumeBusy: s.dep.VolumeBusy(a.Name)})
	}
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		s.render(w, r, "app_form", page{Title: "Editar " + a.Name, App: a, Editing: true})
	}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	old, ok := s.appFor(w, r)
	if !ok {
		return
	}
	a, err := appFromForm(r, old.Name)
	if err == nil && s.domainTaken(a.Domain, a.Name) {
		err = errors.New("domínio já usado por outra aplicação")
	}
	if err == nil {
		if msg := s.dep.PortConflict(a); msg != "" {
			err = errors.New(msg)
		}
	}
	if err == nil {
		err = dbengine.CheckRefs(a.Env, s.store.ListDatabases())
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "app_form", page{Title: "Editar " + old.Name, App: a, Editing: true, Error: err.Error()})
		return
	}
	if a.Port == 0 {
		a.Port = s.dep.PickPortRange(a.Name, a.Replicas)
	}
	if err := s.store.UpdateConfig(a); err != nil {
		http.Error(w, "erro ao salvar", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/apps/"+a.Name+"?msg=Configura%C3%A7%C3%A3o+salva.+Fa%C3%A7a+um+novo+deploy+para+aplicar.", http.StatusSeeOther)
}

// handleLogs keeps the old URL working: logs now live in a tab of the app page.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.appFor(w, r); ok {
		http.Redirect(w, r, "/apps/"+a.Name+"#logs", http.StatusSeeOther)
	}
}

// handleLog serves plain-text logs: kind=service (journal) or deploy, lines=N, download=1.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines < 10 || lines > 5000 {
		lines = 200
	}
	kind := r.URL.Query().Get("kind")
	if kind != "deploy" {
		kind = "service"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Deploying", strconv.FormatBool(a.Deploying()))
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+a.Name+"-"+kind+`.log"`)
	}
	if kind == "service" {
		_, _ = w.Write([]byte(systemd.JournalUnits(systemd.ReplicaUnits(a.Name, a.Replicas), lines)))
		return
	}
	_, _ = w.Write([]byte(deploy.LastLines(deploy.TailFile(s.dep.Paths.LogFile(a.Name), 512<<10), lines)))
}

// handleVolumeDownload serves one volume backup file, after validating its name.
func (s *Server) handleVolumeDownload(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	path, ok := s.dep.VolumeBackupPath(a.Name, r.PathValue("file"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.Name+"-"+r.PathValue("file")+`"`)
	http.ServeFile(w, r, path)
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	// Storage actions return to the tab they came from; the others keep the plain redirect.
	tabFlash := func(msg, tab string) {
		http.Redirect(w, r, "/apps/"+a.Name+"?msg="+url.QueryEscape(msg)+"#"+tab, http.StatusSeeOther)
	}
	tabFail := func(err error, tab string) { tabFlash("Falha: "+err.Error(), tab) }
	msg := ""
	switch action := r.PathValue("action"); action {
	case "deploy":
		if !s.dep.Start(a.Name, "manual") {
			msg = "Já existe um deploy em andamento."
		}
	case "deploy-clean":
		if !s.dep.StartClean(a.Name, "do zero") {
			msg = "Já existe um deploy em andamento."
		} else {
			msg = "Deploy do zero iniciado: o código e as dependências estão sendo refeitos."
		}
	case "restart":
		if a.Replicas > 1 { // one instance at a time, in the background: it can take a while
			go func() { _ = s.dep.Restart(a) }()
			msg = "Reinício gradual iniciado: uma instância por vez, para o site não sair do ar."
			break
		}
		fallthrough
	case "start", "stop":
		if out, err := systemd.Ctl(append([]string{action}, systemd.ReplicaUnits(a.Name, a.Replicas)...)...); err != nil {
			msg = "Falha: " + out
		} else {
			msg = "Ação executada: " + action
		}
	case "mount-add":
		mo := store.Mount{App: a.Name, Name: strings.TrimSpace(r.PostFormValue("mount_name")), Kind: r.PostFormValue("mount_kind"),
			Source: strings.TrimSpace(r.PostFormValue("mount_source")), Target: strings.TrimSpace(r.PostFormValue("mount_target")),
			Content: strings.ReplaceAll(r.PostFormValue("mount_content"), "\r\n", "\n")}
		err := validate.Mount(mo.Kind, mo.Name, mo.Source, mo.Target, mo.Content, s.dep.DataDir)
		if err == nil && len(s.store.ListMounts(a.Name)) >= validate.MaxMounts {
			err = fmt.Errorf("o limite é de %d armazenamentos por aplicação", validate.MaxMounts)
		}
		if err == nil {
			if _, err = s.store.AddMount(mo); errors.Is(err, store.ErrExists) {
				err = errors.New("já existe um armazenamento com esse nome ou esse destino")
			}
		}
		if err != nil {
			tabFlash("Não salvo: "+err.Error(), "armazenamento")
			return
		}
		tabFlash("Armazenamento adicionado. Faça um novo deploy para aplicar.", "armazenamento")
		return
	case "mount-delete":
		id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
		if err != nil {
			tabFail(errors.New("armazenamento inválido"), "armazenamento")
			return
		}
		if err := s.store.DeleteMount(a.Name, id); err != nil {
			tabFail(err, "armazenamento")
			return
		}
		tabFlash("Montagem removida. A pasta e os arquivos continuam no servidor.", "armazenamento")
		return
	case "volume-backup":
		if err := s.dep.BackupVolumes(a.Name, "manual"); err != nil {
			tabFail(err, "armazenamento")
			return
		}
		tabFlash("Backup iniciado. Ele aparece na lista em alguns instantes.", "armazenamento")
		return
	case "volume-restore":
		if err := s.dep.RestoreVolume(a.Name, r.PostFormValue("file")); err != nil {
			tabFail(err, "armazenamento")
			return
		}
		tabFlash("Restauração iniciada: as instâncias param e voltam com os arquivos do backup.", "armazenamento")
		return
	case "volume-delete":
		if err := s.dep.DeleteVolumeBackup(a.Name, r.PostFormValue("file")); err != nil {
			tabFail(err, "armazenamento")
			return
		}
		tabFlash("Backup removido.", "armazenamento")
		return
	case "volume-schedule":
		v := r.PostFormValue("schedule")
		if v != "off" && v != "daily" && v != "weekly" {
			tabFail(errors.New("agendamento inválido"), "armazenamento")
			return
		}
		if err := s.store.SetVolumeBackup(a.Name, v); err != nil {
			tabFail(err, "armazenamento")
			return
		}
		tabFlash("Agendamento salvo.", "armazenamento")
		return
	case "delete":
		if err := s.dep.Remove(a.Name); err != nil { // cancels a deploy in progress, never waits for it
			msg = "Falha ao excluir: " + err.Error()
			break
		}
		s.store.AddEvent("app", a.Name, "Aplicação excluída")
		http.Redirect(w, r, "/?msg=Aplica%C3%A7%C3%A3o+exclu%C3%ADda.", http.StatusSeeOther)
		return
	default:
		http.NotFound(w, r)
		return
	}
	q := ""
	if msg != "" {
		q = "?msg=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, "/apps/"+a.Name+q, http.StatusSeeOther)
}

// handleEnvSave saves the app's variables (the "Variáveis" tab), optionally redeploying.
func (s *Server) handleEnvSave(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	env := strings.ReplaceAll(r.PostFormValue("env"), "\r\n", "\n")
	back := "/apps/" + a.Name
	fail := func(msg string) {
		http.Redirect(w, r, back+"?msg="+url.QueryEscape("Não salvo: "+msg)+"#variaveis", http.StatusSeeOther)
	}
	if _, err := validate.ParseEnv(env); err != nil {
		fail(err.Error())
		return
	}
	if err := dbengine.CheckRefs(env, s.store.ListDatabases()); err != nil {
		fail(err.Error())
		return
	}
	a.Env = env
	if err := s.store.UpdateConfig(a); err != nil {
		fail(err.Error())
		return
	}
	msg := "Variáveis salvas. Faça um novo deploy para aplicar."
	if r.PostFormValue("deploy") == "1" {
		if s.dep.Start(a.Name, "variáveis alteradas") {
			msg = "Variáveis salvas; novo deploy iniciado."
		} else {
			msg = "Variáveis salvas; já há um deploy em andamento."
		}
	}
	http.Redirect(w, r, back+"?msg="+url.QueryEscape(msg)+"#variaveis", http.StatusSeeOther)
}
