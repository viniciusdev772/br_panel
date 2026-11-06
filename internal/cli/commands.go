package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// pollEvery is how often `--wait` looks at the deploy; a variable so tests do not sleep.
var pollEvery = 2 * time.Second

var handlers = map[string]func(*call) error{
	"status": cmdStatus, "events": cmdEvents, "guide": cmdGuide, "help": cmdHelp,
	"apps list": cmdAppsList, "apps show": cmdAppsShow, "apps create": cmdAppsCreate, "apps set": cmdAppsSet,
	"apps deploy": cmdAppsDeploy, "apps wait": cmdAppsWait, "apps logs": cmdAppsLogs,
	"apps start": ctl("apps", "start", "Aplicação ligada."), "apps stop": ctl("apps", "stop", "Aplicação desligada."),
	"apps restart": ctl("apps", "restart", "Reinício feito."), "apps delete": ctl("apps", "delete", "Aplicação excluída."),
	"apps env": cmdEnv, "apps env-set": cmdEnvSet, "apps env-unset": cmdEnvUnset,
	"apps domain-add": cmdDomainAdd, "apps domain-remove": cmdDomainRemove, "apps maintenance": cmdMaintenance,
	"apps auth-add": cmdAuthAdd, "apps auth-remove": cmdAuthRemove, "apps limits": cmdLimits,
	"dbs list": cmdDBList, "dbs show": cmdDBShow, "dbs create": cmdDBCreate,
	"dbs start": ctl("databases", "start", "Banco ligado."), "dbs stop": ctl("databases", "stop", "Banco desligado."),
	"dbs restart": ctl("databases", "restart", "Banco reiniciado."), "dbs backup": ctl("databases", "backup", "Backup iniciado; leva alguns instantes."),
	"dbs regenerate": ctl("databases", "regenerate", "Senha trocada. Faça um novo deploy das apps que usam este banco."),
	"dbs delete":     ctl("databases", "delete", "Banco excluído."),
	"raw":            cmdRaw,
}

func writeJSON(w io.Writer, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(w, string(b))
}

