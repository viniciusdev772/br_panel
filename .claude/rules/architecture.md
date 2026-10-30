# Arquitetura e dependências entre pacotes

Layout Go padrão: `cmd/brpanel` (entrada) → `internal/*` (toda a lógica, não importável de fora) → `web/` (templates e estáticos embutidos).

Direção das dependências (só para a direita/baixo; nunca o contrário):

```
cmd/brpanel → server, store
server      → auth, deploy, detect, githubapp, monitor, nginx, settings, store, sysinfo, systemd, validate, web
deploy      → detect, execx, githubapp, nginx, settings, store, systemd, validate
monitor     → store, sysinfo, systemd
nginx       → execx, settings          store → settings → githubapp, validate
systemd     → sysinfo                  detect → validate
folhas      : auth, execx, githubapp, sysinfo, validate
```

- `server` é a única camada HTTP; não coloque regra de negócio nela. Se um handler precisa de mais que validar → chamar → renderizar, mova a lógica para o pacote de domínio.
- Pacotes de `internal/` nunca importam `server`. Ciclos indicam que algo está no pacote errado.
- Configuração vive no SQLite (`internal/settings` + `store`). **Não crie variáveis de ambiente** nem novas flags além de `-addr`/`-data`.
- Mudança de comportamento observável (rota, campo de formulário, tela) → atualize README e `docs/` no mesmo commit.
