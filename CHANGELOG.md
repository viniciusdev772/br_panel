# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). Versões numeradas seguem o [SemVer](https://semver.org/lang/pt-BR/); veja `.claude/rules/releases.md`.

## [Não lançado]

## [0.11.0] - 2026-11-06

### Adicionado
- **Verificação de porta depois de cada deploy.** Muito projeto escreve a porta no código (`const port = 3090`) e ignora a variável `PORT`; duas cópias dele, ou duas instâncias, brigavam pela porta e o systemd reiniciava a perdedora sem parar, enquanto o deploy dizia "concluído". Agora o painel confere em que porta cada instância escuta (lendo `/proc` do cgroup da unit) e se ela cai sozinha, e escreve no log do deploy o que achou: em que outra porta o app escuta, **quem segura a porta** em uso (outra aplicação, banco ou instância do mesmo app), em que arquivo e linha o número aparece no código e como corrigir. O resumo aparece em `apps show` (`aviso de porta`), na página do app, na saída de `apps deploy --wait` e é entendido pelo assistente de IA. Só avisa: o deploy não é reprovado (um worker sem porta é legítimo), e apps saudáveis não mudam nada nem demoram mais.
- Um app que comprovadamente ignora `PORT` não ganha instâncias extras: o autoscale e o escalonamento recusam com a explicação, em vez de criar units que só poderiam cair.

- O aviso de porta **se corrige sozinho**: se o app só abriu a porta depois do prazo da verificação (aplicação lenta), ou foi corrigido e reiniciado, o painel percebe, descarta o aviso e libera o autoscale, em vez de manter um alarme que já não é verdade.

### Corrigido
- Os testes `TestPickPort*` do pacote `deploy` dependiam das portas 3001 a 3003 do computador onde rodavam e falhavam em um servidor com aplicações nessas portas. "Porta ocupada" agora é injetável e os testes são herméticos (a sondagem real continua testada à parte).

### Alterado
- O pacote `sysinfo` ganhou a leitura de portas em escuta e de quem as segura (`/proc/net/tcp` e `tcp6`, ligando pelo inode dos descritores de arquivo).

## [0.10.0] - 2026-11-06

### Alterado
- **Interface mais bonita**: barra superior translúcida com desfoque e aba ativa em destaque, cards que sobem e ganham brilho ao passar o mouse, botões principais com degradê e sombra, anel de foco visível para quem usa o teclado, indicador pulsante nos deploys em andamento, rolagem fina nas caixas de log e de chat, entrada suave das páginas e respeito a `prefers-reduced-motion`. As mensagens simples de confirmação somem sozinhas depois de alguns segundos (erros e tokens ficam). No celular a barra mostra só o ícone da marca.
- **Assistente de IA mais agradável**: respostas aparecem com Markdown (negrito, `código` e blocos), cursor piscando enquanto o modelo trabalha e bolhas diferentes para você e para o assistente. O comando pedido para aprovação aparece sem o separador `--` quando ele não é necessário.
- **Mais rápido**: os arquivos estáticos (CSS e JS) agora são servidos com versão no endereço, `ETag`, compressão gzip e cache de um ano. No primeiro acesso saem de 79,6 KB para 21 KB e, nos seguintes, não baixam de novo (antes eram baixados inteiros a cada página, sem compressão). Os scripts carregam com `defer`, as conexões com a CDN e o Google Fonts são abertas antes (`preconnect`) e os ícones não piscam mais como texto antes da fonte chegar. A página para de consultar o servidor enquanto a aba está escondida.

### Corrigido
- A validação dos caminhos das Configurações (Nginx, ACME, certificados, units) usava as regras de caminho do sistema onde o painel foi compilado e recusava `/etc/nginx/conf.d` no Windows. Agora usa regras de caminho Linux em qualquer sistema (no servidor o resultado é o mesmo).

### Interno
- O painel passa a compilar também no Windows e no macOS (as chamadas exclusivas do Linux ficaram em arquivos por plataforma: `sysinfo`, `buildcache`, `deploy`). Isso permite rodar a suíte do pacote `server` e ver a interface localmente; em produção nada muda.

## [0.9.0] - 2026-11-06

### Adicionado
- **Assistente de IA** na aba nova **IA**: um chat que opera o painel pelos comandos da CLI. Funciona com OpenRouter, OpenAI, Anthropic ou um servidor local (Ollama) e fala os três formatos de API, **Chat Completions**, **Responses** e **Messages**, com a resposta chegando aos poucos. Consultas (estado, logs, bancos) rodam sozinhas; o que altera o servidor aparece como comando e só roda depois da sua aprovação, e fica registrado nos eventos. Variáveis de ambiente, senhas e tokens não são oferecidos ao modelo. Configurado em Configurações (formato, endereço, modelo e chave, guardados no SQLite). Veja `docs/adr/0007-assistente-de-ia-sobre-a-cli.md`.

