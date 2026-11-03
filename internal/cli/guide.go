package cli

import (
	"fmt"
	"strings"
)

// Prompt is the short block to paste into any agent's system prompt. It is written for the weakest
// models (a few billion parameters): few rules, imperative, one example per idea.
const Prompt = `Você controla um servidor com o BR Panel pela linha de comando "brpanel". Rode sempre como root: sudo brpanel ...
REGRAS:
1. Nunca invente comandos. Se não souber, rode: brpanel help   ou   brpanel help NOME-DO-COMANDO
2. Um comando por vez. Leia a resposta antes do próximo.
3. A resposta começa com OK: (deu certo) ou ERRO: (falhou). Depois do ERRO vêm linhas DICA: com o que fazer. Siga a DICA.
4. Para saber o que existe: brpanel status   |   brpanel apps list   |   brpanel dbs list
5. Publicar ou atualizar um app: brpanel apps deploy NOME --wait   (espera e diz se deu certo)
6. Se o deploy falhar: brpanel apps logs NOME --kind deploy --lines 80   e corrija a causa.
7. Comandos que apagam exigem --yes no fim. Só use --yes se o usuário pediu para apagar.
8. Senhas nunca vão na linha de comando. Para senha do site use: printf '%s' 'SENHA' | brpanel apps auth-add NOME USUARIO --password-stdin
9. Para ler a saída em JSON, acrescente --json.
10. Guia completo com receitas: brpanel guide
11. Em outro computador (não no servidor), conecte antes: printf '%s' 'TOKEN' | brpanel login ENDEREÇO --token-stdin   (o token é criado na aba CLI do painel). Se aparecer "você ainda não entrou em nenhum servidor", é isso que falta.`

// HelpOverview is `brpanel help`: every command in one line, grouped.
func HelpOverview() string {
	var b strings.Builder
	b.WriteString("BR Panel CLI. Use: brpanel COMANDO [argumentos] [opções]. Rode como root (sudo).\n")
	b.WriteString("Respostas: OK: deu certo | ERRO: falhou (leia as linhas DICA:). Detalhes de um comando: brpanel help COMANDO\n")
	for _, g := range Groups() {
		fmt.Fprintf(&b, "\n%s\n", strings.ToUpper(g.Name))
		for _, c := range g.Cmds {
			u := c.Name
			if c.Args != "" {
				u += " " + c.Args
			}
			fmt.Fprintf(&b, "  %-44s %s\n", u, c.Summary)
		}
	}
	b.WriteString("\nOpções de qualquer comando: --json (saída em JSON), --yes (confirma apagar), --data PASTA\n")
	return b.String()
}

// HelpCommand is `brpanel help COMANDO`.
func HelpCommand(c Cmd) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n  %s\n\nUSO: %s\n", c.Name, c.Summary, c.Usage())
	if len(c.Aliases) > 0 {
		fmt.Fprintf(&b, "TAMBÉM FUNCIONA: brpanel %s\n", strings.Join(c.Aliases, ", brpanel "))
	}
	if len(c.Flags) > 0 {
		b.WriteString("\nOPÇÕES\n")
		for _, f := range c.Flags {
			n := "--" + f.Name
			if f.Value != "" {
				n += " " + f.Value
			}
			fmt.Fprintf(&b, "  %-34s %s\n", n, f.Desc)
		}
	}
	if c.Danger {
		b.WriteString("\nATENÇÃO: apaga dados. Só roda com --yes.\n")
	}
	if len(c.Examples) > 0 {
		b.WriteString("\nEXEMPLOS\n")
		for _, e := range c.Examples {
			b.WriteString("  " + e + "\n")
		}
	}
	return b.String()
}

