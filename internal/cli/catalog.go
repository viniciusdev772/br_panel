// Package cli is the command line of BR Panel: the same `brpanel` binary, run with a command
// (`brpanel apps list`), talks to the running panel through a root-only Unix socket. This file is the
// single catalog of commands: it feeds --help, the guide for AI agents (`brpanel guide`) and the
// /cli page of the panel, so the three can never disagree.
package cli

import "strings"

// Flag is one option of a command.
type Flag struct {
	Name  string // without dashes
	Value string // placeholder shown in help; empty for a switch
	Desc  string
}

// Cmd is one command of the CLI.
type Cmd struct {
	Group    string
	Name     string   // canonical name, e.g. "apps deploy"
	Aliases  []string // other full names that run the same command
	Args     string   // positional arguments, e.g. "NOME"
	Min      int      // how many positional arguments are required
	Summary  string
	Flags    []Flag
	Examples []string
	Danger   bool // destructive: refuses to run without --yes
}

// Usage is the one-line syntax, e.g. "brpanel apps deploy NOME [--wait]".
func (c Cmd) Usage() string {
	u := "brpanel " + c.Name
	if c.Args != "" {
		u += " " + c.Args
	}
	for _, f := range c.Flags {
		if f.Value == "" {
			u += " [--" + f.Name + "]"
		} else {
			u += " [--" + f.Name + " " + f.Value + "]"
		}
	}
	if c.Danger {
		u += " --yes"
	}
	return u
}

// globalFlags work on every command.
var globalFlags = []Flag{
	{"json", "", "imprime a resposta em JSON (para programas lerem)"},
	{"yes", "", "confirma comandos que apagam dados"},
	{"data", "PASTA", "pasta de dados do painel (padrão /var/lib/brpanel)"},
	{"local", "", "usa o socket deste servidor mesmo com um login remoto salvo"},
}

// appFlags are shared by `apps create` and `apps set`.
var appFlags = []Flag{
	{"branch", "BRANCH", "branch do Git (padrão main)"},
	{"root", "PASTA", "monorepo: pasta da aplicação dentro do repositório, como apps/web (vazio = repositório inteiro)"},
	{"watch", "PADROES", "monorepo: pastas extras que também publicam a app, separadas por vírgula (! no começo ignora), ex.: 'packages/ui/**,!**/*.md'"},
	{"domain", "DOMINIO", "domínio do site; o painel configura Nginx e SSL"},
	{"port", "PORTA", "porta do app (vazio = o painel escolhe)"},
	{"build", "COMANDO", "comando de build (vazio = detectar sozinho)"},
	{"start", "COMANDO", "comando de início; use $PORT e $HOST (vazio = detectar sozinho)"},
	{"replicas", "N", "quantidade de instâncias (1 a 16)"},
	{"memory-mb", "MB", "limite de memória por instância (0 = sem limite)"},
	{"cpu-percent", "PCT", "limite de CPU por instância (100 = uma CPU, 0 = sem limite)"},
	{"rate-limit", "REQ_S", "requisições por segundo por IP no Nginx (0 = desligado)"},
	{"auto-deploy", "", "deploy automático a cada push (só GitHub); --no-auto-deploy desliga"},
	{"autoscale", "", "liga o autoscale; --no-autoscale desliga"},
	{"scale-min", "N", "autoscale: mínimo de instâncias"},
	{"scale-max", "N", "autoscale: máximo de instâncias"},
	{"scale-cpu", "PCT", "autoscale: CPU média alvo, em %"},
	{"scale-mem", "PCT", "autoscale: memória média alvo, em % (0 = ignorar)"},
}

func with(base []Flag, extra ...Flag) []Flag { return append(append([]Flag{}, extra...), base...) }

