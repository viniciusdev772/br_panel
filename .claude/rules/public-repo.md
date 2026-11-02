# Repositório público

O repositório é **público** desde 02/11/2026: tudo que entra em commit, issue, release ou nota fica visível e indexável, mesmo se apagado depois.

- Nunca coloque no repositório: senhas, tokens, chaves, `.pem`, bancos, IPs ou domínios de servidores reais, nomes de clientes, e-mails que não sejam o de autoria dos commits. Exemplos em código, testes e docs usam valores fictícios (`example.com`, `192.0.2.10`, `senha-de-exemplo`).
- Dados de acesso ao servidor do dono ficam fora do Git (memória local do Claude, notas do Maestri, gerenciador de senhas), nunca em regra, README ou script.
- Antes de publicar algo novo que veio de fora (log, saída de comando, captura de tela, backup), leia-o inteiro: logs do painel podem conter domínios, e-mails e links de instalação com código.
- Se um segredo vazar num commit, **troque o segredo primeiro** (ele já está comprometido) e só depois pense em limpar o histórico; reescrever histórico compartilhado exige autorização do dono.
- Ainda não há arquivo `LICENSE`: sem ele ninguém tem permissão legal de reutilizar o código. Não escolha uma licença por conta própria; a decisão é do dono.
- Segurança: relatos de vulnerabilidade seguem o `SECURITY.md`, nunca uma issue pública.
