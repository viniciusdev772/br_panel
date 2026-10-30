---
description: Roda formatação, vet e testes (com -race) e resume o resultado
allowed-tools: Bash(gofmt:*), Bash(go vet:*), Bash(go test:*), Bash(go build:*), Bash(make:*)
---

Execute a verificação completa do projeto e reporte de forma objetiva:

1. `gofmt -l .` — liste arquivos fora do padrão (não deve haver nenhum).
2. `go vet ./...`
3. `go test -race ./...`

Se algo falhar, mostre só o trecho relevante da saída, diagnostique a causa raiz e proponha (ou aplique, se for óbvio e pequeno) a correção. Ao final, diga claramente: tudo verde, ou o que falta.

$ARGUMENTS
