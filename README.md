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

Como serviço: veja `deploy/brpanel.service`.

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
