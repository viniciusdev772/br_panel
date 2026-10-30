# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). Ainda não há versões numeradas.

## [Não lançado]

### Corrigido
- "Detectar automaticamente" e o primeiro deploy de qualquer aplicação falhavam com `chown: exit status 1` quando o usuário das aplicações estava configurado (o padrão do primeiro início): o `chown` rodava como esse usuário, sem privilégio. Agora roda como o painel, e o erro mostra a mensagem do comando. A pasta `tmp` (clones de detecção) passou de 0750 para 0711 para o usuário das aplicações conseguir entrar na subpasta dele.

### Alterado
- Emissão do SSL mais confiável: antes de chamar o certbot o painel espera o DNS do domínio aparecer nos resolvedores públicos (até 2 min) e repete o certbot até 3 vezes, em vez de falhar na primeira consulta com `NXDOMAIN` logo após criar o registro. Vale para o painel e para as aplicações.
- A página GitHub mostra o motivo real quando o SSL do painel não é emitido e tem o botão **Tentar de novo**, que repete a publicação (antes só recarregava a página). Enquanto publica, ela atualiza sozinha.
- Integração com a Cloudflare (opcional): com um API Token (ou a Global API Key e o e-mail da conta) em Configurações, o deploy cria ou corrige o registro DNS A do domínio da aplicação apontando para o servidor, antes de emitir o SSL.
- Primeiro início simplificado (o log imprime o link pronto, com o IP do servidor e o código: `/setup?code=…`): pede apenas código de instalação, usuário e senha; domínio, SSL e GitHub passaram para Configurações e o usuário das aplicações é criado automaticamente.

### Adicionado
- Nova aplicação: campo de busca com a lista de repositórios do GitHub App (todas as instalações, públicos e privados). Escolher um preenche repositório e branch padrão e dispara a detecção do projeto.
- Criação do GitHub App em um clique (fluxo de manifest do GitHub): a página **GitHub** pede um domínio para o painel (com a Cloudflare configurada, cria o registro DNS A, publica o painel e emite o SSL), envia o App já preenchido ao GitHub e, na volta, guarda App ID, chave privada e segredo do webhook sozinha.
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
