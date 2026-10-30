# Contribuindo

1. Faça um fork/branch a partir de `main`.
2. Leia `docs/architecture.md` e as regras em `.claude/rules/` (valem para pessoas e para o Claude Code).
3. Desenvolva com `make run`; antes de abrir o PR rode **`make check`** (formatação, `go vet`, testes com `-race`).
4. Toda mudança de comportamento traz testes (inclua entradas maliciosas quando houver dado externo) e atualiza README/docs/`CHANGELOG.md`.
5. Abra o PR preenchendo o modelo em `.github/PULL_REQUEST_TEMPLATE.md`.

Padrões: interface e mensagens em português (pt-BR), código e comentários em inglês, bibliotecas padrão sempre que possível, nada de variáveis de ambiente novas (configuração vai para o SQLite).

Vulnerabilidades: não abra issue pública; veja `SECURITY.md`.
