# Contribuindo

Obrigado por querer ajudar! O projeto não usa GitHub Actions: **não existe CI automático**, então a verificação é feita por você, na sua máquina, antes de abrir o PR.

1. Faça um fork e crie um branch a partir de `main`.
2. Leia `docs/architecture.md` e as regras em `.claude/rules/` (valem para pessoas e para o Claude Code).
3. Desenvolva com `make run` (precisa de Go 1.26+).
4. Antes de abrir o PR rode **`make check`** (formatação, `go vet`, testes com `-race`) e cole o resultado na descrição. PR com `make check` falhando não é revisado.
5. Toda mudança de comportamento traz testes (inclua entradas maliciosas quando houver dado externo) e atualiza README/docs/`CHANGELOG.md`.
6. Abra o PR preenchendo o modelo em `.github/PULL_REQUEST_TEMPLATE.md`. Uma mudança por PR, commits curtos em português.

Bugs e ideias: abra uma issue escolhendo um dos formulários em `.github/ISSUE_TEMPLATE/` (bug, app que não sobe, funcionalidade, programa de um clique, documentação). A issue em branco está desligada em `config.yml`, que também leva vulnerabilidades ao relato privado e dúvidas de uso à documentação. Ao mudar um formulário, valide o YAML: o GitHub ignora o formulário inteiro se houver erro de sintaxe.

Padrões: interface e mensagens em português (pt-BR), código e comentários em inglês, bibliotecas padrão sempre que possível, nada de variáveis de ambiente novas (configuração vai para o SQLite).

Vulnerabilidades: não abra issue pública; veja `SECURITY.md`.