// Guide is `brpanel guide` and /cli/guide.txt: everything an AI agent needs, as plain text.
func Guide() string {
	var b strings.Builder
	b.WriteString(`GUIA DO BR PANEL PARA AGENTES DE IA
===================================

O QUE É
O BR Panel publica repositórios Git como serviços (systemd) com Nginx e SSL, cria bancos de dados
(PostgreSQL, MySQL/MariaDB, MongoDB, Redis) e mostra CPU e memória. Você o controla com o comando
"brpanel". Cada app tem um NOME (letras minúsculas, números e hífen). Bancos e apps dividem o mesmo
espaço de nomes.

COMO RODAR
  sudo brpanel COMANDO [argumentos] [opções]
No servidor, o comando fala com o painel por um socket local que só o root acessa; não precisa de senha
nem token. Em outro computador, veja a seção abaixo (brpanel login).
Se aparecer "sem permissão", falta o sudo. Se aparecer "não consegui falar com o painel", o painel
está parado: systemctl status brpanel.

CONTROLAR UM SERVIDOR DE OUTRO COMPUTADOR (Linux, macOS ou Windows)
  1. Instalar a CLI no seu computador:
       Linux/macOS:  curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.sh | sh
       Windows:      irm https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.ps1 | iex
  2. Criar um token (uma vez, por computador ou agente). No servidor: sudo brpanel tokens create --name notebook
     (ou na aba CLI do painel). O segredo (começa com brp_) aparece só nessa hora.
  3. Entrar: printf '%s' 'brp_...' | brpanel login painel.example.com --token-stdin
     Use o domínio do painel (com SSL). Com só o IP o token viaja sem criptografia e é preciso --allow-http.
  4. Usar normalmente: brpanel status, brpanel apps list...   brpanel whoami mostra qual servidor está ativo.
  Sair: brpanel logout.  Revogar um token: sudo brpanel tokens revoke ID --yes.
Um token dá o poder total do painel: trate como senha e nunca o mostre ao usuário sem necessidade.

COMO LER A RESPOSTA
  OK: ...      o comando deu certo (código de saída 0)
  ERRO: ...    falhou (código 1 = o painel recusou; código 2 = você escreveu o comando errado)
  DICA: ...    o próximo passo; siga
  USO: ...     a forma correta do comando
Com --json a resposta é JSON. Listas são tabelas de texto.

REGRAS DE OURO
`)
	for _, l := range strings.Split(Prompt, "\n")[1:] {
		b.WriteString(l + "\n")
	}
	b.WriteString(`
O deploy roda em segundo plano. "apps deploy" volta na hora; use --wait (ou "apps wait NOME") para
esperar o resultado. Mudar configuração ou variáveis só vale depois de um novo deploy
(variáveis: use --deploy para fazer junto).

RECEITAS (copie e troque os valores)

0) Instalar um app pronto (n8n, Uptime Kuma, Gitea…), sem Docker:
   brpanel templates list                       (ids disponíveis)
   brpanel templates install n8n --domain flows.example.com --wait
   Sem --domain o app abre pela porta do servidor (brpanel apps show NOME mostra a porta).
   O dado fica numa pasta de armazenamento própria e sobrevive a deploys.
   Atualizar para a versão mais nova: brpanel apps deploy NOME --wait
   Atalho: brpanel install n8n

1) Publicar um site a partir do GitHub, com domínio:
   brpanel apps create --repo https://github.com/exemplo/loja --name loja --domain loja.example.com --wait
   O painel detecta Node, Python, Go, etc., escolhe a porta e configura Nginx e SSL.
   O app recebe PORT e HOST; o comando de início precisa escutar em $PORT.

2) Atualizar um app depois de mudar o código:
   brpanel apps deploy loja --wait

3) Ver por que um deploy falhou:
   brpanel apps logs loja --kind deploy --lines 80
   Ver por que o app caiu depois de subir:
   brpanel apps logs loja --lines 100

4) Definir variáveis e aplicar:
   brpanel apps env-set loja NODE_ENV=production API_KEY=valor --deploy
   brpanel apps env loja

5) Criar um banco e ligar num app:
   brpanel dbs create --kind postgres --name pg
   brpanel dbs show pg                      (mostra as referências, ex.: ${{pg.DATABASE_URL}})
   brpanel apps env-set loja 'DATABASE_URL=${{pg.DATABASE_URL}}' --deploy
   Use aspas simples em volta de ${{...}} para o shell não mexer nelas.
   Se "dbs create" disser que o servidor do banco não está instalado, peça ao usuário para instalá-lo
   pelo painel (Novo banco de dados > Instalar); isso não é um comando da CLI.

6) Mais instâncias e limites:
   brpanel apps set loja --replicas 3 --memory-mb 512 --cpu-percent 100
   brpanel apps set loja --autoscale --scale-min 1 --scale-max 4 --scale-cpu 60
   brpanel apps deploy loja --wait

7) Domínios extras e redirecionamento:
   brpanel apps domain-add loja shop.example.com
   brpanel apps domain-add loja www.example.com --redirect-to example.com --permanent

8) Página de manutenção:
   brpanel apps maintenance loja on --title "Voltamos já" --text "Estamos atualizando."
   brpanel apps maintenance loja off

9) Senha no site (a senha vem da entrada padrão):
   printf '%s' 'senha-de-exemplo' | brpanel apps auth-add loja cliente --password-stdin

10) Backup de um banco e conferir:
   brpanel dbs backup pg
   brpanel events --limit 5

11) Apagar (só se o usuário pediu):
   brpanel apps delete loja --yes

SE DER ERRO
  comando desconhecido        use a DICA "você quis dizer"; ou brpanel help
  opção desconhecida          a DICA lista as opções válidas daquele comando
  não encontrado              o nome está errado: brpanel apps list  /  brpanel dbs list
  Já existe um deploy...      espere: brpanel apps wait NOME
  domínio já usado            outro app usa esse domínio: escolha outro
  O Nginx recusou...          o valor foi rejeitado e nada foi salvo; corrija o valor
  o deploy falhou             brpanel apps logs NOME --kind deploy --lines 80
  sem permissão               rode com sudo
  você ainda não entrou...    em outro computador falta: brpanel login ENDEREÇO --token-stdin
  o servidor recusou o token  token errado ou revogado: crie outro e entre de novo
  certificado SSL...          use o domínio do painel, não o IP
  não consegui falar...       systemctl status brpanel

LIMITES
- A CLI não instala servidores de banco, não muda as configurações do painel nem o GitHub App:
  isso é feito no painel web.
- Variáveis podem ter segredos: não repita o conteúdo delas em respostas ao usuário sem necessidade.
- "raw" chama rotas do painel diretamente; é o último recurso.

REFERÊNCIA DE COMANDOS
`)
	for _, g := range Groups() {
		fmt.Fprintf(&b, "\n[%s]\n", g.Name)
		for _, c := range g.Cmds {
			fmt.Fprintf(&b, "\n%s\n  %s\n  %s\n", c.Name, c.Summary, c.Usage())
			for _, f := range c.Flags {
				n := "--" + f.Name
				if f.Value != "" {
					n += " " + f.Value
				}
				fmt.Fprintf(&b, "    %-30s %s\n", n, f.Desc)
			}
			for _, e := range c.Examples {
				b.WriteString("  ex.: " + e + "\n")
			}
		}
	}
	b.WriteString("\nOpções de qualquer comando: --json, --yes, --data PASTA\n")
	return b.String()
}
