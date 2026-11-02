# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). Versões numeradas seguem o [SemVer](https://semver.org/lang/pt-BR/); veja `.claude/rules/releases.md`.

## [Não lançado]

### Adicionado
- Instâncias por aplicação (1 a 16): cada uma é uma unit systemd com a própria porta (`PORT + k`); o Nginx balanceia entre elas (`least_conn`, conexões persistentes, nova tentativa em 502/503/504 e erros de conexão) e o deploy e o reinício trocam uma instância por vez, sem tirar o site do ar. As portas das instâncias ficam reservadas.
- Limites por instância de memória (`MemoryMax`, com `MemoryHigh` em 90%) e CPU (`CPUQuota`), `LimitNOFILE=65536` nas apps, limite de requisições por visitante (`limit_req`, erro 429) e compressão gzip nos vhosts das aplicações. CPU e memória do monitor somam as instâncias; logs mostram todas.

### Corrigido
- Atualizar ou reiniciar o painel desconectava o administrador, porque as sessões viviam só na memória. Agora ficam no SQLite (com o hash do id, nunca o cookie) e continuam valendo depois do reinício; trocar a senha e sair continuam encerrando as sessões.

### Alterado
- A tela de criar e editar aplicação agora é feita em quatro etapas (Código, Execução, Endereço, Ajustes), com textos mais simples e os campos técnicos (build, comando de início, porta) recolhidos em "Ajustar à mão". Sem JavaScript, todas as etapas aparecem juntas.

## [0.1.0] - 2026-11-02

### Adicionado
- Script de instalação `scripts/install.sh` (uma linha com `curl | sudo sh`; instala e atualiza) e workflow de release que publica os binários linux amd64/arm64 com soma SHA-256 a cada tag `v*`.
- Instalação automática do MongoDB: adiciona o repositório oficial e instala a última versão principal disponível para a distribuição (hoje 9.0), com a chave PGP verificada pelo apt. Recusa CPUs sem AVX e distribuições sem repositório oficial.
- Aba **Dados** do banco permite criar: tabela (PostgreSQL/MySQL, colunas por tipo), coleção (MongoDB, com documento JSON opcional) ou chave de texto (Redis). Itens existentes não são sobrescritos.

### Alterado
- Configurações: os caminhos do sistema (Nginx, ACME, certificados, units) e o usuário dos bancos ficam recolhidos em "Avançado"; os padrões já servem para Ubuntu/Debian.

### Corrigido
- Aplicações com comando de início `VAR=valor comando` (ex.: `NODE_ENV=production npm start`, o que a detecção gera) entravam em loop de reinício com `exec: NODE_ENV=production: not found`. A unit usa `exec env VAR=valor comando` nesse caso e não usa `exec` em comandos compostos (`a && b`), onde o `b` nunca rodava.
- O build das aplicações (`npm ci`, `pip`, `cargo`…) falhava com `EACCES: mkdir '/home/brapps'`: o `runuser` trocava o `HOME` pela pasta pessoal do usuário das aplicações, que não existe. Agora usa `runuser -m`, e o build recebe o `HOME`, `PORT` e `HOST` definidos pelo deploy.
- "Detectar automaticamente" e o primeiro deploy de qualquer aplicação falhavam com `chown: exit status 1` quando o usuário das aplicações estava configurado (o padrão do primeiro início): o `chown` rodava como esse usuário, sem privilégio. Agora roda como o painel, e o erro mostra a mensagem do comando. A pasta `tmp` (clones de detecção) passou de 0750 para 0711 para o usuário das aplicações conseguir entrar na subpasta dele.

### Alterado
- A versão do Node.js instalada pelo nvm é configurável (Configurações > Node.js) e o padrão passou de `lts/*` para 24: o Node 25+ removeu APIs (como `buffer.SlowBuffer`) que dependências antigas, como o `jsonwebtoken`, ainda usam. Salvar outra versão com o Node.js já instalado troca o `node`/`npm` do servidor em segundo plano.
- Emissão do SSL mais confiável: antes de chamar o certbot o painel espera o DNS do domínio aparecer nos resolvedores públicos (até 2 min) e repete o certbot até 3 vezes, em vez de falhar na primeira consulta com `NXDOMAIN` logo após criar o registro. Vale para o painel e para as aplicações.
- A página GitHub mostra o motivo real quando o SSL do painel não é emitido e tem o botão **Tentar de novo**, que repete a publicação (antes só recarregava a página). Enquanto publica, ela atualiza sozinha.
- Integração com a Cloudflare (opcional): com um API Token (ou a Global API Key e o e-mail da conta) em Configurações, o deploy cria ou corrige o registro DNS A do domínio da aplicação apontando para o servidor, antes de emitir o SSL.
- Primeiro início simplificado (o log imprime o link pronto, com o IP do servidor e o código: `/setup?code=…`): pede apenas código de instalação, usuário e senha; domínio, SSL e GitHub passaram para Configurações e o usuário das aplicações é criado automaticamente.

### Adicionado
- Repasse da porta padrão por banco (**Banco de dados > Configurações > Ativar repasse**): para projetos que fixam `localhost:3306` (ou 5432, 27017, 6379) no código, o painel ouve em `127.0.0.1:<porta padrão>` e repassa para a porta do banco, com um socket do systemd e `systemd-socket-proxyd`. Persiste no boot, liga o banco sob demanda e é removido junto com o banco.
- Node.js pelo nvm: botão **Instalar Node.js com nvm** na detecção da nova aplicação (com andamento ao vivo) e instalação automática no deploy quando um projeto Node.js precisa de `node`/`npm`/`pnpm`/`yarn` e eles faltam. O nvm (versão fixa) vive em `<data>/nvm` e os executáveis são ligados em `/usr/local/bin`.
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
