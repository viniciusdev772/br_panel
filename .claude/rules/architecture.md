# Arquitetura e dependências entre pacotes

Layout Go padrão: `cmd/brpanel` (entrada) → `internal/*` (toda a lógica, não importável de fora) → `web/` (templates e estáticos embutidos).

Direção das dependências (só para a direita/baixo; nunca o contrário):

```
cmd/brpanel → server, store, cli      cmd/brpanel-cli → cli (só a CLI; compila em Windows/macOS)
server      → ai, auth, autoscale, buildcache, cli, monorepo, templates, cloudflare, nodeenv, dbengine, deploy, detect, githubapp, monitor, nginx, settings, store, sysinfo, systemd, validate, volume, web
deploy      → templates, buildcache, cloudflare, nodeenv, dbengine, detect, execx, githubapp, nginx, settings, store, sysinfo, systemd, validate, volume
dbengine    → execx, settings, store, systemd, validate
monitor     → store, sysinfo, systemd
autoscale   → store
nginx       → execx, settings          store → settings → ai, cloudflare, githubapp, validate
ai          → cli (só o catálogo)
systemd     → sysinfo                  detect → validate
volume      → biblioteca padrão
folhas      : auth, buildcache, cli (+ skills), monorepo, templates, cloudflare, nodeenv, execx, githubapp, sysinfo, validate, volume
```

- `server` é a única camada HTTP; não coloque regra de negócio nela. Se um handler precisa de mais que validar → chamar → renderizar, mova a lógica para o pacote de domínio.
- Pacotes de `internal/` nunca importam `server`. Ciclos indicam que algo está no pacote errado.
- `ai` (assistente) só usa a biblioteca padrão e `cli.Catalog`: as ferramentas do modelo são os comandos da CLI, executados por `server` dentro do processo pelas mesmas rotas. Comando novo na CLI = decidir em `ai/tools.go` se é livre, exige confirmação ou fica retido (o teste falha se faltar). Quem altera o servidor nunca roda sem a aprovação do usuário.
- Configuração vive no SQLite (`internal/settings` + `store`). **Não crie variáveis de ambiente** nem novas flags além de `-addr`/`-data`.
- `cli` só usa a biblioteca padrão e conversa com o painel por HTTP sobre o socket: nunca importe `store`/`deploy` nela. Comando novo = uma entrada em `cli.Catalog` + um handler em `cli/commands.go`; o teste do catálogo falha se faltar um dos dois ou se o exemplo não passar no parser.
- Mudança de comportamento observável (rota, campo de formulário, tela) → atualize README e `docs/` no mesmo commit.