func table(w io.Writer, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ---- general ----

func cmdStatus(x *call) error {
	raw, err := x.c.get("/api/overview")
	if err != nil {
		return err
	}
	if x.json {
		x.out.Write(raw)
		return nil
	}
	var ov struct {
		Host struct {
			MemTotal  int64      `json:"mem_total"`
			MemUsed   int64      `json:"mem_used"`
			Load      [3]float64 `json:"load"`
			Uptime    int64      `json:"uptime"`
			DiskTotal int64      `json:"disk_total"`
			DiskUsed  int64      `json:"disk_used"`
			CPUs      int        `json:"cpus"`
		} `json:"host"`
		Cache int64 `json:"cache"`
		Res   map[string]struct {
			Type   string   `json:"type"`
			State  string   `json:"state"`
			Status string   `json:"status"`
			CPU    *float64 `json:"cpu"`
			Mem    *int64   `json:"mem"`
		} `json:"res"`
	}
	if err := json.Unmarshal(raw, &ov); err != nil {
		return fail("resposta inesperada do painel")
	}
	h := ov.Host
	fmt.Fprintf(x.out, "Servidor: %d CPUs, carga %.2f %.2f %.2f, memória %s de %s, disco %s de %s, ligado há %s\n",
		h.CPUs, h.Load[0], h.Load[1], h.Load[2], humanBytes(h.MemUsed), humanBytes(h.MemTotal),
		humanBytes(h.DiskUsed), humanBytes(h.DiskTotal), (time.Duration(h.Uptime) * time.Second).Round(time.Minute))
	fmt.Fprintf(x.out, "Cache de builds: %s (limpar: brpanel cache prune)\n", humanBytes(ov.Cache))
	if len(ov.Res) == 0 {
		fmt.Fprintln(x.out, "Nenhuma aplicação ou banco ainda. Crie uma app com: brpanel apps create --repo URL")
		return nil
	}
	rows := [][]string{{"NOME", "TIPO", "ESTADO", "STATUS", "CPU", "MEMÓRIA"}}
	for _, n := range sortedKeys(ov.Res) {
		r := ov.Res[n]
		cpu, mem := "-", "-"
		if r.CPU != nil {
			cpu = fmt.Sprintf("%.1f%%", *r.CPU)
		}
		if r.Mem != nil {
			mem = humanBytes(*r.Mem)
		}
		rows = append(rows, []string{n, r.Type, dash(r.State), dash(r.Status), cpu, mem})
	}
	table(x.out, rows)
	return nil
}

func cmdEvents(x *call) error {
	limit := 20
	if n, ok, err := x.num("limit"); err != nil {
		return err
	} else if ok {
		limit = max(1, min(n, 30))
	}
	var ov struct {
		Events []struct {
			TS       time.Time `json:"ts"`
			Kind     string    `json:"kind"`
			Resource string    `json:"resource"`
			Message  string    `json:"message"`
		} `json:"events"`
	}
	if err := x.c.getJSON("/api/overview", &ov); err != nil {
		return err
	}
	ev := ov.Events[:min(limit, len(ov.Events))]
	if x.json {
		writeJSON(x.out, ev)
		return nil
	}
	if len(ev) == 0 {
		fmt.Fprintln(x.out, "Nenhum evento ainda.")
	}
	for _, e := range ev {
		fmt.Fprintf(x.out, "%s  %-8s %-16s %s\n", e.TS.Local().Format("2006-01-02 15:04:05"), e.Kind, e.Resource, e.Message)
	}
	return nil
}

func cmdGuide(x *call) error { fmt.Fprint(x.out, Guide()); return nil }

func cmdHelp(x *call) error {
	if len(x.pos) == 0 {
		fmt.Fprint(x.out, HelpOverview())
		return nil
	}
	c, _ := resolve(x.pos)
	if c == nil {
		return unknownCommand(x.pos)
	}
	fmt.Fprint(x.out, HelpCommand(*c))
	return nil
}

// ---- apps ----

func getApp(x *call, name string) (AppInfo, error) {
	var a AppInfo
	err := x.c.getJSON("/api/apps/"+url.PathEscape(name), &a)
	return a, err
}

func cmdAppsList(x *call) error {
	var apps []AppInfo
	if err := x.c.getJSON("/api/apps", &apps); err != nil {
		return err
	}
	if x.json {
		for i := range apps {
			apps[i].Env = ""
		}
		writeJSON(x.out, apps)
		return nil
	}
	if len(apps) == 0 {
		fmt.Fprintln(x.out, "Nenhuma aplicação. Crie uma com: brpanel apps create --repo https://github.com/exemplo/app")
		return nil
	}
	rows := [][]string{{"NOME", "ESTADO", "STATUS", "INSTÂNCIAS", "DOMÍNIO", "REPOSITÓRIO"}}
	for _, a := range apps {
		rows = append(rows, []string{a.Name, dash(a.State), dash(a.Status), strconv.Itoa(a.Replicas), dash(a.Domain), a.Repo})
	}
	table(x.out, rows)
	return nil
}

func cmdAppsShow(x *call) error {
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	a.Env = "" // `apps env` is the explicit way to see variables, which may hold secrets
	if x.json {
		writeJSON(x.out, a)
		return nil
	}
	rows := [][]string{
		{"nome:", a.Name}, {"estado:", dash(a.State)}, {"status do deploy:", dash(a.Status)}, {"último deploy:", dash(a.LastDeploy)},
		{"repositório:", a.Repo}, {"branch:", a.Branch}, {"pasta no repositório:", dash(a.RootDir)}, {"pastas extras:", dash(strings.ReplaceAll(a.WatchPaths, "\n", ", "))}, {"tipo detectado:", dash(a.Kind)},
		{"build:", dash(a.BuildCmd)}, {"início:", dash(a.StartCmd)}, {"porta:", strconv.Itoa(a.Port)},
		{"domínio:", dash(a.Domain)}, {"url:", dash(a.URL)},
		{"instâncias:", strconv.Itoa(a.Replicas)}, {"memória (MB):", strconv.Itoa(a.MemoryMB)}, {"cpu (%):", strconv.Itoa(a.CPUPercent)},
		{"rate limit (req/s):", strconv.Itoa(a.RateLimit)}, {"deploy automático:", yesNo(a.AutoDeploy)},
		{"autoscale:", yesNo(a.AutoScale.Enabled)}, {"manutenção:", yesNo(a.Maintenance)},
		{"upload máx (MB):", strconv.Itoa(a.UploadMB)}, {"timeout leitura (s):", strconv.Itoa(a.ReadTimeout)},
		{"usuários com senha:", dash(strings.Join(a.AuthUsers, ", "))},
	}
	if a.Monorepo {
		rows = append(rows, []string{"monorepo:", "sim; outras pastas: " + strings.Join(a.MonorepoApps, ", ")})
	}
	if len(a.MissingEnv) > 0 {
		rows = append(rows, []string{"variáveis faltando:", strings.Join(a.MissingEnv, ", ") + "  (defina com: brpanel apps env-set " + a.Name + " CHAVE=valor)"})
	}
	if len(a.OptionalEnv) > 0 {
		rows = append(rows, []string{"variáveis opcionais:", strings.Join(a.OptionalEnv, ", ")})
	}
	if a.PortNote != "" {
		rows = append(rows, []string{"aviso de porta:", a.PortNote + "  (detalhes no log do último deploy: brpanel apps logs " + a.Name + " --kind deploy)"})
	}
	for _, d := range a.Domains {
		kind := "alias"
		if d.To != "" {
			kind = "redireciona para " + d.To
		}
		rows = append(rows, []string{"domínio extra:", d.Name + " (" + kind + ")"})
	}
	table(x.out, rows)
	return nil
}

func yesNo(b bool) string {
	if b {
		return "sim"
	}
	return "não"
}

// appForm builds the fields of the panel's app form from the current app plus the flags given.
func appForm(x *call, b AppInfo) (url.Values, error) {
	v := url.Values{}
	var err error
	str := func(key, flag, cur string) {
		if x.set(flag) {
			cur = x.s(flag)
		}
		v.Set(key, cur)
	}
	num := func(key, flag string, cur int) {
		n, ok, e := x.num(flag)
		if e != nil && err == nil {
			err = e
		}
		if ok {
			cur = n
		}
		v.Set(key, strconv.Itoa(cur))
	}
	sw := func(key, flag string, cur bool) {
		if x.set(flag) {
			cur = x.on(flag)
		}
		if cur {
			v.Set(key, "on")
		}
	}
	str("repo", "repo", b.Repo)
	str("branch", "branch", b.Branch)
	str("root_dir", "root", b.RootDir)
	str("watch_paths", "watch", b.WatchPaths)
	str("domain", "domain", b.Domain)
	str("build_cmd", "build", b.BuildCmd)
	str("start_cmd", "start", b.StartCmd)
	num("port", "port", b.Port)
	num("replicas", "replicas", b.Replicas)
	num("memory_mb", "memory-mb", b.MemoryMB)
	num("cpu_percent", "cpu-percent", b.CPUPercent)
	num("rate_limit", "rate-limit", b.RateLimit)
	sw("auto_deploy", "auto-deploy", b.AutoDeploy)
	sw("autoscale", "autoscale", b.AutoScale.Enabled)
	num("scale_min", "scale-min", b.AutoScale.Min)
	num("scale_max", "scale-max", b.AutoScale.Max)
	num("scale_cpu", "scale-cpu", b.AutoScale.CPU)
	num("scale_mem", "scale-mem", b.AutoScale.Mem)
	v.Set("scale_out_cd", strconv.Itoa(b.AutoScale.OutCD))
	v.Set("scale_in_cd", strconv.Itoa(b.AutoScale.InCD))
	v.Set("env", b.Env)
	return v, err
}

func cmdAppsCreate(x *call) error {
	if !x.set("repo") {
		return usageErr(x.cmd, "faltou --repo (o repositório Git da aplicação)")
	}
	form, err := appForm(x, AppInfo{Branch: "main", Replicas: 1})
	if err != nil {
		return err
	}
	for _, kv := range x.list("env") {
		if !strings.Contains(kv, "=") || strings.HasPrefix(kv, "=") {
			return usageErr(x.cmd, fmt.Sprintf("--env precisa ser CHAVE=VALOR, recebi %q", kv))
		}
	}
	form.Set("env", strings.Join(x.list("env"), "\n"))
	form.Set("name", x.s("name"))
	_, loc, err := x.c.act("POST", "/apps", form)
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(loc, "/apps/")
	if name == "" || name == loc {
		name = x.s("name")
	}
	if x.on("wait") {
		return waitDeploy(x, name)
	}
	x.ok(fmt.Sprintf("aplicação %q criada; o primeiro deploy começou. Acompanhe com: brpanel apps wait %s", name, name))
	return nil
}

func cmdAppsSet(x *call) error {
	cur, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	form, err := appForm(x, cur)
	if err != nil {
		return err
	}
	msg, _, err := x.c.act("POST", "/apps/"+url.PathEscape(cur.Name)+"/edit", form)
	if err != nil {
		return err
	}
	x.ok(dflt(msg, "Configuração salva.") + " Para aplicar rode: brpanel apps deploy " + cur.Name + " --wait")
	return nil
}

func dflt(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func cmdAppsDeploy(x *call) error {
	name := x.pos[0]
	action := "deploy"
	if x.on("clean") {
		action = "deploy-clean"
	}
	msg, _, err := x.c.act("POST", "/apps/"+url.PathEscape(name)+"/"+action, nil)
	if err != nil && !(x.on("wait") && strings.HasPrefix(err.Error(), "Já existe um deploy")) {
		return err
	}
	_ = msg
	if x.on("wait") {
		time.Sleep(pollEvery / 2)
		return waitDeploy(x, name)
	}
	if x.on("clean") {
		x.ok("deploy do zero iniciado em segundo plano (código, dependências e cache do app apagados). Para esperar o fim rode: brpanel apps wait " + name)
		return nil
	}
	x.ok("deploy iniciado em segundo plano. Para esperar o fim rode: brpanel apps wait " + name)
	return nil
}

func cmdAppsWait(x *call) error { return waitDeploy(x, x.pos[0]) }

func waitDeploy(x *call, name string) error {
	secs := 600
	if n, ok, err := x.num("timeout"); err != nil {
		return err
	} else if ok {
		secs = max(n, 1)
	}
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for {
		a, err := getApp(x, name)
		if err != nil {
			return err
		}
		if !a.Deploying {
			if a.Status == "failed" {
				return fail("o deploy de "+name+" falhou",
					"DICA: veja o motivo no fim do log: brpanel apps logs "+name+" --kind deploy --lines 80",
					"DICA: corrija (código, variáveis ou comandos) e rode: brpanel apps deploy "+name+" --wait")
			}
			msg := fmt.Sprintf("deploy de %s concluído (status %s, estado %s).", name, dash(a.Status), dash(a.State))
			if a.PortNote != "" { // the deploy worked, but the app does not behave: say so now, not in a log nobody opens
				msg += " AVISO DE PORTA: " + a.PortNote + ". Detalhes e como corrigir: brpanel apps logs " + name + " --kind deploy --lines 40"
			}
			x.ok(msg)
			return nil
		}
		if time.Now().After(deadline) {
			return fail(fmt.Sprintf("o deploy de %s ainda está rodando depois de %d s", name, secs),
				"DICA: continue esperando com: brpanel apps wait "+name,
				"DICA: ou veja o andamento: brpanel apps logs "+name+" --kind deploy --lines 50")
		}
		time.Sleep(pollEvery)
	}
}

func cmdAppsLogs(x *call) error {
	kind := dflt(x.s("kind"), "service")
	if kind != "service" && kind != "deploy" {
		return usageErr(x.cmd, "--kind precisa ser service ou deploy")
	}
	q := url.Values{"kind": {kind}}
	if n, ok, err := x.num("lines"); err != nil {
		return err
	} else if ok {
		q.Set("lines", strconv.Itoa(n))
	}
	b, err := x.c.get("/apps/" + url.PathEscape(x.pos[0]) + "/log?" + q.Encode())
	if err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, map[string]string{"app": x.pos[0], "kind": kind, "log": string(b)})
		return nil
	}
	fmt.Fprint(x.out, string(b))
	return nil
}

