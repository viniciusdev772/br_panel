# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). Ainda não há versões numeradas.

## [Não lançado]

### Alterado
- Primeiro início simplificado (o log imprime um link `/setup?code=…` que já traz o código): pede apenas código de instalação, usuário e senha; domínio, SSL e GitHub passaram para Configurações e o usuário das aplicações é criado automaticamente.

### Adicionado
- Painel web em Go para clonar repositórios Git e subir aplicações como serviços systemd, com interface Material Design 2.
- Domínio com Nginx e SSL automático (Let's Encrypt via certbot, renovação pelo timer do pacote).
- GitHub App: deploy automático por push (webhook assinado) e clone de repositórios privados com token de instalação restrito.
- Página de primeiro início com token de instalação; configurações em SQLite e tela de Configurações (sem variáveis de ambiente).
- Monitoramento de CPU e RAM por aplicação (cgroup v2, 24 h) e resumo do servidor no dashboard.
- Página de logs (journal e deploy) com filtro, acompanhamento e download.
- Detecção automática de Node.js, Go, Rust, Python, Ruby, PHP e sites estáticos; suporte a `Procfile` e `brpanel.json`.
- Regras do Claude Code (`.claude/rules/`), Makefile, CI, documentação e ADRs.

- Bancos de dados (PostgreSQL, MySQL/MariaDB, MongoDB, Redis) como instâncias dedicadas: criação com um clique, instalação via apt, conexão, variáveis de referência `${{db.VAR}}`, navegador de dados, backups/restauração, acesso externo opcional, métricas e logs.
- Interface inspirada em canvas de arquitetura: cards de recursos com setas entre aplicações e bancos, painel de atividade, abas por recurso, tema escuro/claro.

### Alterado
- Código reorganizado no layout padrão Go (`cmd/`, `internal/`, `web/`, `deployments/`).

### Corrigido
- Conteúdo das páginas ficava sob a barra superior; layout mobile do dashboard.
- Binário de 12 MB commitado por engano removido do repositório (permanece no histórico antigo).
