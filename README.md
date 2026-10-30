# BR Panel

Painel web leve (Go) para VPS Linux — uma alternativa ao EasyPanel focada em **serviços nativos do
Linux (systemd)** em vez de Docker.

Informe um repositório Git, branch, comando de build e de início: o painel clona, compila, cria uma
unit `brpanel-<app>.service` e sobe a aplicação. Opcionalmente publica um domínio com Nginx + SSL
automático e faz deploy a cada push via GitHub App. UI em Material Design 2 (MDC Web 14).

## Instalação e primeiro início

Requer Go 1.26+ para compilar (sem CGO; o SQLite é o driver puro-Go `modernc.org/sqlite`) e, no servidor,
`git` e systemd (e opcionalmente `nginx`, `certbot`).

```sh
make build                                   # gera bin/brpanel
sudo install -m 0755 bin/brpanel /usr/local/bin/brpanel
sudo install -m 0644 deployments/systemd/brpanel.service /etc/systemd/system/
sudo systemctl enable --now brpanel
sudo journalctl -u brpanel | grep "PRIMEIRO INÍCIO"   # mostra o link de configuração
```

Abra `http://<IP-do-servidor>:8080/setup`. A tela pede só três coisas:

1. **Código de instalação** — já vem no link do log (também em `/var/lib/brpanel/setup_token`); sem o link, digite-o.
   Impede que outra pessoa configure o painel antes de você; é apagado ao concluir.
2. **Usuário** e **senha** (mín. 12 caracteres).

O usuário do sistema das aplicações (`brapps`) é criado automaticamente. Domínio, SSL e GitHub App ficam
em **Configurações** e só precisam ser preenchidos quando você quiser usá-los. A tela só avisa se faltar
algo indispensável no servidor (root, git, systemd).
Tudo é salvo no banco SQLite `/var/lib/brpanel/brpanel.db` (modo 0600) e pode ser alterado depois em
**Configurações**. Não há variáveis de ambiente: as únicas opções de inicialização são flags.

| Flag | Padrão | Descrição |
|---|---|---|
| `-addr` | `:8080` | Endereço de escuta. Depois de configurar o SSL, use `127.0.0.1:8080` e acesse pelo domínio |
| `-data` | `/var/lib/brpanel` | Banco de dados, logs e arquivos das apps |

Quem já usava a versão anterior: o `apps.json` é importado automaticamente (e renomeado para `.migrated`).

## Escolher o repositório na lista do GitHub App

Com o GitHub App configurado, a tela **Nova aplicação** mostra um campo para buscar entre os repositórios em que o App está instalado (públicos e privados, de todas as instalações). Ao escolher um, o painel preenche a URL e a branch padrão e detecta o projeto. Se o App ainda não está instalado em nenhum repositório, a tela avisa; você também pode digitar a URL na mão. A lista usa um token de instalação com a permissão mínima (*Metadata: read*), usado só para listar.

## Node.js pelo nvm

