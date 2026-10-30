# Fluxo de Git

- Branch padrão: `main`. Commits pequenos, mensagem curta em português no imperativo/descritiva ("Adiciona…", "Corrige…"), corpo explicando o porquê.
- **Nunca commite binários, bancos, chaves ou diretórios de dados** (`.gitignore` cobre `brpanel`, `*.db`, `*.pem`, `data/`). Um binário de 12 MB já entrou no histórico por um `go build` sem `-o`: compile sempre com `-o` ou use `make build`.
- Antes de commitar: `make check` (fmt + vet + test).
- Sem `git push --force`, sem reescrever histórico compartilhado, sem pular hooks.
- Atualize `CHANGELOG.md` (seção *Não lançado*) em mudanças visíveis ao usuário.
