---
paths:
  - "**/*_test.go"
---

# Testes

- Rode `go test ./...` (e `-race` antes de abrir PR). Um teste: `go test -run Nome ./internal/pacote`.
- Os testes não podem exigir root, systemd, nginx, certbot nem rede. Simule o mundo externo:
  - API do GitHub → `httptest.Server` (veja `githubapp`)
  - cgroup e `/proc` → diretório temporário + `sysinfo.ProcRoot`
  - banco → `store.Open(filepath.Join(t.TempDir(), "t.db"))`
  - HTTP do painel → `httptest` sobre `Server.routes()` (veja `internal/server/server_test.go`)
  - detecção de projeto → monte arquivos em `t.TempDir()`
- Tabelas de casos para validadores e detectores; inclua entradas **maliciosas** (injeção em nome, repositório, `main` do package.json, domínio).
- Teste o comportamento, não a implementação; um bug corrigido ganha um teste que falhava antes.
- Testes de deploy que disparam `git clone` precisam esperar o fim do deploy (`waitIdle`) antes do `TempDir` ser removido.
