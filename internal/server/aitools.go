package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/ai"
	"github.com/viniciusdev772/br_panel/internal/deploy"
	"github.com/viniciusdev772/br_panel/internal/detect"
	"github.com/viniciusdev772/br_panel/internal/sysinfo"
	"github.com/viniciusdev772/br_panel/internal/validate"
)

// The assistant's extra tools. They only read: none changes the server, so none asks for approval.
// What they return is data for the model, never instructions (the system prompt says so too).
const (
	aiReadCap    = 12 << 10 // bytes of a file repo_read returns
	aiProbeCap   = 1500     // bytes of a response body app_probe returns
	aiTreeLimit  = 300
	aiTreeDepth  = 4
	aiProbeLimit = 5 * time.Second
)

type obj = map[string]any

// aiExtraTools are offered next to the CLI commands (see ai.Tools).
func aiExtraTools() []ai.Tool {
	app := obj{"type": "string", "description": "Nome da aplicação"}
	return []ai.Tool{
		{Name: "repo_tree", Desc: "Lista os arquivos do código de uma aplicação já publicada (sem node_modules, .git e pastas de build). Use para entender a estrutura do projeto.",
			Schema: obj{"type": "object", "required": []string{"app"}, "properties": obj{"app": app,
				"path": obj{"type": "string", "description": "Subpasta a listar (opcional, relativa à pasta do projeto)"}}}},
		{Name: "repo_read", Desc: "Lê um arquivo de texto do código de uma aplicação publicada (até 12 KB). Não lê segredos (.env, chaves). Use para achar a linha que fixa a porta, ver scripts do package.json, Dockerfile, settings etc.",
			Schema: obj{"type": "object", "required": []string{"app", "path"}, "properties": obj{"app": app,
				"path": obj{"type": "string", "description": "Caminho relativo à pasta do projeto, ex.: package.json ou src/server.js"}}}},
		{Name: "app_probe", Desc: "Faz uma requisição GET local (127.0.0.1) à porta da aplicação e devolve o status, alguns cabeçalhos e o início da resposta. Use para saber se o app responde de fato.",
			Schema: obj{"type": "object", "required": []string{"app"}, "properties": obj{"app": app,
				"path": obj{"type": "string", "description": "Caminho da URL, começando por /. Padrão: /"}}}},
		{Name: "ports", Desc: "Lista as portas TCP em escuta no servidor. Com port, diz também qual serviço systemd (ou comando) segura essa porta.",
			Schema: obj{"type": "object", "properties": obj{"port": obj{"type": "integer", "description": "Porta a investigar (opcional)"}}}},
		{Name: "dns_lookup", Desc: "Resolve um domínio e devolve os endereços IP para onde ele aponta. Compare com o IP do servidor para saber se o DNS está certo antes de emitir o SSL.",
			Schema: obj{"type": "object", "required": []string{"domain"}, "properties": obj{"domain": obj{"type": "string", "description": "Domínio, ex.: app.exemplo.com.br"}}}},
		{Name: "repo_detect", Desc: "Faz um clone temporário de um repositório Git e diz como o painel o detectaria (tipo, comandos de build e início, programas que faltam no servidor, variáveis que o código lê, projetos de um monorepo). Use antes de criar um app.",
			Schema: obj{"type": "object", "required": []string{"repo"}, "properties": obj{
				"repo":     obj{"type": "string", "description": "URL do repositório (https://… ou git@…)"},
				"branch":   obj{"type": "string", "description": "Branch (padrão main)"},
				"root_dir": obj{"type": "string", "description": "Pasta do projeto num monorepo (opcional)"}}}},
	}
}

func isExtraTool(name string) bool {
	for _, t := range aiExtraTools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// aiDescribe renders a call for the browser: the CLI line for commands, the name and arguments for extras.
func aiDescribe(name string, args json.RawMessage) string {
	if isExtraTool(name) {
		var in map[string]any
		if json.Unmarshal(args, &in) != nil || len(in) == 0 {
			return name
		}
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := []string{name}
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, in[k]))
		}
		return strings.Join(parts, " ")
	}
	return ai.Describe(name, args)
}

