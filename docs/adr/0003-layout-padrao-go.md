# ADR 0003 — Layout padrão de projeto Go

- **Status:** aceita
- **Contexto:** o código nasceu em um único `package main` na raiz (~4 mil linhas), com tudo acoplado ao tipo `Server`.
- **Decisão:** adotar `cmd/` + `internal/` + `web/` + `deployments/` + `docs/` + `scripts/`, com um pacote por responsabilidade e dependências em uma só direção (ver `docs/architecture.md`). `web/embed.go` embute templates e estáticos. A configuração de desenvolvimento com Claude Code fica em `.claude/`.
- **Consequências:** + fronteiras claras, testes por pacote, `internal/` impede uso externo; + CI e Makefile padronizados. − mais arquivos e identificadores exportados; − o pacote `server` ainda concentra os handlers.
