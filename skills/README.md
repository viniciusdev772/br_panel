# Skills para agentes que usam a CLI do BR Panel

Estes skills ensinam um agente de IA (inclusive um modelo pequeno) a controlar um servidor com o
`brpanel` **sem errar**: publicar apps, ligar bancos, configurar domínio e SSL, instalar apps
prontos e achar a causa quando algo quebra. Cada skill é uma pasta com um `SKILL.md` (Markdown com
cabeçalho `name` e `description`) e, quando útil, `references/` e `scripts/`.

> Não confundir com a configuração do Claude Code **deste** repositório (`.claude/rules/`), que não
> tem skills. Esta pasta é conteúdo do produto, para quem **usa** o painel.

## Os skills

| Skill | Quando usar |
|---|---|
| [`brpanel-cli`](brpanel-cli/SKILL.md) | **Comece aqui.** Regras de ouro, como ler `OK:`/`ERRO:`/`DICA:`, qual skill abrir. Traz a referência de todos os comandos e a tabela de erros. |
| [`brpanel-deploy`](brpanel-deploy/SKILL.md) | Publicar, atualizar, reiniciar, ver logs, deploy que falha, monorepo. |
| [`brpanel-dados`](brpanel-dados/SKILL.md) | Variáveis de ambiente, bancos de dados, `${{pg.DATABASE_URL}}`, backup. |
| [`brpanel-site`](brpanel-site/SKILL.md) | Domínio e SSL, www/redirecionamento, manutenção, senha no site, instâncias, autoscale. |
| [`brpanel-templates`](brpanel-templates/SKILL.md) | n8n, Uptime Kuma, Gitea em um comando. |
| [`brpanel-remoto`](brpanel-remoto/SKILL.md) | Usar a CLI de outro computador: instalar, token, `login`. |
| [`brpanel-diagnostico`](brpanel-diagnostico/SKILL.md) | Roteiro para descobrir por que algo quebrou. |

## Como instalar

**Claude Code** (ou outro agente que leia skills no formato `SKILL.md`): copie as pastas para onde o
agente procura skills, no projeto de quem usa o painel ou na pasta pessoal.

```sh
mkdir -p ~/.claude/skills
cp -r skills/brpanel-* ~/.claude/skills/
```

**OpenCode, Codex e outros** (leem `AGENTS.md`): ponha no `AGENTS.md` do projeto o conteúdo de
`brpanel-cli/SKILL.md` (sem o cabeçalho `---`) e, se o agente aceitar arquivos de apoio, copie a
pasta `skills/` inteira e aponte para ela. As regras do `brpanel-cli` sozinhas já evitam a maioria
dos erros.

**Modelos muito pequenos** (poucos bilhões de parâmetros): use o bloco curto que a própria CLI gera.
Ele fica sempre atual e cabe em qualquer prompt:

```sh
brpanel guide
```

O guia completo também está na página **CLI** do painel e em `/cli/guide.txt`.

## Por que os skills são seguros contra erro

- **Sem comando inventado.** Cada comando citado aqui é conferido por um teste
  (`internal/cli/skills_test.go`): se um exemplo não passar no parser da CLI, ou se um comando ou
  opção do catálogo não estiver em `brpanel-cli/references/comandos.md`, `make check` falha.
- **Exemplos concretos.** Nomes (`loja`, `pg`), domínios (`example.com`) e senhas (`senha-de-exemplo`)
  fictícios, que o agente troca pelos reais.
- **Uma ação, uma conferência.** Todo skill termina mandando conferir (`apps show`, `apps logs`).
- **Perigo sinalizado.** O que apaga dados exige `--yes` e a confirmação da pessoa, e o texto diz
  exatamente o que é apagado.

## Ao mudar a CLI

Comando, opção ou mensagem nova em `internal/cli/catalog.go`? Atualize aqui no mesmo commit:

1. a linha do comando em `brpanel-cli/references/comandos.md` (o teste cobra);
2. o skill da tarefa e `brpanel-cli/references/erros.md`, se a mensagem de erro mudou;
3. rode `make check`.

## Regras para escrever ou editar skills

- Cabeçalho: `name` igual ao nome da pasta; `description` diz **quando usar** (até 1024 caracteres).
- Frases curtas, no imperativo, uma ideia por passo. Tabelas "sintoma → ação".
- Todo bloco ` ```sh ` contém só comandos que funcionam como estão (exemplos fictícios, sem
  placeholders como `<nome>`), porque o teste os executa no parser.
- Nada de dados reais: use `example.com`, `192.0.2.10`, `senha-de-exemplo`, `brp_exemplo`.
- Links relativos precisam apontar para arquivos que existem (o teste confere).
