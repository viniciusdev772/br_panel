# Referência dos comandos do `brpanel`

Todos os comandos. Se uma linha daqui discordar do programa, vale o programa: `brpanel help COMANDO`.
No servidor use `sudo brpanel ...`. De outro computador, depois do `brpanel login`, sem `sudo`.

**Opções de qualquer comando:** `--json` (saída em JSON), `--yes` (confirma o que apaga),
`--data PASTA` (pasta de dados do painel, padrão `/var/lib/brpanel`) e `--local` (usa o socket do
servidor mesmo com login remoto salvo).

**Sinônimos aceitos** (para não travar): `app`/`aplicacao` = `apps`, `db`/`banco` = `dbs`,
`ls`/`list`, `rm`/`remove`/`delete`, `info`/`show`, `redeploy`/`deploy`, `log`/`logs`.
Atalhos: `brpanel ls`, `brpanel deploy NOME`, `brpanel logs NOME`, `brpanel install n8n`.
As palavras dos comandos são em inglês; as mensagens, em português.

Convenção: em `NOME` você coloca o nome da app ou do banco; `--opção VALOR` leva um valor;
`--opção` sozinha é uma chave liga/desliga (`--no-opção` desliga, quando existe).

## Geral

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `status` | Servidor (CPU, memória, disco) e estado de cada app e banco | | `brpanel status` |
| `events` | Últimos acontecimentos (deploys, backups, criações) | `--limit N` (até 30) | `brpanel events --limit 5` |
| `guide` | Guia completo em texto puro, sempre atual | | `brpanel guide` |
| `help` | Lista os comandos ou explica um | | `brpanel help apps deploy` |

## Aplicações

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `apps list` | Lista as apps com estado e domínio | | `brpanel apps list --json` |
| `apps show NOME` | Configuração e estado de uma app (sem as variáveis) | | `brpanel apps show loja` |
| `apps create` | Cria a app a partir de um repositório Git e já faz o 1º deploy | `--repo URL` (obrigatória), `--name`, `--env CHAVE=VALOR` (repetível), `--wait`, `--timeout SEGUNDOS` e as opções de configuração abaixo | `brpanel apps create --repo https://github.com/exemplo/loja --name loja --domain loja.example.com --wait` |
| `apps set NOME` | Muda a configuração (só o que você passar). Vale após `apps deploy` | `--repo URL` e as opções de configuração abaixo | `brpanel apps set loja --replicas 2 --memory-mb 512` |
| `apps deploy NOME` | Baixa o código, faz o build e reinicia | `--wait`, `--timeout SEGUNDOS`, `--clean` (refaz do zero, demora mais) | `brpanel apps deploy loja --wait` |
| `apps wait NOME` | Espera o deploy em andamento; sai com erro se falhar | `--timeout SEGUNDOS` | `brpanel apps wait loja` |
| `apps start NOME` | Liga a app | | `brpanel apps start loja` |
| `apps stop NOME` | Desliga a app (o site sai do ar) | | `brpanel apps stop loja` |
| `apps restart NOME` | Reinicia (com várias instâncias, uma por vez) | | `brpanel apps restart loja` |
| `apps logs NOME` | Mostra o log | `--kind service` (app rodando) ou `--kind deploy` (último deploy), `--lines N` (10 a 5000) | `brpanel apps logs loja --kind deploy --lines 100` |
| `apps delete NOME` | Exclui a app, o serviço, o site **e as pastas de armazenamento com os backups delas**. Não apaga bancos | `--yes` obrigatório | `brpanel apps delete loja --yes` |

**Opções de configuração** (valem em `apps create` e `apps set`): `--branch BRANCH` (padrão `main`),
`--root PASTA` (monorepo: pasta da app no repositório, como `apps/web`), `--watch PADROES` (monorepo:
pastas extras que também publicam), `--domain DOMINIO`, `--port PORTA` (1024 a 65535; vazio = o
painel escolhe), `--build COMANDO`, `--start COMANDO` (use `$PORT` e `$HOST`), `--replicas N` (1 a 16),
`--memory-mb MB` (0 = sem limite, ou 32 em diante), `--cpu-percent PCT` (0 = sem limite, ou 5 em
diante; 100 = uma CPU), `--rate-limit REQ_S` (0 = desligado), `--auto-deploy` / `--no-auto-deploy`,
`--autoscale` / `--no-autoscale`, `--scale-min N`, `--scale-max N`, `--scale-cpu PCT` (10 a 95),
`--scale-mem PCT` (0 = ignorar, ou 10 a 95).