// Catalog lists every command, in the order shown in help and in the panel.
var Catalog = []Cmd{
	{Group: "Geral", Name: "status", Summary: "Resumo do servidor: CPU, memória, disco e o estado de cada app e banco.",
		Examples: []string{"brpanel status"}},
	{Group: "Geral", Name: "events", Summary: "Últimos acontecimentos (deploys, backups, criações).",
		Flags: []Flag{{"limit", "N", "quantos eventos mostrar (padrão 20)"}}, Examples: []string{"brpanel events --limit 5"}},
	{Group: "Geral", Name: "guide", Summary: "Imprime o guia completo para agentes de IA (texto puro). Cole no prompt do agente.",
		Examples: []string{"brpanel guide"}},
	{Group: "Geral", Name: "skills install", Summary: "Grava os skills para agentes de IA (Claude Code e similares) na pasta de skills do agente. Rode de novo para atualizar.",
		Flags:    []Flag{{"dir", "PASTA", "pasta de skills do agente (padrão ~/.claude/skills)"}},
		Examples: []string{"brpanel skills install", "brpanel skills install --dir ~/.config/opencode/skills"}},
	{Group: "Geral", Name: "help", Args: "[COMANDO]", Summary: "Lista os comandos, ou explica um comando em detalhe.",
		Examples: []string{"brpanel help", "brpanel help apps deploy"}},

	{Group: "Aplicações", Name: "apps list", Aliases: []string{"ls", "list"}, Summary: "Lista as aplicações com estado e domínio.",
		Examples: []string{"brpanel apps list", "brpanel apps list --json"}},
	{Group: "Aplicações", Name: "apps show", Aliases: []string{"show"}, Args: "NOME", Min: 1, Summary: "Mostra a configuração e o estado de uma aplicação (sem as variáveis).",
		Examples: []string{"brpanel apps show loja"}},
	{Group: "Aplicações", Name: "apps create", Aliases: []string{"create"}, Min: 0,
		Summary: "Cria uma aplicação a partir de um repositório Git e já faz o primeiro deploy.",
		Flags: with(appFlags,
			Flag{"repo", "URL", "repositório Git (obrigatório), ex.: https://github.com/exemplo/loja"},
			Flag{"name", "NOME", "nome da app (padrão: derivado do repositório)"},
			Flag{"env", "CHAVE=VALOR", "variável de ambiente; repita para várias"},
			Flag{"wait", "", "espera o primeiro deploy terminar e diz se deu certo"},
			Flag{"timeout", "SEGUNDOS", "com --wait: tempo máximo (padrão 600)"}),
		Examples: []string{
			"brpanel apps create --repo https://github.com/exemplo/loja --name loja --domain loja.example.com",
			"brpanel apps create --repo https://github.com/exemplo/mono --name mono-web --root apps/web --watch 'packages/ui/**' --auto-deploy",
			"brpanel apps create --repo https://github.com/exemplo/api --env NODE_ENV=production --env 'DATABASE_URL=${{pg.DATABASE_URL}}'"}},
	{Group: "Aplicações", Name: "apps set", Args: "NOME", Min: 1,
		Summary: "Altera a configuração. Só o que você passar muda. Depois rode `apps deploy` para aplicar.",
		Flags:   with(appFlags, Flag{"repo", "URL", "novo repositório Git"}), Examples: []string{"brpanel apps set loja --replicas 2 --memory-mb 512", "brpanel apps set loja --no-auto-deploy", "brpanel apps set mono-web --root apps/web --watch 'packages/ui/**,!**/*.md'"}},
	{Group: "Aplicações", Name: "apps deploy", Aliases: []string{"deploy", "redeploy"}, Args: "NOME", Min: 1,
		Summary: "Baixa o código, faz o build e reinicia a app. Roda em segundo plano; use --wait para esperar.",
		Flags: []Flag{{"wait", "", "espera o deploy terminar e diz se deu certo"}, {"timeout", "SEGUNDOS", "com --wait: tempo máximo (padrão 600)"},
			{"clean", "", "refaz do zero: apaga o código baixado, as dependências instaladas e o cache do app antes do build (demora mais)"}},
		Examples: []string{"brpanel apps deploy loja --wait", "brpanel apps deploy loja --clean --wait"}},
	{Group: "Aplicações", Name: "apps wait", Args: "NOME", Min: 1, Summary: "Espera o deploy em andamento terminar. Sai com erro se falhar.",
		Flags: []Flag{{"timeout", "SEGUNDOS", "tempo máximo (padrão 600)"}}, Examples: []string{"brpanel apps wait loja"}},
	{Group: "Aplicações", Name: "apps start", Aliases: []string{"start"}, Args: "NOME", Min: 1, Summary: "Liga a aplicação.", Examples: []string{"brpanel apps start loja"}},
	{Group: "Aplicações", Name: "apps stop", Aliases: []string{"stop"}, Args: "NOME", Min: 1, Summary: "Desliga a aplicação (o site sai do ar).", Examples: []string{"brpanel apps stop loja"}},
	{Group: "Aplicações", Name: "apps restart", Aliases: []string{"restart"}, Args: "NOME", Min: 1, Summary: "Reinicia a aplicação (com várias instâncias, uma por vez).", Examples: []string{"brpanel apps restart loja"}},
	{Group: "Aplicações", Name: "apps logs", Aliases: []string{"logs", "log"}, Args: "NOME", Min: 1,
		Summary:  "Mostra o log. kind=service é o log do app rodando; kind=deploy é o log do último deploy (use quando o deploy falhar).",
		Flags:    []Flag{{"kind", "service|deploy", "qual log (padrão service)"}, {"lines", "N", "quantas linhas (10 a 5000, padrão 200)"}},
		Examples: []string{"brpanel apps logs loja", "brpanel apps logs loja --kind deploy --lines 100"}},
	{Group: "Aplicações", Name: "apps delete", Aliases: []string{"delete", "rm"}, Args: "NOME", Min: 1, Danger: true,
		Summary:  "Exclui a aplicação, o serviço, o site e as pastas de armazenamento (volumes) com os backups delas. Não apaga bancos.",
		Examples: []string{"brpanel apps delete loja --yes"}},

	{Group: "Variáveis", Name: "apps env", Args: "NOME", Min: 1, Summary: "Mostra as variáveis de ambiente (cuidado: podem ter segredos).", Examples: []string{"brpanel apps env loja"}},
	{Group: "Variáveis", Name: "apps env-set", Args: "NOME CHAVE=VALOR [CHAVE=VALOR ...]", Min: 2,
		Summary:  "Cria ou troca variáveis. Para apontar para um banco use ${{banco.VARIAVEL}}.",
		Flags:    []Flag{{"deploy", "", "faz um novo deploy logo depois de salvar"}},
		Examples: []string{"brpanel apps env-set loja NODE_ENV=production --deploy", "brpanel apps env-set api 'DATABASE_URL=${{pg.DATABASE_URL}}'"}},
	{Group: "Variáveis", Name: "apps env-unset", Args: "NOME CHAVE [CHAVE ...]", Min: 2, Summary: "Remove variáveis.",
		Flags: []Flag{{"deploy", "", "faz um novo deploy logo depois de salvar"}}, Examples: []string{"brpanel apps env-unset loja DEBUG"}},

	{Group: "Site (Nginx)", Name: "apps domain-add", Args: "NOME DOMINIO", Min: 2,
		Summary:  "Adiciona um domínio extra (com certificado próprio) ou um redirecionamento.",
		Flags:    []Flag{{"redirect-to", "DOMINIO", "em vez de servir o site, redireciona para este domínio"}, {"permanent", "", "redirecionamento permanente (301)"}},
		Examples: []string{"brpanel apps domain-add loja shop.example.com", "brpanel apps domain-add loja www.example.com --redirect-to example.com --permanent"}},
	{Group: "Site (Nginx)", Name: "apps domain-remove", Args: "NOME DOMINIO", Min: 2, Summary: "Remove um domínio extra ou redirecionamento.", Examples: []string{"brpanel apps domain-remove loja shop.example.com"}},
	{Group: "Site (Nginx)", Name: "apps maintenance", Args: "NOME on|off", Min: 2, Summary: "Liga ou desliga a página de manutenção.",
		Flags:    []Flag{{"title", "TEXTO", "título da página"}, {"text", "TEXTO", "mensagem da página"}},
		Examples: []string{"brpanel apps maintenance loja on --title 'Voltamos já' --text 'Estamos atualizando.'", "brpanel apps maintenance loja off"}},
	{Group: "Site (Nginx)", Name: "apps auth-add", Args: "NOME USUARIO", Min: 2, Summary: "Protege o site com usuário e senha (senha lida da entrada padrão, nunca na linha de comando).",
		Flags:    []Flag{{"password-stdin", "", "lê a senha da entrada padrão (obrigatório)"}},
		Examples: []string{"printf '%s' 'senha-de-exemplo' | brpanel apps auth-add loja cliente --password-stdin"}},
	{Group: "Site (Nginx)", Name: "apps auth-remove", Args: "NOME USUARIO", Min: 2, Summary: "Remove um usuário da senha do site.", Examples: []string{"brpanel apps auth-remove loja cliente"}},
	{Group: "Site (Nginx)", Name: "apps limits", Args: "NOME", Min: 1, Summary: "Define o tamanho máximo de upload e o tempo de leitura (0 = padrão).",
		Flags:    []Flag{{"upload-mb", "MB", "tamanho máximo de upload"}, {"read-timeout", "SEGUNDOS", "tempo de leitura do proxy"}},
		Examples: []string{"brpanel apps limits loja --upload-mb 100 --read-timeout 120"}},

	{Group: "Bancos de dados", Name: "dbs list", Aliases: []string{"dbs"}, Summary: "Lista os bancos (PostgreSQL, MySQL/MariaDB, MongoDB, Redis).", Examples: []string{"brpanel dbs list"}},
	{Group: "Bancos de dados", Name: "dbs show", Args: "NOME", Min: 1, Summary: "Mostra o banco e as referências ${{...}} que as apps usam para conectar.", Examples: []string{"brpanel dbs show pg"}},
	{Group: "Bancos de dados", Name: "dbs create", Summary: "Cria um banco (o servidor do banco precisa estar instalado; veja o erro devolvido).",
		Flags:    []Flag{{"kind", "postgres|mysql|mariadb|mongodb|redis", "tipo do banco (obrigatório)"}, {"name", "NOME", "nome (obrigatório)"}},
		Examples: []string{"brpanel dbs create --kind postgres --name pg"}},
	{Group: "Bancos de dados", Name: "dbs start", Args: "NOME", Min: 1, Summary: "Liga o banco.", Examples: []string{"brpanel dbs start pg"}},
	{Group: "Bancos de dados", Name: "dbs stop", Args: "NOME", Min: 1, Summary: "Desliga o banco.", Examples: []string{"brpanel dbs stop pg"}},
	{Group: "Bancos de dados", Name: "dbs restart", Args: "NOME", Min: 1, Summary: "Reinicia o banco.", Examples: []string{"brpanel dbs restart pg"}},
	{Group: "Bancos de dados", Name: "dbs backup", Args: "NOME", Min: 1, Summary: "Faz um backup agora (roda em segundo plano).", Examples: []string{"brpanel dbs backup pg"}},
	{Group: "Bancos de dados", Name: "dbs regenerate", Args: "NOME", Min: 1, Danger: true,
		Summary: "Troca a senha do banco. As apps que o usam precisam de novo deploy.", Examples: []string{"brpanel dbs regenerate pg --yes"}},
	{Group: "Bancos de dados", Name: "dbs delete", Args: "NOME", Min: 1, Danger: true, Summary: "Exclui o banco e todos os dados dele.", Examples: []string{"brpanel dbs delete pg --yes"}},

	{Group: "Templates (um clique)", Name: "templates list", Aliases: []string{"templates"},
		Summary:  "Lista os apps prontos para instalar com um clique, sem Docker (n8n, Uptime Kuma, Gitea…).",
		Examples: []string{"brpanel templates list", "brpanel templates list --json"}},
	{Group: "Templates (um clique)", Name: "templates show", Args: "ID", Min: 1,
		Summary: "Mostra o que o template instala, o que ele precisa e o que fazer depois.", Examples: []string{"brpanel templates show n8n"}},
	{Group: "Templates (um clique)", Name: "templates install", Aliases: []string{"install"}, Args: "ID", Min: 1,
		Summary: "Instala o template como uma aplicação nova (cria, prepara e inicia em segundo plano). Use --wait para esperar.",
		Flags: []Flag{{"name", "NOME", "nome da aplicação (padrão: o id do template)"}, {"domain", "DOMINIO", "domínio com Nginx e SSL (sem ele, abre pela porta do servidor)"},
			{"version", "VERSAO", "versão a instalar (padrão: a do template; veja templates show)"}, {"wait", "", "espera a instalação terminar e diz se deu certo"},
			{"timeout", "SEGUNDOS", "com --wait: tempo máximo (padrão 600)"}},
		Examples: []string{"brpanel templates install n8n --domain flows.example.com --wait", "brpanel install gitea --name git"}},

	{Group: "Acesso remoto", Name: "login", Args: "[SERVIDOR]",
		Summary: "Conecta esta máquina a um painel remoto (IP ou domínio) com um token. Depois, todos os comandos passam a controlar esse servidor.",
		Flags: []Flag{{"token-stdin", "", "lê o token da entrada padrão (para scripts e agentes)"},
			{"allow-http", "", "aceita servidor sem SSL (o token viaja sem criptografia; evite fora de uma rede de confiança)"}},
		Examples: []string{"brpanel login painel.example.com", "printf '%s' 'brp_exemplo' | brpanel login painel.example.com --token-stdin"}},
	{Group: "Acesso remoto", Name: "logout", Summary: "Esquece o servidor remoto salvo; os comandos voltam a usar o socket local.", Examples: []string{"brpanel logout"}},
	{Group: "Acesso remoto", Name: "whoami", Summary: "Mostra qual servidor os comandos estão controlando (local ou remoto).", Examples: []string{"brpanel whoami"}},
	{Group: "Acesso remoto", Name: "tokens list", Aliases: []string{"tokens"}, Summary: "Lista os tokens de acesso remoto (o segredo nunca é mostrado de novo).", Examples: []string{"brpanel tokens list"}},
	{Group: "Acesso remoto", Name: "tokens create", Summary: "Cria um token para o brpanel login de outro computador. O segredo aparece só desta vez.",
		Flags:    []Flag{{"name", "NOME", "nome do token, ex.: notebook (obrigatório)"}},
		Examples: []string{"brpanel tokens create --name notebook"}},
	{Group: "Acesso remoto", Name: "tokens revoke", Aliases: []string{"tokens delete"}, Args: "ID", Min: 1, Danger: true,
		Summary: "Revoga um token: quem o usa perde o acesso na hora. O ID vem de tokens list.", Examples: []string{"brpanel tokens revoke 2 --yes"}},

	{Group: "Cache de builds", Name: "cache status", Aliases: []string{"cache"},
		Summary:  "Mostra quanto espaço o cache de pacotes dos builds (npm, pip, Go, Cargo...) ocupa, por ferramenta. O cache é um só para todas as apps.",
		Examples: []string{"brpanel cache status", "brpanel cache status --json"}},
	{Group: "Cache de builds", Name: "cache prune",
		Summary:  "Apaga do cache os pacotes que nenhum build usa há N dias (padrão 30). Recusa se houver deploy em andamento.",
		Flags:    []Flag{{"days", "DIAS", "sem uso há quantos dias (0 a 3650, padrão 30)"}},
		Examples: []string{"brpanel cache prune", "brpanel cache prune --days 7"}},
	{Group: "Cache de builds", Name: "cache clear", Danger: true,
		Summary:  "Esvazia todo o cache de pacotes. O próximo build de cada app baixa tudo de novo. Recusa se houver deploy em andamento.",
		Examples: []string{"brpanel cache clear --yes"}},
	{Group: "Avançado", Name: "raw", Args: "GET|POST CAMINHO [campo=valor ...]", Min: 2,
		Summary:  "Último recurso: chama uma rota do painel diretamente (POST leva campos de formulário). Prefira os comandos acima.",
		Examples: []string{"brpanel raw GET /api/overview", "brpanel raw POST /apps/loja/restart"}},
}

