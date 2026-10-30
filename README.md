# BR Panel

Painel web leve (Go, só biblioteca padrão) para VPS Linux — uma alternativa ao EasyPanel
focada em **serviços nativos do Linux (systemd)** em vez de Docker.

Informe um repositório Git, branch, comando de build e de início: o painel clona, compila,
cria uma unit `brpanel-<app>.service` e sobe a aplicação. UI em Material Design 2 (MDC Web 14).

## Executar

```sh
go build -o brpanel .
sudo PANEL_PASSWORD='uma-senha-forte' ./brpanel
```

| Variável | Padrão | Descrição |
|---|---|---|
| `PANEL_ADDR` | `127.0.0.1:8080` | Endereço de escuta (use proxy reverso com TLS ou túnel SSH) |
| `PANEL_DATA` | `/var/lib/brpanel` | Dados, logs e variáveis das apps |
| `PANEL_UNIT_DIR` | `/etc/systemd/system` | Onde as units são gravadas |
| `PANEL_PASSWORD` | gerada | Senha do admin (se ausente, é gerada em `$PANEL_DATA/admin_password`) |
| `PANEL_APP_USER` | — | Usuário sem privilégios que clona, compila e executa as apps (via `runuser`) |
| `PANEL_SECURE_COOKIE` | — | `1` para cookie `Secure` (quando atrás de HTTPS) |
| `PANEL_DOMAIN` | — | Publica o painel neste domínio com Nginx + SSL |
| `PANEL_ACME_EMAIL` | — | E-mail do Let's Encrypt; habilita a emissão automática de SSL |
| `PANEL_NGINX_DIR` | `/etc/nginx/conf.d` | Onde os vhosts são gravados |
| `PANEL_ACME_ROOT` | `/var/www/brpanel-acme` | Webroot do desafio ACME |
| `PANEL_CERT_DIR` | `/etc/letsencrypt/live` | Diretório dos certificados |
| `PANEL_GITHUB_WEBHOOK_SECRET` | — | Habilita `POST /webhooks/github` |
| `PANEL_GITHUB_APP_ID` / `PANEL_GITHUB_KEY_FILE` | — | Credenciais do GitHub App (clone de repositórios privados) |

Como serviço: veja `deploy/brpanel.service`.

### Domínio com Nginx + SSL automático

Requer `nginx` e `certbot` instalados no servidor (ex.: `apt install nginx certbot`) e o DNS do domínio
apontando para o servidor. Ao informar um **domínio** na aplicação (com a **porta** em que ela escuta), cada deploy:

1. grava `brpanel-<app>.conf` em `PANEL_NGINX_DIR` (proxy reverso para `127.0.0.1:<porta>`, com suporte a WebSocket);
2. valida (`nginx -t`) e recarrega o Nginx — se a configuração for inválida, ela é revertida e o deploy falha;
3. se `PANEL_ACME_EMAIL` estiver definido, emite o certificado Let's Encrypt via `certbot certonly --webroot`
   e passa o site para HTTPS (porta 80 só serve o desafio ACME e redireciona). A renovação fica a cargo do
   timer do pacote do certbot; o `--deploy-hook` recarrega o Nginx após cada renovação.

Se a emissão falhar (ex.: DNS ainda não propagou), o site continua em HTTP e o próximo deploy tenta de novo.
`PANEL_DOMAIN` publica o próprio painel com Nginx + SSL (defina também `PANEL_SECURE_COOKIE=1`).

### GitHub App: deploy automático

1. Crie um GitHub App (veja a página **GitHub** no painel, que mostra a URL do webhook e as permissões):
   webhook em `https://<PANEL_DOMAIN>/webhooks/github`, content type JSON, evento **Push**,
   permissão **Contents: read-only**.
2. Exporte `PANEL_GITHUB_WEBHOOK_SECRET` (o mesmo segredo do App).
3. Para repositórios **privados**, exporte também `PANEL_GITHUB_APP_ID` e `PANEL_GITHUB_KEY_FILE` (.pem):
   o painel gera um token de instalação de 1 hora, limitado ao repositório e somente leitura, entregue só ao `git`
   (nunca ao build nem à aplicação, e nunca gravado em `.git/config`).
4. Instale o App no repositório e marque **Deploy automático** na aplicação.

O webhook valida `X-Hub-Signature-256` (HMAC-SHA256, comparação em tempo constante). Um push na branch
configurada dispara o deploy; pushes durante um deploy geram uma nova execução ao final.

## Segurança

- Sessão por cookie `HttpOnly` + `SameSite=Strict`, token CSRF em todo POST, bloqueio por IP após 5 falhas.
- URLs de repositório validadas (https/ssh apenas, sem credenciais, sem `-` inicial); `git` e `systemctl`
  são chamados com argumentos separados, nunca por shell. Repositórios privados: use chave de deploy SSH.
- Comandos de build/início são definidos pelo admin e rodam via `sh -c`: proteja o acesso ao painel e
  prefira `PANEL_APP_USER`. As units usam `NoNewPrivileges`, `PrivateTmp` e `ProtectSystem=full`.

## Testes

```sh
go test ./...
```
