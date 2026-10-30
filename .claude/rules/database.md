---
paths:
  - "internal/store/**"
  - "internal/settings/**"
---

# Banco de dados (SQLite)

- Esquema muda **só** acrescentando um item ao slice `migrations` em `store.go` (versão = posição, guardada em `PRAGMA user_version`). Nunca edite nem reordene itens existentes: bancos em produção já aplicaram.
- Uma migração por mudança, idempotente dentro da transação; teste com banco novo e, quando houver dados, com um banco na versão anterior.
- Uma única conexão (`SetMaxOpenConns(1)`): não segure `rows` abertos enquanto faz outra consulta.
- Novas configurações: campo em `settings.Settings` + entrada em `Fields()` + validação em `settings.Validate` + campo no formulário (`web/templates/settings.html`) + `readSettingsForm`. **Nunca** uma variável de ambiente.
- `UpdateConfig` não altera status de deploy de propósito; use `BeginDeploy`/`FinishDeploy` para transições.
