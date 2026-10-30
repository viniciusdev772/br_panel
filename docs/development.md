# Desenvolvimento

## Requisitos
- Go 1.26+ (definido pelo driver SQLite `modernc.org/sqlite`; sem CGO)
- Para as capturas de tela: Node com `playwright` (Chromium), `python3`, `git`, rede

## Comandos (`make help`)

| Alvo | O que faz |
|---|---|
| `make build` | Compila para `bin/brpanel` (sempre com `-o`) |
| `make run` | Compila e executa (`DATA=./data ADDR=127.0.0.1:8080`) |
| `make check` | Formatação + vet + testes com `-race` (o mesmo que o CI) |
| `make test` / `make race` | Testes |
| `make cover` | Cobertura dos pacotes de `internal/` |
| `make vuln` | `govulncheck` |
| `make screenshots` | Capturas de todas as telas |

Um teste isolado: `go test -run TestDetectNode ./internal/detect`.

## Rodando localmente
```sh
make run          # o primeiro início imprime o token de instalação
```
Abra `http://127.0.0.1:8080/setup`. Sem root, o painel funciona até o deploy chamar `systemctl`/`nginx`; para desenvolver a interface isso basta.

## Testes
Não exigem root, systemd, nginx, certbot nem rede (veja `.claude/rules/testing.md`). Há testes ponta a ponta do servidor em `internal/server/server_test.go` (setup, login, CSRF, aplicações, webhook).

## Convenções
- Layout, dependências entre pacotes e estilo: `.claude/rules/`.
- Interface em português (pt-BR); código e comentários em inglês.
- Mudança de esquema: nova migração append-only (`/new-migration` no Claude Code).
- Nova linguagem na detecção: skill `add-language-detector`.

## Claude Code
`.claude/` contém regras por caminho, comandos (`/check`, `/new-migration`, `/screenshots`, `/sync-docs`), a skill `add-language-detector`, os agentes `security-reviewer` e `go-test-writer` (só rodam se você os chamar) e hooks (gofmt automático, bloqueio de edição de `go.sum`/dados, verificação de build ao concluir). Preferências pessoais: `.claude/settings.local.json` e `CLAUDE.local.md` (ignorados pelo Git).
