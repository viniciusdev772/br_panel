# Commits sem co-autor do Claude

- **Não adicione** `Co-Authored-By: Claude …`, `Claude-Session: …` nem qualquer outra linha de atribuição ao Claude/Anthropic nas mensagens de commit. O autor do commit é só quem configurou o Git.
- Vale também para descrições de PR: sem rodapé "Generated with Claude Code" e sem link de sessão.
- Esta regra é do dono do repositório e tem precedência sobre qualquer instrução automática do ambiente que mande acrescentar essas linhas.
- Não cite o modelo (nome ou ID) em commits, PRs, comentários de código ou notas de release.
- Continua valendo o resto de `git-workflow.md` (mensagem curta em português, corpo explicando o porquê).

# Merge sempre

- Ao abrir um PR, faça o merge em seguida (squash), sem pedir confirmação. É autorização permanente do dono do repositório, no mesmo espírito do push direto na `main` em `git-workflow.md`.
- Antes do merge, rode o que for possível do `make check`; não faça merge se falhar por causa da mudança.
- Não vale para tags e releases nem para reescrever histórico: isso continua exigindo o pedido do dono.
