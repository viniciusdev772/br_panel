---
name: brpanel-templates
description: Instalar apps prontos no BR Panel com um comando, sem Docker - n8n (automação), Uptime Kuma (monitoramento), Gitea (repositórios Git) - com `brpanel templates install`. Use quando a pessoa pedir um desses programas ou "um app pronto".
---

# Templates: apps prontos em um comando

Pré-requisito: leu o skill `brpanel-cli`.

Um template é um app normal já configurado: o painel baixa o programa oficial, cria o serviço,
a porta, os segredos próprios e (com domínio) o Nginx e o SSL. Os dados ficam numa pasta de
armazenamento que **sobrevive a deploys**.

## Passo a passo

1. Ver o que existe (use os `ID` da primeira coluna; **não invente**):

   ```sh
   brpanel templates list
   ```

2. Ler o que o template instala, o que precisa e o que fazer depois:

   ```sh
   brpanel templates show n8n
   ```

   Guarde as linhas `precisa de:`, `memória sugerida (MB):` e `Depois de instalar:`.

3. Instalar, esperando o fim:

   ```sh
   brpanel templates install n8n --domain flows.example.com --wait
   ```

   - `--domain` é opcional. Com ele: Nginx + SSL (o DNS já deve apontar para o servidor).
     Sem ele o app abre pela porta do servidor (`brpanel apps show n8n` mostra a porta).
   - `--name` troca o nome (padrão = id do template). Nome livre: se `n8n` já existe, use outro.
   - `--version` fixa a versão (a padrão aparece em `templates show`).
   - Atalho: `brpanel install gitea --name git --wait`.

4. A resposta traz **`PRÓXIMO PASSO:`**. Repasse esse texto à pessoa (em geral: abrir o endereço e
   criar a conta de administrador no primeiro acesso).

5. Conferir:

   ```sh
   brpanel apps show n8n
   ```

   Procure `estado: active` e `status do deploy: deployed`, e passe a `url:` para a pessoa.

## Cuidados por template

| Template | Lembrete |
|---|---|
| `n8n` | Precisa de uns **1 GB de memória livre** na instalação. Os webhooks já saem com `https://` quando há domínio. |
| `uptime-kuma` | Baixa de um repositório Git; precisa de `git`, `node` e `npm`. |
| `gitea` | Usa SQLite e fica sem SSH; clone por HTTPS. Precisa de `curl` e `git`. O binário é conferido por SHA-256. |

Se faltar Node, o painel o instala sozinho com o nvm. Outros programas que faltem (`curl`, `git`):
peça à pessoa para instalar no servidor.

## Atualizar para a versão mais nova

```sh
brpanel apps deploy n8n --wait
```

Com a versão fixada em `--version`, o deploy mantém aquela versão. Para mudar a versão fixada, use
`brpanel apps env-set n8n TPL_VERSION=2.45.7 --deploy` (use um número que exista; erre e o deploy
falha com o motivo no log).

## Se falhar

```sh
brpanel apps logs n8n --kind deploy --lines 80
```

Causas comuns: pouca memória (`out of memory`: `brpanel apps set n8n --memory-mb 2048` e deploy),
programa faltando no servidor, versão que não existe, domínio sem DNS. Mais em `brpanel-diagnostico`.

## Apagar

`brpanel apps delete NOME --yes` **apaga também os dados do template** (a pasta de armazenamento e
os backups dela). Só com a confirmação da pessoa, depois de avisá-la.
