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
go build -o brpanel .
sudo install -m 0755 brpanel /usr/local/bin/brpanel
sudo install -m 0644 deploy/brpanel.service /etc/systemd/system/
sudo systemctl enable --now brpanel
sudo journalctl -u brpanel | grep "PRIMEIRO INÍCIO"   # mostra o token de instalação
```

Abra `http://<IP-do-servidor>:8080/setup` e siga a página de configuração inicial:

1. **Token de instalação** — impresso no log na primeira execução (e em `/var/lib/brpanel/setup_token`).
   Impede que outra pessoa configure o painel antes de você; é apagado ao concluir.
2. **Conta de administrador** — usuário e senha (mín. 12 caracteres).
3. **Domínio e SSL do painel** (opcional) — domínio + e-mail do Let's Encrypt.
4. **Usuário das aplicações** (recomendado) — criado se não existir; as apps rodam sem root.
5. **GitHub App** (opcional) — App ID, chave privada e segredo do webhook.

A página também mostra uma verificação do servidor (root, git, systemd, nginx, certbot…).
Tudo é salvo no banco SQLite `/var/lib/brpanel/brpanel.db` (modo 0600) e pode ser alterado depois em
**Configurações**. Não há variáveis de ambiente: as únicas opções de inicialização são flags.

| Flag | Padrão | Descrição |
|---|---|---|
| `-addr` | `:8080` | Endereço de escuta. Depois de configurar o SSL, use `127.0.0.1:8080` e acesse pelo domínio |
| `-data` | `/var/lib/brpanel` | Banco de dados, logs e arquivos das apps |

Quem já usava a versão anterior: o `apps.json` é importado automaticamente (e renomeado para `.migrated`).

## Domínio com Nginx + SSL automático

Requer `nginx` e `certbot` e o DNS apontando para o servidor. Ao informar um **domínio** na aplicação
(com a **porta**), cada deploy:

1. grava `brpanel-<app>.conf` no diretório de vhosts (proxy reverso para `127.0.0.1:<porta>`, com WebSocket);
2. valida (`nginx -t`) e recarrega o Nginx — configuração inválida é revertida e o deploy falha;
3. com e-mail configurado, emite o certificado via `certbot certonly --webroot` e passa o site para HTTPS.
   A renovação fica com o timer do pacote do certbot; um `--deploy-hook` recarrega o Nginx.

Se a emissão falhar (ex.: DNS ainda não propagou), o site continua em HTTP e o próximo deploy tenta de novo.
O domínio do painel (Configurações) usa o mesmo mecanismo.

## GitHub App: deploy automático

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

## Testes

```sh
go test ./...
```