// byName finds a command by canonical name or alias.
func byName(name string) *Cmd {
	for i := range Catalog {
		c := &Catalog[i]
		if c.Name == name {
			return c
		}
		for _, a := range c.Aliases {
			if a == name {
				return c
			}
		}
	}
	return nil
}

// Names lists every canonical command name.
func Names() []string {
	out := make([]string, len(Catalog))
	for i, c := range Catalog {
		out[i] = c.Name
	}
	return out
}

// Group is a run of commands under one heading.
type Group struct {
	Name string
	Cmds []Cmd
}

// Groups returns the catalog split by group, in order, for the panel page.
func Groups() []Group {
	var out []Group
	for _, c := range Catalog {
		if n := len(out); n == 0 || out[n-1].Name != c.Group {
			out = append(out, Group{Name: c.Group})
		}
		out[len(out)-1].Cmds = append(out[len(out)-1].Cmds, c)
	}
	return out
}

// firstWord maps the words people (and small models) naturally write to the canonical ones.
var firstWord = map[string]string{
	"app": "apps", "application": "apps", "aplicacao": "apps", "aplicacoes": "apps", "aplicação": "apps", "aplicações": "apps",
	"db": "dbs", "database": "dbs", "databases": "dbs", "banco": "dbs", "bancos": "dbs",
	"event": "events", "evento": "events", "eventos": "events",
	"token": "tokens", "entrar": "login", "sair": "logout", "template": "templates", "modelo": "templates", "modelos": "templates",
	"ajuda": "help", "--help": "help", "-h": "help", "-help": "help", "guia": "guide",
}

