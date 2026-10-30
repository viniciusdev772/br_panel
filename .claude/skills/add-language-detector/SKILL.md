---
name: add-language-detector
description: Use when asked to make the panel auto-detect and configure a new language or framework (build/start commands) from a repository's files. Covers detect.go, tests, README and requirement hints.
---

# Adicionar um detector de linguagem/framework

A detecção vive em `internal/detect/detect.go` e devolve uma `Detection` (tipo, build, start, porta, programas exigidos, notas). Os comandos usam `$PORT` e `$HOST`.

## Passo a passo

1. **Escolha o sinal** (arquivo na raiz): ex. `pom.xml`, `mix.exs`, `deno.json`. Acrescente um `case` no `switch` de `Detect`, respeitando a ordem de precedência (arquivos mais específicos primeiro).
2. **Escreva `detectXxx(dir string) *Detection`** seguindo os existentes:
   - `Kind` legível ("Elixir (Phoenix)"), `Port` padrão do ecossistema, `Requires` = executáveis necessários (`mix`, `elixir`).
   - `Build`: instalação + compilação reprodutível (prefira lockfiles); `Start`: faça o servidor escutar em `$HOST:$PORT`.
   - **Nunca** coloque texto vindo do repositório num comando sem validar: use `safeRel` para caminhos e regex estritas para nomes (veja `detectNode`, `detectRust`).
   - Use `Notes` para avisos úteis (migrações, variáveis obrigatórias).
3. **Dicas de instalação**: acrescente cada novo executável em `requireHints` (`detect.go`), ex. `"mix": "apt install elixir"`.
4. **Testes** em `internal/detect/detect_test.go`: um caso por variação (com/sem lockfile, com/sem script) e um caso **malicioso** (nome de módulo/arquivo com `;`, `..`, espaços) que não pode virar comando. Use o helper `project(t, files)`.
5. **Documentação**: linha na tabela "Configuração automática do projeto" do `README.md` e entrada no `CHANGELOG.md`.
6. Rode `go test ./internal/detect/... && go vet ./...`.

`Procfile` (linha `web:`) e `brpanel.json` já sobrescrevem qualquer detector — não reimplemente isso.