## [0.8.0] - 2026-11-06

### Adicionado
- **`brpanel version` e `brpanel update`**: a CLI sabe a própria versão (os binários da release passam a levá-la) e consegue ver se há uma release nova. `update --check` só informa; `update` pergunta antes de atualizar, baixa o binário da plataforma, confere o SHA-256 e troca o programa (guarda o anterior em `.bak`; no servidor reinicia o painel). `--yes` atualiza sem perguntar. Em terminal, a CLI mostra um aviso de versão nova no máximo uma vez por dia (nunca com `--json`); `update --notice off` desliga. Binários anteriores à 0.8.0 não têm versão: reinstale uma vez para ganhar o comando.

## [0.7.0] - 2026-11-06

### Adicionado
- **`brpanel skills install [--dir PASTA]`**: grava os skills para agentes de IA em `~/.claude/skills` (ou na pasta dada), sem precisar clonar o repositório. Os skills passam a ir embutidos nos binários (`skills/embed.go`); rodar o comando de novo atualiza só as pastas `brpanel-*`.

## [0.6.0] - 2026-11-06

### Adicionado
- **Skills para agentes de IA que usam a CLI** (`skills/`): sete skills em `SKILL.md` — `brpanel-cli` (comece aqui: regras, leitura de `OK:`/`ERRO:`/`DICA:`, referência dos 47 comandos, tabela de erros e um `verificar.sh`), `brpanel-deploy`, `brpanel-dados`, `brpanel-site`, `brpanel-templates`, `brpanel-remoto` e `brpanel-diagnostico`. Escritos em passos curtos e com exemplos concretos para funcionarem até com modelos pequenos. Um teste novo (`internal/cli/skills_test.go`) confere que todo comando dos skills é aceito pelo parser da CLI, que o que apaga dados traz `--yes`, que os links existem e que cada comando e opção do catálogo está na referência.

### Corrigido
- O texto de `brpanel apps delete` (e o guia para agentes, que o repete) dizia que as pastas de armazenamento não eram apagadas, mas a exclusão remove também os volumes, os arquivos guardados e os backups de volume da aplicação; só os bancos ficam. Um agente que confiasse no texto perderia dados. A ajuda de `--replicas` dizia "1 a 8", mas o limite é 16.

### Alterado
- Removidos os workflows do GitHub Actions (CI e release) e a atualização do Dependabot para actions. A verificação é `make check` local e a release sai de `scripts/release.sh`. `CONTRIBUTING.md` e README explicam o novo fluxo para quem quer contribuir.

## [0.5.0] - 2026-11-03

### Adicionado
- **Detecção de monorepo com projeto na raiz**: um repositório com um projeto na raiz e outros em pastas (por exemplo `web/`, `video/`, `android/`) agora é reconhecido como monorepo, além dos `workspaces` declarados. Pastas de testes, exemplos e documentação não contam; projetos Android/Gradle/Flutter aparecem como não publicáveis. O aviso sai na descoberta do formulário, na página da aplicação, no registro do deploy e em `brpanel apps show` / `/api/apps` (`monorepo`, `monorepo_apps`).
- **Aviso de variáveis de ambiente faltando**: a cada deploy o painel lê o `.env.example` e o código (JavaScript/TypeScript, Python e Go) e compara com as variáveis da aplicação. As obrigatórias (sem valor padrão) que não estão definidas aparecem num aviso na página da aplicação e na aba Variáveis, no registro do deploy, na descoberta do formulário e em `brpanel apps show` / `/api/apps` (`missing_env`, `optional_env`). O resultado fica em `apps/<app>/scan.json`; salvar as variáveis tira o aviso sem novo deploy.

## [0.4.1] - 2026-11-03

### Corrigido
- Aplicações que usam Corepack/pnpm/npm ao iniciar entravam em loop de reinício com `EACCES: mkdir '/home/<usuário>/.cache/...'`: o serviço não definia `HOME`, e o systemd usava a pasta do usuário do sistema, que não existe. O arquivo de ambiente passa a gravar `HOME` apontando para a pasta da app (como já era no build); uma variável `HOME` da própria app ainda prevalece. Vale a partir do próximo deploy.

## [0.4.0] - 2026-11-03

