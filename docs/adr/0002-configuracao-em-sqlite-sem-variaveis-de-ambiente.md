# ADR 0002 — Configuração no SQLite, sem variáveis de ambiente

- **Status:** aceita
- **Contexto:** a primeira versão lia domínio, e-mail do Let's Encrypt, usuário das aplicações e credenciais do GitHub de variáveis de ambiente. Isso dificulta a instalação e mistura segredos com a unit do serviço.
- **Decisão:** uma página de primeiro início (protegida por token de uso único) grava tudo na tabela `settings` do SQLite; a página *Configurações* altera depois. Só restam as flags `-addr` e `-data`.
- **Consequências:** + instalação guiada e uma só fonte de verdade; + mudanças sem reiniciar. − segredos em texto no banco (arquivo 0600); − quem automatiza a instalação precisa preencher o formulário (não há configuração por arquivo).
