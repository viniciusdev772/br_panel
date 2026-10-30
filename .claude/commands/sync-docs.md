---
description: Alinha README, CLAUDE.md, docs/ e CHANGELOG com o código atual
allowed-tools: Read, Edit, Grep, Glob, Bash(git diff:*), Bash(git log:*)
---

Revise `git diff main...HEAD` (ou os últimos commits se estiver em `main`) e atualize a documentação para refletir o código:

- `README.md`: instalação, flags, funcionalidades, tabelas de detecção.
- `CLAUDE.md` e `docs/architecture.md`: pacotes, fluxos e rotas que mudaram.
- `.claude/rules/*`: regras que ficaram desatualizadas (ex.: grafo de dependências).
- `CHANGELOG.md`: entradas em *Não lançado*.

Não invente funcionalidades: documente só o que existe no código. Liste ao final o que foi alterado e o que ficou incerto.

$ARGUMENTS