// ctl builds the handler of a one-word action (POST /<group>/<name>/<action>).
func ctl(group, action, done string) func(*call) error {
	return func(x *call) error {
		msg, _, err := x.c.act("POST", "/"+group+"/"+url.PathEscape(x.pos[0])+"/"+action, nil)
		if err != nil {
			return err
		}
		if action == "start" || action == "stop" || action == "restart" {
			msg = "" // the panel says "Ação executada: start"; ours is clearer
		}
		x.ok(dflt(msg, done))
		return nil
	}
}

// ---- variables ----

func cmdEnv(x *call) error {
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, map[string]string{"app": a.Name, "env": a.Env})
		return nil
	}
	if strings.TrimSpace(a.Env) == "" {
		fmt.Fprintln(x.out, "(nenhuma variável definida)")
		return nil
	}
	fmt.Fprintln(x.out, strings.TrimRight(a.Env, "\n"))
	return nil
}

// editEnv rewrites env: lines whose key is in set are replaced (new keys are appended in the given
// order), lines whose key is in unset are dropped, everything else is kept untouched.
func editEnv(env string, order []string, set map[string]string, unset map[string]bool) string {
	var out []string
	done := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(env, "\r\n", "\n"), "\n") {
		key, _, has := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		switch {
		case has && unset[key]:
		case has && hasKey(set, key):
			if !done[key] { // a repeated key keeps only its first line
				out = append(out, key+"="+set[key])
				done[key] = true
			}
		case strings.TrimSpace(line) != "":
			out = append(out, line)
		}
	}
	for _, k := range order {
		if !done[k] {
			out = append(out, k+"="+set[k])
			done[k] = true
		}
	}
	return strings.Join(out, "\n")
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

