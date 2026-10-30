# Política de segurança

## Como reportar
Envie os detalhes de forma privada ao mantenedor (use o recurso *Report a vulnerability* do GitHub no repositório). Inclua versão/commit, passos de reprodução e impacto. Evite publicar a falha antes de haver correção.

## Escopo
O painel roda como root e executa código de repositórios; consideramos relevantes: execução de comandos por entrada não confiável, bypass de autenticação/CSRF/assinatura do webhook, vazamento de segredos (senha, token do GitHub, chave do App) e escalonamento de privilégios do usuário das aplicações.

O modelo de ameaças e as limitações conhecidas estão em `docs/security.md`.

## Versões
Apenas a `main` recebe correções.