## Variáveis de ambiente

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `apps env NOME` | Mostra as variáveis (**podem ter segredos**) | | `brpanel apps env loja` |
| `apps env-set NOME CHAVE=VALOR ...` | Cria ou troca variáveis; as outras ficam como estão | `--deploy` (deploy logo depois) | `brpanel apps env-set loja NODE_ENV=production --deploy` |
| `apps env-unset NOME CHAVE ...` | Remove variáveis | `--deploy` | `brpanel apps env-unset loja DEBUG` |

## Site (Nginx)

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `apps domain-add NOME DOMINIO` | Domínio extra (certificado próprio) ou redirecionamento | `--redirect-to DOMINIO`, `--permanent` (301) | `brpanel apps domain-add loja www.example.com --redirect-to example.com --permanent` |
| `apps domain-remove NOME DOMINIO` | Remove domínio extra ou redirecionamento | | `brpanel apps domain-remove loja shop.example.com` |
| `apps maintenance NOME on` ou `off` | Liga ou desliga a página de manutenção (503) | `--title TEXTO`, `--text TEXTO` | `brpanel apps maintenance loja on --title 'Voltamos já'` |
| `apps auth-add NOME USUARIO` | Senha no site (a senha vem da entrada padrão) | `--password-stdin` obrigatória | `printf '%s' 'senha-de-exemplo' \| brpanel apps auth-add loja cliente --password-stdin` |
| `apps auth-remove NOME USUARIO` | Remove um usuário da senha do site | | `brpanel apps auth-remove loja cliente` |
| `apps limits NOME` | Upload máximo e tempo de leitura (0 = padrão) | `--upload-mb MB`, `--read-timeout SEGUNDOS` | `brpanel apps limits loja --upload-mb 100 --read-timeout 120` |

## Bancos de dados

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `dbs list` | Lista os bancos | | `brpanel dbs list` |
| `dbs show NOME` | Estado do banco e as referências `${{...}}` para as apps | | `brpanel dbs show pg` |
| `dbs create` | Cria um banco (o servidor do banco precisa estar instalado) | `--kind` (`postgres`, `mysql`, `mariadb`, `mongodb`, `redis`) e `--name NOME`, os dois obrigatórios | `brpanel dbs create --kind postgres --name pg` |
| `dbs start NOME` | Liga o banco | | `brpanel dbs start pg` |
| `dbs stop NOME` | Desliga o banco | | `brpanel dbs stop pg` |
| `dbs restart NOME` | Reinicia o banco | | `brpanel dbs restart pg` |
| `dbs backup NOME` | Backup agora (segundo plano) | | `brpanel dbs backup pg` |
| `dbs regenerate NOME` | Troca a senha do banco; as apps que o usam precisam de novo deploy | `--yes` obrigatório | `brpanel dbs regenerate pg --yes` |
| `dbs delete NOME` | Exclui o banco **e todos os dados** | `--yes` obrigatório | `brpanel dbs delete pg --yes` |

## Templates (apps prontos, sem Docker)

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `templates list` | Lista os templates (n8n, Uptime Kuma, Gitea) | | `brpanel templates list` |
| `templates show ID` | O que instala, o que precisa e o que fazer depois | | `brpanel templates show n8n` |
| `templates install ID` | Instala como app nova | `--name NOME`, `--domain DOMINIO`, `--version VERSAO`, `--wait`, `--timeout SEGUNDOS` | `brpanel templates install n8n --domain flows.example.com --wait` |

## Acesso remoto e tokens

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `login SERVIDOR` | Liga este computador a um painel remoto com um token | `--token-stdin`, `--allow-http` | `printf '%s' 'brp_exemplo' \| brpanel login painel.example.com --token-stdin` |
| `logout` | Esquece o servidor remoto salvo | | `brpanel logout` |
| `whoami` | Mostra qual servidor está sendo controlado | | `brpanel whoami` |
| `tokens list` | Lista os tokens (o segredo nunca volta) | | `brpanel tokens list` |
| `tokens create` | Cria um token; o segredo aparece só desta vez | `--name NOME` obrigatória | `brpanel tokens create --name notebook` |
| `tokens revoke ID` | Revoga um token na hora (ID vem de `tokens list`) | `--yes` obrigatório | `brpanel tokens revoke 2 --yes` |

## Cache de builds

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `cache status` | Espaço do cache de pacotes (npm, pip, Go, Cargo...), por ferramenta | | `brpanel cache status` |
| `cache prune` | Apaga o que ninguém usa há N dias (recusa se há deploy rodando) | `--days DIAS` (0 a 3650, padrão 30) | `brpanel cache prune --days 7` |
| `cache clear` | Esvazia todo o cache; o próximo build baixa tudo de novo | `--yes` obrigatório | `brpanel cache clear --yes` |

## Avançado

| Comando | Para quê | Opções | Exemplo |
|---|---|---|---|
| `raw GET ou POST CAMINHO` | Último recurso: chama uma rota do painel direto | campos `campo=valor` | `brpanel raw GET /api/overview` |