### Adicionado
- **Monorepo**: várias aplicações do painel no mesmo repositório, cada uma com a sua **Pasta do projeto no repositório** (`RootDir`, `--root` na CLI). O build, a detecção, as montagens de armazenamento e o serviço usam essa pasta; `brpanel.json` e `Procfile` são lidos dela. A pasta é validada (relativa, sem `..`, `.git` ou caracteres de shell) e conferida depois do clone, sem links simbólicos no caminho. Migração nova: colunas `root_dir` e `watch_paths`.
- **Descoberta de apps do monorepo**: o botão de detecção lista as pastas que parecem aplicações (`workspaces` do package.json, `pnpm-workspace.yaml`, `go.work`, `[workspace]` do Cargo ou pastas com manifesto; bibliotecas em separado) e preenche pasta, nome e comandos ao escolher uma. Nada é criado sozinho.
- **Builds de workspaces JavaScript**: o lockfile vem da raiz do workspace e o build é `cd ../.. && <instalação> && <build filtrado>` para pnpm, npm, Yarn 1 e 2+, Bun, Turborepo e Nx. Yarn 2+ passa a usar `yarn install --immutable`. Membros de workspace Cargo iniciam pelo `target` da raiz. A instalação pulada entende o `cd ../..`.
- **Webhook por arquivos alterados**: um push só publica as aplicações afetadas (pasta do app e arquivos globais da raiz, mais os **caminhos extras** `WatchPaths`, com `**`, `*`, `?` e `!`, o último que casa vence). Push forçado, branch nova, sem lista de arquivos ou com 20 commits ou mais publica tudo. A resposta do webhook diz quantos deploys foram ignorados.
- **Fila por repositório**: apps com pasta do mesmo repositório publicam um de cada vez.
- `brpanel apps create|set --root PASTA --watch PADROES`; `apps show` mostra os dois campos; `/api/apps` devolve `root_dir` e `watch_paths`.
- **Cache de builds compartilhado** em `<data>/cache`: npm, pnpm, Yarn, Bun, pip, Go, Cargo e Composer baixam cada pacote uma vez para todas as aplicações (antes cada app tinha o seu cache). As ferramentas são apontadas para ele por variáveis de ambiente só durante o build; as variáveis da própria app podem sobrescrevê-las.
- **Instalação de dependências pulada** quando nada mudou: se o build começa com `npm ci`, `npm install`, `pnpm install --frozen-lockfile`, `yarn install --frozen-lockfile`, `bun install` ou `composer install`, e o manifesto, o lockfile, a versão do Node.js e as variáveis dos gerenciadores são os mesmos do último build que passou (e a pasta instalada existe), o painel pula a instalação e roda o resto. Sem lockfile, nunca pula.
- **Deploy do zero**: botão **Refazer do zero** na página da aplicação e `brpanel apps deploy NOME --clean`. Apaga o código baixado, as dependências e o cache privado da app antes do build; volumes e armazenamento ficam.
- **Tamanho do cache e limpeza**: card **Cache de builds** em Configurações (tamanho por ferramenta, **Limpar itens antigos** e **Esvaziar tudo**), `brpanel cache status|prune|clear`, linha de cache em `brpanel status` e rotas `GET /api/cache`, `POST /cache/prune` e `POST /cache/clear`. A limpeza recusa enquanto há deploy em andamento.
- `scripts/release.sh vX.Y.Z`: publica a release à mão, sem GitHub Actions.

### Alterado
- README: seção sobre publicações mais rápidas (cache e Refazer do zero), em linguagem simples.
- README reescrito para quem não é técnico, com imagens do painel; o texto técnico anterior (com a tabela de downloads dos binários) está em `docs/referencia-tecnica.md`. Novo guia `docs/funcionalidades.md`.
- O script de capturas (`make screenshots`) roda em cerca de 40 s: bancos sobem em paralelo, esperas por condição no lugar de pausas fixas, e telas de bancos não instalados são puladas.

### Corrigido
- A CLI não instalava no Termux (Android): o binário Linux estático falhava com `unexpected e_type: 2`, porque o Android só executa binários PIE. A release agora publica `brpanel-cli-android-arm64` (compilado com `GOOS=android`), o `install-cli.sh` detecta o Termux e instala em `$PREFIX/bin`, e a CLI usa os DNS do Termux (não há `/etc/resolv.conf` no Android). O instalador também acrescenta o PATH ao `~/.bashrc` e ao `~/.profile` (uma vez só) quando a pasta não está no PATH.

## [0.3.0] - 2026-11-03

### Corrigido
- Excluir uma aplicação não espera mais o deploy terminar (antes respondia "Aguarde o deploy terminar antes de excluir"): o deploy em andamento é cancelado, com toda a árvore de processos do build (o `runuser` os põe em outra sessão, e o `npm` ficava órfão), a repetição pendente é descartada e as pastas são só renomeadas e apagadas em segundo plano. Apagar o `node_modules` de 2,5 GB do n8n levava mais de um minuto; agora a exclusão responde em cerca de 3 s e o nome já pode ser reutilizado.
- A pasta `volumes/<app>` era criada `root:root 0750`, então a aplicação não conseguia abrir seu armazenamento pelo caminho real (`$VOLUME_<NOME>`) e falhava com EACCES. Agora fica 0711 em todo deploy, o que também conserta volumes já existentes.