func saveEnv(x *call, name, env string) error {
	form := url.Values{"env": {env}}
	if x.on("deploy") {
		form.Set("deploy", "1")
	}
	msg, _, err := x.c.act("POST", "/apps/"+url.PathEscape(name)+"/env", form)
	if err != nil {
		return err
	}
	x.ok(dflt(msg, "Variáveis salvas."))
	return nil
}

func cmdEnvSet(x *call) error {
	set, order := map[string]string{}, []string{}
	for _, kv := range x.pos[1:] {
		k, v, ok := strings.Cut(kv, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return usageErr(x.cmd, fmt.Sprintf("%q não está no formato CHAVE=VALOR", kv))
		}
		if !hasKey(set, k) {
			order = append(order, k)
		}
		set[k] = v
	}
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	return saveEnv(x, a.Name, editEnv(a.Env, order, set, nil))
}

func cmdEnvUnset(x *call) error {
	unset := map[string]bool{}
	for _, k := range x.pos[1:] {
		unset[k] = true
	}
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	return saveEnv(x, a.Name, editEnv(a.Env, nil, nil, unset))
}

// ---- site ----

func site(x *call, action string, form url.Values, done string) error {
	msg, _, err := x.c.act("POST", "/apps/"+url.PathEscape(x.pos[0])+"/site/"+action, form)
	if err != nil {
		return err
	}
	x.ok(dflt(msg, done))
	return nil
}

