---
name: brpanel-cli
description: Comece por aqui sempre que a tarefa for controlar um servidor com o BR Panel pelo comando `brpanel` (publicar ou atualizar um site/app, ver logs, variáveis, bancos de dados, domínio, SSL). Traz as regras que evitam erro, como ler OK/ERRO/DICA e qual outro skill abrir para cada tarefa.
---

# BR Panel CLI: comece por aqui

O BR Panel publica repositórios Git como serviços (systemd) com Nginx e SSL, cria bancos
(PostgreSQL, MySQL/MariaDB, MongoDB, Redis) e instala apps prontos (n8n, Uptime Kuma, Gitea).
Você o controla com **um programa só: `brpanel`**. Sem Docker.

## 1. Antes do primeiro comando

Escolha o caso e faça só os passos dele.

| Onde você está | O que fazer |
|---|---|
| **No servidor do painel** (Linux) | Use `sudo brpanel ...`. Não precisa de senha nem de token. |
| **Em outro computador** (notebook, Windows, macOS, Termux) | Abra o skill `brpanel-remoto` e entre com `brpanel login`. |

Depois confira se está tudo certo (um comando só):

```sh
brpanel status
```

Se preferir um teste guiado, rode `sh scripts/verificar.sh` (está neste skill). Ele diz em
português o que falta.

## 2. As 12 regras

1. **Nunca invente comando nem opção.** Se não tiver certeza, rode `brpanel help` ou
   `brpanel help apps deploy` (troque pelo comando que quer). A lista completa está em
   [references/comandos.md](references/comandos.md).
2. **Um comando por vez.** Leia a resposta inteira antes do próximo.
3. **A resposta começa com `OK:` ou `ERRO:`.** Depois do `ERRO:` vêm linhas `DICA:`, `USO:` e
   `EXEMPLO:`. **Faça o que a DICA manda.** Elas foram escritas para você.
4. **Descubra antes de agir.** Nomes de apps e bancos vêm de `brpanel apps list` e
   `brpanel dbs list`. Nunca chute um nome.
5. **Nomes** têm 2 a 32 caracteres: letras minúsculas, números e hífen, começando por letra
   (`loja`, `api-pedidos`). Apps e bancos dividem o mesmo espaço de nomes.
6. **Publicar ou atualizar:** `brpanel apps deploy NOME --wait`. O `--wait` espera e diz se deu certo.
7. **Mudar configuração ou variável só vale depois de um novo deploy.** Use `--deploy` em
   `apps env-set` e rode `apps deploy NOME --wait` depois de `apps set`.
8. **Se o deploy falhar:** `brpanel apps logs NOME --kind deploy --lines 80`, corrija a causa e
   rode o deploy de novo. Não repita o mesmo deploy às cegas.
9. **Comandos que apagam dados exigem `--yes` no fim.** Só use `--yes` se a pessoa pediu
   *aquela* exclusão, com aquele nome. Em caso de dúvida, pergunte antes.
10. **Senha nunca vai na linha de comando.** Use `printf '%s' 'SENHA' | brpanel ... --password-stdin`
    (ou `--token-stdin`).
11. **Aspas simples em volta de `${{...}}`**, para o shell não mexer:
    `brpanel apps env-set api 'DATABASE_URL=${{pg.DATABASE_URL}}'`.
12. **Segredos não saem na conversa.** `brpanel apps env` e `brpanel tokens create` mostram segredos:
    use o resultado, mas não o repita ao usuário sem necessidade.

## 3. Como ler a resposta

| Você viu | Significa | Código de saída |
|---|---|---|
| `OK: ...` | Deu certo. | 0 |
| `ERRO: ...` e `DICA: ...` | O painel recusou. Siga a DICA. | 1 |
| `ERRO: ...` e `USO: ...` | Você escreveu o comando errado. Copie o `USO:` ou o `EXEMPLO:`. | 2 |
| Uma tabela de texto | Listagens (`apps list`, `status`). | 0 |

Para um programa ler a resposta, acrescente `--json` (vale em qualquer comando).
Tabela de erros comuns e o que fazer: [references/erros.md](references/erros.md).

**Atenção:** `OK:` num comando que roda em segundo plano (`dbs create`, `dbs backup`,
`apps deploy` sem `--wait`, `templates install` sem `--wait`) quer dizer só "o pedido foi aceito".
Confirme o resultado depois (veja o skill da tarefa).

## 4. Qual skill abrir

| A pessoa quer... | Skill |
|---|---|
| Publicar um repositório, atualizar, reiniciar, ver log, deploy que falha, monorepo | `brpanel-deploy` |
| Variáveis de ambiente, criar/ligar banco de dados, backup | `brpanel-dados` |
| Domínio, SSL, redirecionamento, manutenção, senha no site, mais instâncias, autoscale | `brpanel-site` |
| Instalar n8n, Uptime Kuma, Gitea ou outro app pronto | `brpanel-templates` |
| Usar a CLI de outro computador, criar ou revogar token | `brpanel-remoto` |
| Algo quebrou e não se sabe por quê | `brpanel-diagnostico` |

## 5. O ciclo de trabalho (use sempre)

1. **Ver:** `brpanel status` e `brpanel apps list` (o que existe e em que estado).
2. **Agir:** um comando, o mais específico possível.
3. **Conferir:** `brpanel apps show NOME` (estado, URL) ou `brpanel apps logs NOME`.
4. **Relatar:** diga o que fez, o que viu na conferência e a URL final. Se algo falhou, diga o
   `ERRO:` exato.

## 6. Nunca faça

- Rodar `brpanel raw` se existir um comando próprio (é o último recurso).
- Editar arquivos do painel à mão (`/var/lib/brpanel`, units do systemd, vhosts do Nginx). Tudo se
  faz pelos comandos; o painel valida e desfaz o que o Nginx recusar.
- Colocar `PORT` ou `HOST` nas variáveis: o painel já entrega as duas a todo app.
- Apagar (`apps delete`, `dbs delete`, `cache clear`, `tokens revoke`) para "resolver" um erro.
- Usar `--yes` por hábito ou "para ver se passa".
- Fazer vários deploys seguidos do mesmo app: o segundo é recusado enquanto o primeiro roda
  (`Já existe um deploy em andamento`). Use `brpanel apps wait NOME`.

## 7. O que a CLI não faz (peça à pessoa para usar o painel web)

- Instalar o servidor de um banco (PostgreSQL, MariaDB, Redis, MongoDB) quando falta.
- Mudar as configurações do painel, o GitHub App ou a Cloudflare.
- Restaurar backups, navegar nos dados do banco, configurar pastas de armazenamento (volumes).

## Referências

- [references/comandos.md](references/comandos.md): os 49 comandos, opções e exemplos.
- [references/erros.md](references/erros.md): cada mensagem de erro e o que fazer.
- `brpanel guide`: o guia oficial em texto puro, gerado pelo próprio programa (sempre atual).
