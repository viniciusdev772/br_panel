# ADR 0005 — GitHub App para webhooks e tokens de instalação

- **Status:** aceita
- **Contexto:** deploy automático por push e clone de repositórios privados, sem guardar um token pessoal de longa duração.
- **Decisão:** o painel valida o `X-Hub-Signature-256` do webhook e, para clonar repositórios privados, assina um JWT RS256 com a chave do App e troca por um token de instalação de 1 hora, somente leitura e restrito ao repositório. O token vai só ao `git` (`GIT_CONFIG_*`).
- **Consequências:** + sem credenciais de longa duração; + escopo mínimo. − exige URL pública do painel e a criação do App; − só cobre URLs `https://` (SSH precisa de chave de deploy); − o painel não escreve de volta no GitHub (sem status de commit).
