# Segurança

## Modelo de ameaças
- O painel roda como root e **executa código de repositórios Git** (build e início). Quem controla um repositório cadastrado controla o que roda como o usuário das aplicações. Use um repositório em que você confia e o usuário sem privilégios (Configurações).
- Quem acessa o painel como admin tem execução de código no servidor (comandos de build/início são definidos por ele).
- A internet vê: `/setup` (só antes de configurar, com token), `/login`, `/static/*` e `/webhooks/github` (com HMAC).

## Controles
| Risco | Controle | Onde |
|---|---|---|
| Tomada do painel no primeiro acesso | Token de instalação de uso único | `server/setup.go` |
| Senha fraca/roubada | Argon2id (32 MiB, t=3), mínimo 12 caracteres, bloqueio por IP após 5 falhas, tempo equivalente para usuário inexistente | `settings`, `auth`, `server` |
| CSRF | Token por sessão em todo POST; cookie `HttpOnly` + `SameSite=Strict` (+`Secure` sob HTTPS) | `auth`, `server.protected` |
| Injeção em comando/unit/Nginx | Validadores estritos; argv separado; `Escape` de `ExecStart`; `nginx -t` com rollback | `validate`, `systemd`, `nginx` |
| Detecção maliciosa (`package.json`) | Caminhos só passam por `safeRel`; nomes por regex | `detect` |
| Webhook forjado | HMAC-SHA256 em tempo constante antes de qualquer efeito; limite de 5 MiB | `githubapp`, `server/webhook.go` |
| Vazamento do token do GitHub | Token de 1 h, só leitura, restrito ao repositório; entregue só ao `git` via `GIT_CONFIG_*`; nunca em argv/log/ambiente do build | `githubapp.AuthEnv` |
| Segredos de bancos vazando em `ps`/logs | Senhas só por variável de ambiente ou arquivo 0600 temporário (apagado); nunca em argv; config com senha 0600 | `dbengine` |
| Injeção via nomes de tabela/coleção/chave | Identificadores validados por regex e citados; chaves Redis vão como argumento (sem shell) | `dbengine` |
| Download/restauração de arquivo arbitrário | Nome de backup validado por regex e resolvido dentro de `databases/<nome>/backups` | `dbengine.ValidBackupFile` |
| Banco exposto na internet | Só `127.0.0.1` por padrão; acesso externo é opt-in explícito (e ajusta `pg_hba`) | `dbengine`, UI |
| IP forjado | `X-Real-IP`/`X-Forwarded-Proto` só de loopback | `auth` |
| Dados em repouso | Banco e env files 0600, diretório 0750 | `store`, `deploy` |
| XSS | CSP restritiva (sem inline), templates com escape automático | `server.securityHeaders` |

## Limitações conhecidas
- Todos os bancos rodam com o mesmo usuário de sistema (`brdb`): isolamento por porta/credencial, não por usuário do SO.
- Senhas dos bancos ficam em texto no SQLite (necessárias para as referências `${{db.VAR}}`).
- Segredos (chave `.pem`, segredo do webhook) ficam **em texto** no SQLite; proteja o disco e os backups.
- O deploy não é atômico nem sem queda de serviço: o build roda no diretório do serviço em execução.
- Não há proteção contra repetição de webhooks assinados (apenas dispara outro deploy).
- Repositórios privados via SSH exigem uma chave de deploy gerenciada manualmente.

Para reportar uma vulnerabilidade, veja `SECURITY.md`.