func cmdDomainAdd(x *call) error {
	f := url.Values{"domain": {x.pos[1]}, "kind": {"serve"}}
	if to := x.s("redirect-to"); to != "" {
		f.Set("kind", "redirect")
		f.Set("to", to)
		if x.on("permanent") {
			f.Set("permanent", "on")
		}
	}
	return site(x, "domain-add", f, "Domínio adicionado.")
}

func cmdDomainRemove(x *call) error {
	return site(x, "domain-delete", url.Values{"domain": {x.pos[1]}}, "Domínio removido.")
}

func cmdMaintenance(x *call) error {
	state := strings.ToLower(x.pos[1])
	if state != "on" && state != "off" {
		return usageErr(x.cmd, "o segundo argumento precisa ser on ou off")
	}
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	f := url.Values{"state": {state}, "title": {dflt(x.s("title"), a.MaintTitle)}, "text": {a.MaintText}}
	if x.set("text") {
		f.Set("text", x.s("text"))
	}
	return site(x, "maintenance", f, "Manutenção atualizada.")
}

func cmdAuthAdd(x *call) error {
	if !x.on("password-stdin") {
		return usageErr(x.cmd, "a senha não vai na linha de comando; envie pela entrada padrão e use --password-stdin")
	}
	line, _ := bufio.NewReader(x.in).ReadString('\n')
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return usageErr(x.cmd, "a entrada padrão veio vazia: envie a senha, por exemplo com printf '%s' 'senha' | brpanel ...")
	}
	return site(x, "auth-add", url.Values{"user": {x.pos[1]}, "password": {pw}}, "Usuário salvo.")
}