Projetos Node.js precisam de `node` e `npm` no servidor. O painel instala os dois com o [nvm](https://github.com/nvm-sh/nvm), sem usar os pacotes da distribuição:

- Em **Nova aplicação**, quando a detecção avisa que faltam `node`/`npm`, aparece o botão **Instalar Node.js com nvm**, com o andamento ao vivo; ao terminar, a detecção roda de novo.
- No **deploy**, se um projeto Node.js precisa de `node`/`npm`/`pnpm`/`yarn` e eles não existem, o painel instala sozinho antes de seguir.

O nvm (versão fixa, clonada do GitHub, sem `curl | bash`) fica em `<data>/nvm`. A versão do Node.js é a de **Configurações > Node.js** (padrão **24**; aceita `22`, `20.18.1`, `lts/*`…): evite `lts/*` com dependências antigas, porque o Node 25+ removeu APIs que elas usam. Trocar a versão depois, com o Node.js já instalado, reinstala em segundo plano e religa `node`/`npm`. O corepack é habilitado (`pnpm` e `yarn`). `node`, `npm`, `npx`, `corepack`, `pnpm` e `yarn` são ligados em `/usr/local/bin`, então builds e serviços systemd os encontram. Um `node` que já exista ali (pacote da distribuição) nunca é sobrescrito. Ao terminar, o painel confere que o usuário das aplicações consegue executá-los. Requer `git`, `bash` e `curl` ou `wget`.

## Configuração automática do projeto

Informe só o repositório: o painel dá nome à aplicação, escolhe uma porta livre e descobre como
compilar e iniciar. O botão **Detectar automaticamente** (ou sair do campo do repositório) faz um clone
temporário e mostra o que foi encontrado; se o comando de início ficar vazio, a detecção roda no deploy
e o resultado é salvo na aplicação (editável). Os comandos usam `$PORT` e `$HOST` (127.0.0.1 com domínio,
0.0.0.0 sem domínio).

| Arquivo encontrado | Tipo | Build / início (resumo) |
|---|---|---|
| `package.json` | Node.js | `npm ci`/`pnpm`/`yarn`/`bun` conforme o lockfile + `run build`; início por `start`, Next, Nuxt, Vite ou arquivo principal |
| `go.mod` | Go | `go build -o app .` (ou `./cmd/<x>`) / `./app` |
| `Cargo.toml` | Rust | `cargo build --release` / `./target/release/<nome>` |
| `requirements.txt`, `pyproject.toml` | Python | venv + pip; Django/Flask via gunicorn, FastAPI via uvicorn |
| `Gemfile` | Ruby | `bundle install`; Rails ou Rack |
| `composer.json` | PHP | `composer install`; servidor embutido do PHP |
| `index.html` | Site estático | `python3 -m http.server` |

Um `Procfile` (linha `web:`) define o comando de início, e um `brpanel.json` na raiz do repositório
(`{"build": "...", "start": "...", "port": 8080}`) sobrescreve tudo. Se faltar algum programa no servidor
(node, go, python3…), o painel avisa com a sugestão de instalação antes de tentar compilar.

## Bancos de dados (PostgreSQL, MySQL/MariaDB, MongoDB, Redis)

Em **Criar → Banco de dados** o painel provisiona uma instância **dedicada** por banco, como serviço systemd
(`brpaneldb-<nome>.service`) com diretório de dados, porta e senha próprios, rodando com um usuário sem privilégios
(`brdb`, configurável) e ouvindo só em `127.0.0.1`. Nada de Docker: usa os binários do próprio servidor.

- **Instalação:** se o motor não está instalado, a página de criação mostra o que falta e, em Debian/Ubuntu, instala
  com `apt-get` (`postgresql`, `mariadb-server`, `redis-server`) e desativa a instância padrão da distribuição.
  O MongoDB não está nos repositórios padrão: instale `mongodb-org` + `mongosh` e o painel detecta sozinho.
- **Conectar:** URL privada, host, porta, usuário, banco e senha (mascarada, com copiar/mostrar), linha de comando
  pronta (`psql`, `mongosh`, `redis-cli`, `mariadb`) e **regenerar senha**.
- **Variáveis de referência:** em *Variáveis* da aplicação, `${{postgres.DATABASE_URL}}` é resolvido no deploy
  (`DATABASE_URL`/`MONGO_URL`/`REDIS_URL`, `URL`, `HOST`, `PORT`, `USER`, `PASSWORD`, `DATABASE`). Trocar a senha do banco
  não exige editar a aplicação, só um novo deploy. As referências aparecem como **setas** no canvas.
- **Dados:** navegador somente leitura (tabelas/coleções/chaves, até 50 linhas por item).
- **Backups:** manual ou agendado (diário/semanal), os 14 mais recentes, download e **restauração**
  (`pg_dump -Fc`, `mariadb-dump`, `mongodump --archive`, `redis-cli --rdb`).
- **Acesso externo:** opcional (liga em `0.0.0.0`, ajusta o `pg_hba.conf` no PostgreSQL); libere a porta no firewall.
- Métricas de CPU/RAM, logs do serviço e do provisionamento, iniciar/parar/reiniciar e exclusão.

Validação: PostgreSQL, MariaDB e Redis foram exercitados de ponta a ponta contra servidores reais (criar, conectar,
dados, backup, restauração, senha, acesso externo); MongoDB só tem testes das linhas de comando geradas, pois não
foi possível instalá-lo no ambiente de desenvolvimento.

### Apps com banco ou porta fixos no código

Alguns projetos escrevem `host: "localhost"` (porta 3306) e a própria porta HTTP no código, sem ler variáveis de ambiente. Sem mexer no código:

- **Porta HTTP fixa** (ex.: `const port = 3090`): em **Editar** a aplicação, informe essa porta em **Porta**; o painel a usa no Nginx e no `PORT`.
- **Banco em `localhost:3306`**: como cada banco do painel usa a própria porta (3307, 3308…), crie um repasse do systemd, que persiste no boot e vale para qualquer linguagem. Exemplo para o MariaDB do painel na 3307 (unit `brpaneldb-mysql`):

```ini
# /etc/systemd/system/brpanelfwd-3306.socket
[Socket]
ListenStream=127.0.0.1:3306
ListenStream=[::1]:3306
[Install]
WantedBy=sockets.target

# /etc/systemd/system/brpanelfwd-3306.service
[Unit]
Requires=brpaneldb-mysql.service
After=brpaneldb-mysql.service
[Service]
ExecStart=/usr/lib/systemd/systemd-socket-proxyd 127.0.0.1:3307
PrivateTmp=yes
DynamicUser=yes
```

Depois `systemctl daemon-reload && systemctl enable --now brpanelfwd-3306.socket`. Crie no banco do painel o mesmo banco, usuário e senha que o código usa, restritos a `127.0.0.1`, `localhost` e `::1` (`CREATE USER 'u'@'127.0.0.1' …`). O repasse é manual: se o banco mudar de porta, atualize o `ExecStart`.

## Monitoramento e logs

- **CPU e RAM por aplicação:** a cada 15 s o painel lê o cgroup v2 do serviço
  (`/sys/fs/cgroup/system.slice/brpanel-<app>.service`: `cpu.stat` e `memory.current` sem o cache inativo) e guarda
  24 h no SQLite. A página da aplicação mostra gráficos de 1 h/6 h/24 h, uso atual, tempo no ar e reinícios;
  o dashboard mostra CPU/RAM de cada app e RAM, carga, disco e uptime do servidor, atualizados a cada 5 s.
  CPU é em % de um núcleo (200% = dois núcleos). Requer cgroup v2 (padrão nas distros atuais).
- **Logs:** página dedicada por aplicação com o journal do serviço e o log de deploy: 100–5000 linhas,
  filtro por texto, destaque de erros e avisos, acompanhamento ao vivo e download.

## Domínio com Nginx + SSL automático

Requer `nginx` e `certbot` e o DNS apontando para o servidor. Ao informar um **domínio** na aplicação
(com a **porta**), cada deploy:

1. grava `brpanel-<app>.conf` no diretório de vhosts (proxy reverso para `127.0.0.1:<porta>`, com WebSocket);
2. valida (`nginx -t`) e recarrega o Nginx — configuração inválida é revertida e o deploy falha;
3. com e-mail configurado, emite o certificado via `certbot certonly --webroot` e passa o site para HTTPS.
   A renovação fica com o timer do pacote do certbot; um `--deploy-hook` recarrega o Nginx.

Antes de pedir o certificado, o painel espera o DNS aparecer nos resolvedores públicos (até 2 min) e repete o certbot até 3 vezes. Se mesmo assim a emissão falhar, o site continua em HTTP e o próximo deploy tenta de novo; para o painel, a página **GitHub** mostra o motivo e o botão **Tentar de novo**.
O domínio do painel (Configurações) usa o mesmo mecanismo.

### DNS automático na Cloudflare (opcional)

Se o DNS do domínio está na Cloudflare, o painel pode criar o registro por você:

1. Na Cloudflare, escolha a credencial:
   - **Global API Key** (acesso a todos os domínios da conta): **Meu perfil > Tokens de API > Chaves de API > Global API Key**. No painel, preencha também o e-mail da conta. Ela dá acesso total à conta, então só use se aceitar esse risco.
   - **API Token restrito** (recomendado): **Criar token > modelo "Editar DNS da zona"**, com *Zone:Read* e *DNS:Edit* só nas zonas desejadas. Deixe o e-mail em branco.
2. No painel: **Configurações > Cloudflare**, cole a credencial e salve.
3. A cada deploy de uma app com domínio, antes do SSL, o painel procura a zona (o domínio ou um domínio pai) e cria o registro `A` apontando para o IP do servidor, ou o corrige se estiver diferente. O registro fica sem proxy; se você já o deixou com proxy (nuvem laranja), o proxy é mantido. Nada é apagado.

Se algo falhar (zona fora da conta, token sem permissão, servidor atrás de NAT), o deploy só registra um aviso no log e segue. A credencial fica no SQLite e nunca volta para o navegador.

## GitHub App: deploy automático

### Criação automática (recomendado)

Na página **GitHub** do painel:

1. **Domínio do painel.** O GitHub só entrega pushes a um endereço HTTPS público. Se a Cloudflare estiver configurada, informe um domínio (e o e-mail do Let's Encrypt, se ainda não salvo): o painel cria o registro DNS A, publica o painel com Nginx e emite o SSL. Sem a Cloudflare, defina o domínio em **Configurações**, com o DNS já apontando para o servidor.
2. Abra o painel pelo domínio (`https://<domínio>/github`; será preciso entrar de novo) e clique em **Criar GitHub App**. O painel envia ao GitHub o App já preenchido (webhook de push, *Contents: read-only*); você só confirma lá.
3. O GitHub devolve você ao painel, que guarda sozinho o App ID, a chave privada e o segredo do webhook. Depois, instale o App nos repositórios e marque **Deploy automático** na aplicação.

### Configuração manual

1. Crie um GitHub App (a página **GitHub** do painel mostra a URL do webhook e as permissões):
   webhook em `https://<domínio-do-painel>/webhooks/github`, content type JSON, evento **Push**,
   permissão **Contents: read-only**.
2. Em **Configurações**, salve o segredo do webhook; para repositórios **privados**, salve também o App ID e a
   chave privada (.pem). O painel gera um token de instalação de 1 hora, limitado ao repositório e somente
   leitura, entregue só ao `git` (nunca ao build nem à aplicação, e nunca gravado em `.git/config`).
3. Instale o App no repositório e marque **Deploy automático** na aplicação.

O webhook valida `X-Hub-Signature-256` (HMAC-SHA256, tempo constante). Pushes durante um deploy geram uma
nova execução ao final.

## Segurança

- Senha do admin com **Argon2id** (32 MiB, t=3; acima do mínimo da OWASP); entrada limitada a 128 caracteres.
  Login com tempo equivalente para usuário inexistente, bloqueio por IP após 5 falhas.
- Sessão por cookie `HttpOnly` + `SameSite=Strict` (`Secure` automático sob HTTPS), CSRF em todo POST,
  sessões encerradas ao trocar a senha. Até a configuração inicial, só `/setup` responde e exige o token.
- Banco e diretório de dados só para root (0600/0750). Segredos (chave do GitHub App, segredo do webhook)
  ficam em texto no banco: proteja backups e acesso ao servidor.
- URLs de repositório validadas (https/ssh, sem credenciais); `git`, `systemctl`, `nginx` e `certbot` são
  chamados com argumentos separados, nunca por shell. Domínios, e-mail e caminhos são validados antes de
  entrar em arquivos do Nginx/systemd.
- Comandos de build/início são definidos pelo admin e rodam via `sh -c`: proteja o acesso ao painel e use o
  usuário das aplicações. As units usam `NoNewPrivileges`, `PrivateTmp` e `ProtectSystem=full`.

## Desenvolvimento

```sh
make help     # lista os alvos
make check    # formatação + vet + testes com -race (o mesmo que o CI)
make run      # executa localmente
```

Estrutura (layout padrão Go): `cmd/brpanel` (entrada), `internal/*` (um pacote por responsabilidade), `web/` (templates e
estáticos embutidos), `deployments/` (unit systemd), `docs/` (arquitetura, segurança e ADRs), `scripts/` (capturas de tela),
`.claude/` (regras do Claude Code). Veja [`docs/`](docs/README.md) e [`CONTRIBUTING.md`](CONTRIBUTING.md).
