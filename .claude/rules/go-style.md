---
paths:
  - "**/*.go"
---

# Estilo Go

- `gofmt` sempre (`make fmt`); `go vet ./...` limpo.
- Todo identificador exportado tem comentário de documentação começando pelo nome; todo pacote tem `// Package x ...`.
- Erros: embrulhe com `%w`, compare com `errors.Is/As`; nunca ignore erro silenciosamente, exceto em limpeza best-effort com `_ =` e um motivo óbvio.
- Mensagens ao usuário e logs do painel em **português (pt-BR)**; identificadores e comentários de código em inglês.
- Sem estado global mutável, exceto variáveis de teste explícitas (ex.: `sysinfo.ProcRoot`). Dependências entram por construtor/campo (`deploy.New`, `server.New`).
- Contexto: operações externas (git, systemctl, nginx, certbot, HTTP) recebem `context.Context` com timeout.
- Prefira a biblioteca padrão. Nova dependência exige justificativa no PR e `go mod tidy`.
- Comentários explicam o *porquê* (restrição, armadilha), não repetem o código.