// aiRun runs one tool: an extra one here, a CLI command through aiExec.
func (s *Server) aiRun(ctx context.Context, name string, args json.RawMessage, approved bool) string {
	if !isExtraTool(name) {
		return s.aiExec(ctx, name, args, approved)
	}
	var in struct {
		App     string `json:"app"`
		Path    string `json:"path"`
		Port    int    `json:"port"`
		Domain  string `json:"domain"`
		Repo    string `json:"repo"`
		Branch  string `json:"branch"`
		RootDir string `json:"root_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "ERRO: os argumentos precisam ser um objeto JSON"
	}
	var out string
	var err error
	switch name {
	case "repo_tree":
		out, err = s.toolRepoTree(in.App, in.Path)
	case "repo_read":
		out, err = s.toolRepoRead(in.App, in.Path)
	case "app_probe":
		out, err = s.toolProbe(ctx, in.App, in.Path)
	case "ports":
		out = toolPorts(in.Port)
	case "dns_lookup":
		out, err = toolDNS(ctx, in.Domain)
	case "repo_detect":
		out, err = s.toolRepoDetect(ctx, in.Repo, in.Branch, in.RootDir)
	}
	if err != nil {
		return "ERRO: " + err.Error()
	}
	if len(out) > aiMaxOutput {
		out = strings.ToValidUTF8(out[:aiMaxOutput], "") + "\n…(cortado)"
	}
	return out
}

// projectDir is the folder of an app's code, checked: the name must exist and the folder must not
// be reached through a link (the repository is not trusted and the panel reads it as root).
func (s *Server) projectDir(app string) (string, error) {
	if err := validate.Name(app); err != nil {
		return "", err
	}
	a, ok := s.store.Get(app)
	if !ok {
		return "", fmt.Errorf("aplicação %q não encontrada (veja apps_list)", app)
	}
	dir := s.dep.ProjectDir(a)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("o código de %q ainda não foi baixado (faça um deploy)", app)
	}
	return dir, nil
}

// inside resolves rel within dir and refuses anything that leaves it, including through links.
func inside(dir, rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return dir, nil
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("use um caminho relativo à pasta do projeto")
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." {
			return "", fmt.Errorf("caminho fora da pasta do projeto")
		}
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	realDir, err1 := filepath.EvalSymlinks(dir)
	realFull, err2 := filepath.EvalSymlinks(full)
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("%q não existe", rel)
	}
	if realFull != realDir && !strings.HasPrefix(realFull, realDir+string(filepath.Separator)) {
		return "", fmt.Errorf("caminho fora da pasta do projeto")
	}
	return realFull, nil
}

// secretName reports files the assistant must never read: they hold credentials.
func secretName(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	if strings.HasPrefix(n, ".env") {
		return !(strings.HasSuffix(n, ".example") || strings.HasSuffix(n, ".sample") || strings.HasSuffix(n, ".template"))
	}
	switch {
	case strings.HasSuffix(n, ".pem"), strings.HasSuffix(n, ".key"), strings.HasSuffix(n, ".p12"), strings.HasSuffix(n, ".pfx"),
		strings.HasSuffix(n, ".keystore"), strings.HasPrefix(n, "id_rsa"), strings.HasPrefix(n, "id_ed25519"), strings.HasPrefix(n, "id_ecdsa"),
		n == ".npmrc", n == ".netrc", n == ".pypirc", n == ".git-credentials", strings.HasPrefix(n, "credentials"), strings.HasPrefix(n, "secrets"):
		return true
	}
	return false
}

func (s *Server) toolRepoTree(app, rel string) (string, error) {
	dir, err := s.projectDir(app)
	if err != nil {
		return "", err
	}
	base, err := inside(dir, rel)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%q não é uma pasta", rel)
	}
	lines, _, cut := detect.Tree(base, aiTreeLimit, aiTreeDepth)
	if len(lines) == 0 {
		return "(pasta vazia)", nil
	}
	out := strings.Join(lines, "\n")
	if cut {
		out += "\n…(lista cortada: peça uma subpasta)"
	}
	return out, nil
}

func (s *Server) toolRepoRead(app, rel string) (string, error) {
	dir, err := s.projectDir(app)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("informe o caminho do arquivo")
	}
	if secretName(rel) {
		return "", fmt.Errorf("este arquivo pode conter segredos e não é lido pelo assistente")
	}
	full, err := inside(dir, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(full)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%q não é um arquivo", rel)
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf, _ := io.ReadAll(io.LimitReader(f, aiReadCap+1))
	if strings.IndexByte(string(buf), 0) >= 0 {
		return "", fmt.Errorf("arquivo binário")
	}
	text := strings.ToValidUTF8(string(buf), "")
	if len(buf) > aiReadCap {
		text = text[:aiReadCap] + "\n…(arquivo cortado em 12 KB)"
	}
	return text, nil
}

func (s *Server) toolProbe(ctx context.Context, app, path string) (string, error) {
	if err := validate.Name(app); err != nil {
		return "", err
	}
	a, ok := s.store.Get(app)
	if !ok {
		return "", fmt.Errorf("aplicação %q não encontrada (veja apps_list)", app)
	}
	if a.Port == 0 {
		return "", fmt.Errorf("a aplicação ainda não tem porta (faça um deploy)")
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \r\n\x00") || len(path) > 300 {
		return "", fmt.Errorf("o caminho precisa começar por / e não ter espaços")
	}
	ctx, cancel := context.WithTimeout(ctx, aiProbeLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(a.Port)+path, nil)
	if err != nil {
		return "", err
	}
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("sem resposta em 127.0.0.1:%d: %v (o app não está escutando nessa porta ou caiu)", a.Port, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, aiProbeCap))
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s -> %s em %d ms\n", http.MethodGet, path, resp.Status, time.Since(started).Milliseconds())
	for _, h := range []string{"Content-Type", "Server", "Location", "X-Powered-By", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", h, v)
		}
	}
	b.WriteString("\n" + strings.ToValidUTF8(string(body), ""))
	return b.String(), nil
}

func toolPorts(port int) string {
	ports := map[int]bool{}
	for _, l := range sysinfo.HostListeners() {
		ports[l.Port] = true
	}
	list := make([]int, 0, len(ports))
	for p := range ports {
		list = append(list, p)
	}
	sort.Ints(list)
	var b strings.Builder
	if len(list) == 0 {
		b.WriteString("Nenhuma porta TCP em escuta foi lida (este servidor não expõe /proc/net/tcp).\n")
	} else {
		strs := make([]string, len(list))
		for i, p := range list {
			strs[i] = strconv.Itoa(p)
		}
		b.WriteString("Portas TCP em escuta: " + strings.Join(strs, ", ") + "\n")
	}
	if port > 0 {
		if err := validate.Port(port); err != nil {
			return b.String() + "ERRO: " + err.Error()
		}
		if h := sysinfo.PortHolders(port); len(h) > 0 {
			b.WriteString(fmt.Sprintf("A porta %d é usada por: %s\n", port, strings.Join(h, ", ")))
		} else if ports[port] {
			b.WriteString(fmt.Sprintf("A porta %d está em escuta, mas não achei o serviço dono dela.\n", port))
		} else {
			b.WriteString(fmt.Sprintf("A porta %d está livre.\n", port))
		}
	}
	return b.String()
}

func toolDNS(ctx context.Context, domain string) (string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if err := validate.Domain(domain); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, aiProbeLimit)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, domain)
	if err != nil {
		return "", fmt.Errorf("%s não resolve: %v (o domínio não existe ou o registro DNS ainda não foi criado)", domain, err)
	}
	sort.Strings(ips)
	return domain + " aponta para: " + strings.Join(ips, ", "), nil
}

func (s *Server) toolRepoDetect(ctx context.Context, repo, branch, root string) (string, error) {
	repo, branch = strings.TrimSpace(repo), strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	if err := validate.Repo(repo); err != nil {
		return "", err
	}
	if err := validate.Branch(branch); err != nil {
		return "", err
	}
	root, err := validate.RootDir(root)
	if err != nil {
		return "", err
	}
	res, err := s.dep.DetectRepo(ctx, repo, branch, root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Nome sugerido: %s\n", deploy.SuggestNameIn(repo, root))
	if res.Det == nil {
		b.WriteString("Tipo: não reconhecido pelo painel.\n")
	} else {
		fmt.Fprintf(&b, "Tipo: %s\nBuild: %s\nInício: %s\n", res.Det.Kind, orDash(res.Det.Build), orDash(res.Det.Start))
		for _, n := range res.Det.Notes {
			b.WriteString("Nota: " + n + "\n")
		}
	}
	if len(res.Missing) > 0 {
		b.WriteString("Falta instalar no servidor: " + strings.Join(res.Missing, ", ") + "\n")
	}
	if res.Monorepo || len(res.Apps) > 0 {
		b.WriteString("Projetos no repositório:\n")
		for _, c := range res.Apps {
			fmt.Fprintf(&b, "- %s (%s)%s\n", c.Path, c.Kind, map[bool]string{true: "", false: " [biblioteca]"}[c.Deployable])
		}
	}
	var req []string
	for _, e := range res.Env {
		if e.Required {
			req = append(req, e.Name)
		}
	}
	if len(req) > 0 {
		b.WriteString("Variáveis que o código lê, sem valor padrão: " + strings.Join(req, ", ") + "\n")
	}
	return b.String(), nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