func cmdAuthRemove(x *call) error {
	return site(x, "auth-delete", url.Values{"user": {x.pos[1]}}, "Usuário removido.")
}

func cmdLimits(x *call) error {
	a, err := getApp(x, x.pos[0])
	if err != nil {
		return err
	}
	up, rd := a.UploadMB, a.ReadTimeout
	if n, ok, err := x.num("upload-mb"); err != nil {
		return err
	} else if ok {
		up = n
	}
	if n, ok, err := x.num("read-timeout"); err != nil {
		return err
	} else if ok {
		rd = n
	}
	f := url.Values{"upload_mb": {zeroEmpty(up)}, "read_timeout": {zeroEmpty(rd)}}
	return site(x, "limits", f, "Limites salvos.")
}

func zeroEmpty(n int) string { // the panel reads an empty field as "use the default"
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// ---- databases ----

func cmdDBList(x *call) error {
	var dbs []DBInfo
	if err := x.c.getJSON("/api/databases", &dbs); err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, dbs)
		return nil
	}
	if len(dbs) == 0 {
		fmt.Fprintln(x.out, "Nenhum banco. Crie um com: brpanel dbs create --kind postgres --name pg")
		return nil
	}
	rows := [][]string{{"NOME", "TIPO", "ESTADO", "STATUS", "PORTA", "USADO POR"}}
	for _, d := range dbs {
		rows = append(rows, []string{d.Name, d.Kind, dash(d.State), dash(d.Status), strconv.Itoa(d.Port), dash(strings.Join(d.UsedBy, ","))})
	}
	table(x.out, rows)
	return nil
}

func cmdDBShow(x *call) error {
	var dbs []DBInfo
	if err := x.c.getJSON("/api/databases", &dbs); err != nil {
		return err
	}
	var names []string
	for _, d := range dbs {
		names = append(names, d.Name)
		if d.Name != x.pos[0] {
			continue
		}
		if x.json {
			writeJSON(x.out, d)
			return nil
		}
		table(x.out, [][]string{{"nome:", d.Name}, {"tipo:", d.Kind}, {"estado:", dash(d.State)}, {"status:", dash(d.Status)},
			{"porta:", strconv.Itoa(d.Port)}, {"acesso externo:", yesNo(d.Public)}, {"backup agendado:", dash(d.Schedule)},
			{"criado em:", d.Created}, {"usado por:", dash(strings.Join(d.UsedBy, ", "))}})
		fmt.Fprintln(x.out, "Para conectar uma app, use estas referências nas variáveis (brpanel apps env-set APP CHAVE='${{...}}'):")
		for _, r := range d.Refs {
			fmt.Fprintln(x.out, "  "+r)
		}
		return nil
	}
	return fail(fmt.Sprintf("banco %q não existe", x.pos[0]), "DICA: bancos existentes: "+dash(strings.Join(names, ", ")))
}

func cmdDBCreate(x *call) error {
	if !x.set("kind") || !x.set("name") {
		return usageErr(x.cmd, "faltou --kind e/ou --name")
	}
	_, _, err := x.c.act("POST", "/databases", url.Values{"kind": {x.s("kind")}, "name": {x.s("name")}})
	if err != nil {
		return err
	}
	x.ok(fmt.Sprintf("banco %q criado; a preparação roda em segundo plano. Acompanhe com: brpanel dbs show %s", x.s("name"), x.s("name")))
	return nil
}

// ---- raw ----

func cmdRaw(x *call) error {
	method := strings.ToUpper(x.pos[0])
	path := x.pos[1]
	if method != "GET" && method != "POST" || !strings.HasPrefix(path, "/") {
		return usageErr(x.cmd, "use GET ou POST e um caminho que começa com /")
	}
	form := url.Values{}
	for _, kv := range x.pos[2:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return usageErr(x.cmd, fmt.Sprintf("%q não está no formato campo=valor", kv))
		}
		form.Add(k, v)
	}
	if method == "GET" {
		b, err := x.c.get(path)
		if err != nil {
			return err
		}
		x.out.Write(b)
		return nil
	}
	msg, loc, err := x.c.act("POST", path, form)
	if err != nil {
		return err
	}
	x.ok(dflt(msg, "feito (redirecionou para "+dash(loc)+")"))
	return nil
}
