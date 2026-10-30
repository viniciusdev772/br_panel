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

## Windows
- O painel **só compila para Linux** (systemd, cgroups, `syscall`). No Windows, desenvolva no **WSL2** (recomendado) ou verifique a compilação com `GOOS=linux go build ./... && GOOS=linux go vet ./...`; os testes precisam rodar no Linux/WSL.
- `make` e `scripts/screenshots/run.sh` exigem WSL ou Git Bash. Sem `make`, use os comandos Go equivalentes: `gofmt -l cmd internal web`, `go vet ./...`, `go test -race ./...`, `go build -o bin/brpanel ./cmd/brpanel`.
- `.gitattributes` força final de linha LF nos scripts.

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
- Mudança de esquema: nova migração append-only (`.claude/rules/database.md`).
- Nova linguagem na detecção: novo `detectXxx` em `internal/detect`, com testes, entrada em `requireHints` e linha na tabela do README.

## Claude Code
`.claude/rules/` guarda as regras do projeto (algumas carregam só ao tocar em arquivos de certos caminhos). Não há agentes, skills, comandos nem hooks. Preferências pessoais: `.claude/settings.local.json` e `CLAUDE.local.md` (ignorados pelo Git).
