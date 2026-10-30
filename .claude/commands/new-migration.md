---
description: Cria uma migração de banco de dados (append-only) com teste
argument-hint: <o que muda no esquema>
allowed-tools: Read, Edit, Bash(go test:*), Bash(gofmt:*)
---

Crie uma nova migração SQLite para: **$ARGUMENTS**

Siga `.claude/rules/database.md`:

1. Leia `internal/store/store.go` e acrescente **um novo item no fim** do slice `migrations` (nunca edite os anteriores).
2. Se houver novas colunas/tabelas usadas pela aplicação, atualize as consultas e structs correspondentes (`scanApp`, `appCols`, `Create`, `UpdateConfig`, …).
3. Escreva um teste em `internal/store/store_test.go` que abre um banco novo e verifica o novo esquema; se a migração transforma dados, teste também a partir de um banco na versão anterior.
4. Rode `go test ./internal/store/...` e `go vet ./...`.
5. Registre a mudança em `CHANGELOG.md`.