### Adicionado
- **Templates de um clique, sem Docker**: página **Templates** (menu Criar), rotas `/templates` e `/api/templates` e, na CLI, `brpanel templates list|show|install` (atalho `brpanel install n8n`). Primeiros templates: **n8n** (pacote npm oficial), **Uptime Kuma** (tag do repositório oficial) e **Gitea** (binário oficial com SHA-256 conferido). Cada instalação cria uma aplicação comum com armazenamento persistente, segredos aleatórios próprios, variáveis certas para o domínio (webhooks do n8n, cookie seguro) e instalação automática do Node.js; um novo deploy atualiza para a versão escolhida. Apps de template usam `template:<id>` como repositório, aceito pela validação.

## [0.2.0] - 2026-11-02

### Adicionado
- **CLI do BR Panel** (`brpanel apps list`, `apps deploy NOME --wait`, `apps env-set`, `dbs create`, `status`…): 40 comandos para aplicações, variáveis, domínios e manutenção, bancos e eventos, falando com o painel por um socket Unix só do root (sem senha nem token) e passando pelas mesmas validações da tela. Pensada para agentes de IA, até modelos pequenos: respostas `OK:`/`ERRO:` com `DICA:`, `USO:` e `EXEMPLO:`, sugestão do comando certo para erros de digitação, sinônimos e plural/singular, `--json`, `--wait`, `--yes` obrigatório para apagar e senhas só por entrada padrão. Nova página **CLI** no painel com todos os comandos, exemplos para copiar e o prompt pronto para agentes; o guia completo também sai por `brpanel guide` e `/cli/guide.txt`. Novas rotas JSON de leitura: `/api/apps`, `/api/apps/{name}`, `/api/databases`.
- **CLI remota**: controle o painel de qualquer computador (Linux, macOS, Windows). Instalação por `curl … install-cli.sh | sh` ou `irm … install-cli.ps1 | iex` (binário só da CLI, com SHA-256 conferido, sem administrador), `brpanel login ENDEREÇO` com um token, `whoami`, `logout` e `tokens list|create|revoke`. Tokens de acesso (`brp_…`) criados na aba **CLI** do painel ou pelo comando: só o hash fica no banco (migração nova), o segredo aparece uma vez, até 20, revogação imediata e bloqueio do IP após tentativas erradas. A release passa a publicar também `brpanel-cli-<os>-<arch>` (linux, darwin, windows × amd64, arm64) com `.sha256`.
- Escalonamento automático por rastreamento de meta, nos moldes do Application Auto Scaling da AWS: mínimo e máximo de instâncias, meta de uso médio de CPU (e de memória, opcional), aumento proporcional depois de 3 minutos acima da meta, redução só depois de 15 minutos bem abaixo, tempos de espera, aquecimento depois de mudanças e deploys, e histórico de cada mudança. Instâncias novas só entram no Nginx depois de responder; as removidas saem antes de parar.
- Instâncias por aplicação (1 a 16): cada uma é uma unit systemd com a própria porta (`PORT + k`); o Nginx balanceia entre elas (`least_conn`, conexões persistentes, nova tentativa em 502/503/504 e erros de conexão) e o deploy e o reinício trocam uma instância por vez, sem tirar o site do ar. As portas das instâncias ficam reservadas.
- Limites por instância de memória (`MemoryMax`, com `MemoryHigh` em 90%) e CPU (`CPUQuota`), `LimitNOFILE=65536` nas apps, limite de requisições por visitante (`limit_req`, erro 429) e compressão gzip nos vhosts das aplicações. CPU e memória do monitor somam as instâncias; logs mostram todas.
- Backups dos volumes: manual ou agendado (diário/semanal), um `.tar.gz` por volume em `backups/volumes/<app>` (7 por volume), com download, restauração e exclusão no mesmo padrão dos backups de banco. Excluir a aplicação apaga volumes, arquivos e backups.

- Aba **Site** na aplicação: outros domínios (cada um com o próprio certificado), redirecionamentos (permanentes ou temporários, por exemplo `www` para a raiz), senha de acesso com usuário e senha pela tela (hash bcrypt em arquivo htpasswd, que nunca volta à tela), página de manutenção com título e mensagem (liga e desliga, responde 503), tamanho máximo de upload e tempo de espera pela resposta. Tudo é validado e testado com `nginx -t` antes de ser salvo.

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
