---
name: brpanel-diagnostico
description: Descobrir por que algo deu errado no BR Panel - site fora do ar, erro 502/503, deploy que falha, app que cai, banco que não conecta, SSL que não emite, disco ou memória cheios. Siga o roteiro de diagnóstico com `brpanel status`, `events` e `apps logs` antes de mudar qualquer coisa.
---

# Diagnóstico: algo quebrou

Pré-requisito: leu o skill `brpanel-cli`.

**Regra de ouro: primeiro olhe, depois mexa.** Todos os comandos abaixo só leem. Não reinicie,
não faça deploy e não apague nada antes de saber a causa.

## Roteiro (sempre nesta ordem)

1. **O painel responde?**

   ```sh
   brpanel status
   ```

   - Se der erro de conexão, o problema é o painel ou o acesso: tabela de erros em
     `brpanel-cli/references/erros.md`. No servidor: `systemctl status brpanel`.
   - Leia a 1ª linha: carga, memória e **disco**. Disco quase cheio, ou memória toda usada, derruba
     tudo. Se for o disco, `brpanel cache status` mostra o cache de builds e
     `brpanel cache prune --days 7` apaga o que ninguém usa há 7 dias (seguro, só custa baixar de
     novo).
   - Veja a tabela: o app aparece com `ESTADO` `active`? Se está `failed` ou `inactive`, é ele.

2. **O que aconteceu há pouco?**

   ```sh
   brpanel events --limit 10
   ```

   Mostra deploys, falhas de provisionamento de banco, backups e criações, com horário.

3. **Estado e configuração do app:**

   ```sh
   brpanel apps show loja
   ```

   Leia `estado`, `status do deploy`, `último deploy`, `variáveis faltando`, `build`, `início`,
   `porta` e `domínio`.

4. **Log do último deploy** (se o `status do deploy` for `failed`):

   ```sh
   brpanel apps logs loja --kind deploy --lines 100
   ```

5. **Log do app rodando** (se o deploy passou, mas o site cai ou responde erro):

   ```sh
   brpanel apps logs loja --lines 200
   ```

6. **Banco** (se o log fala em conexão, `ECONNREFUSED`, `authentication failed`):

   ```sh
   brpanel dbs list
   brpanel dbs show pg
   ```

   `ESTADO` deve ser `active` e `status` `ready`.

## Sintoma → causa provável → o que fazer

| Sintoma | Causa provável | Ação |
|---|---|---|
| Site dá **502 Bad Gateway** | O app caiu ou não escuta em `$PORT` | Passos 3 e 5. Confirme que `início` usa `$PORT`. Depois `brpanel apps restart loja`. |
| Site dá **503** | Página de manutenção ligada | `brpanel apps show loja` mostra `manutenção: sim`; desligue com `brpanel apps maintenance loja off`. |
| Site dá **413** ao enviar arquivo | Limite de upload do Nginx (1 MB) | `brpanel apps limits loja --upload-mb 100`. |
| Site dá **429** | Limite de requisições por IP | `brpanel apps set loja --rate-limit 0` e deploy (ou suba o limite). |
| Pede **usuário e senha** sem querer | Senha de site ligada | `brpanel apps show loja` (`usuários com senha`); `brpanel apps auth-remove loja USUARIO`. |
| **Sem cadeado** (HTTP) | O certificado não foi emitido (DNS não aponta para o servidor) | Corrija o DNS, depois `brpanel apps deploy loja --wait` (tenta de novo). |
| App cai logo depois de subir | Erro no código, variável faltando, banco fora | Passos 3, 5 e 6. |
| `aviso de porta` em `apps show`, `AVISO DE PORTA` ao publicar, ou `EADDRINUSE` / `Address already in use` no log | O código fixa a porta (ex.: `const port = 3090`) em vez de ler `$PORT`; duas cópias do mesmo app, ou várias instâncias, brigam pela mesma porta | Leia o log do deploy (`brpanel apps logs loja --kind deploy --lines 40`): ele diz quem usa a porta e em que arquivo e linha ela está escrita. O certo é o código usar `process.env.PORT` (Node), `os.environ["PORT"]` (Python) ou `os.Getenv("PORT")` (Go). Sem mexer no código, com **uma** instância: `brpanel apps set loja --port N` (N = a porta do aviso) e deploy. Várias instâncias só com o código corrigido. |
| `Killed` / `out of memory` no log | Falta memória | `brpanel apps set loja --memory-mb 1024`, ou menos instâncias; deploy. |
| Deploy "trava" | Build demorado (ou fila do monorepo) | `brpanel apps wait loja`. Não dispare outro. |
| Deploy não dispara no push | Auto-deploy desligado ou GitHub App | `brpanel apps show loja` (`deploy automático`); o GitHub App é configurado no painel web. |
| Muita lentidão | CPU saturada | `brpanel status`; considere `--replicas` ou `--autoscale` (skill `brpanel-site`). |
| Banco `failed` | Servidor do banco não instalado | Peça à pessoa para instalar pelo painel web. |

## Depois de corrigir

1. Aplique **a menor mudança** que resolve (uma variável, um comando, um limite).
2. `brpanel apps deploy loja --wait` quando a mudança exigir.
3. Confirme: `brpanel apps show loja` e `brpanel apps logs loja --lines 50` sem erros novos.
4. Relate: **sintoma → causa → o que mudou → resultado**. Se não achou a causa, diga isso e mostre o
   `ERRO:` e as linhas do log que viu. Não invente.

## Quando parar e chamar a pessoa

- O erro pede algo que a CLI não faz (instalar servidor de banco, mexer no GitHub App, DNS).
- A mesma correção falhou 2 vezes.
- A solução envolve apagar dados, parar um app em produção ou trocar a senha de um banco.
- A causa está no código do projeto (bug da aplicação): mostre o trecho do log.