var verbWord = map[string]string{
	"ls": "list", "listar": "list", "all": "list",
	"info": "show", "get": "show", "ver": "show", "view": "show", "describe": "show",
	"new": "create", "add": "create", "criar": "create",
	"instalar": "install", "revogar": "revoke", "remove": "delete", "rm": "delete", "del": "delete", "excluir": "delete", "destroy": "delete",
	"redeploy": "deploy", "publish": "deploy", "log": "logs",
	"envs": "env", "variables": "env", "vars": "env",
	"domain-rm": "domain-remove", "auth-rm": "auth-remove",
	"edit": "set", "update": "set", "config": "set",
}

// resolve turns the leading words of args into a command. It tolerates plural/singular, synonyms and
// a space instead of a hyphen ("apps env set" = "apps env-set"). rest is what is left for the parser.
func resolve(args []string) (c *Cmd, rest []string) {
	low := make([]string, 0, len(args))
	for _, a := range args {
		low = append(low, strings.ToLower(a))
	}
	if len(low) == 0 {
		return nil, args
	}
	w := []string{low[0]}
	if n, ok := firstWord[w[0]]; ok {
		w[0] = n
	}
	alts := func(i int) []string { // the word as typed, then its canonical synonym
		if i >= len(low) {
			return nil
		}
		a := []string{low[i]}
		if n, ok := verbWord[low[i]]; ok {
			a = append(a, n)
		}
		return a
	}
	try := func(name string, used int) bool {
		if cmd := byName(name); cmd != nil {
			c, rest = cmd, args[used:]
			return true
		}
		return false
	}
	for _, v2 := range alts(1) {
		for _, v3 := range alts(2) {
			if try(w[0]+" "+v2+"-"+v3, 3) {
				return
			}
		}
	}
	for _, v2 := range alts(1) {
		if try(w[0]+" "+v2, 2) {
			return
		}
	}
	if try(w[0], 1) {
		return
	}
	// "dbs" alone is "dbs list"; the bare "apps" too.
	if w[0] == "apps" && try("apps list", 1) {
		return
	}
	return nil, args
}
