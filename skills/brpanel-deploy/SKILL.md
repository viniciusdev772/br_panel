---
name: brpanel-deploy
description: Publicar, atualizar, reiniciar e desligar aplicações no BR Panel com `brpanel apps ...`, a partir de um repositório Git (Node, Go, Python, Rust, PHP, Ruby, site estático, monorepo). Use também quando um deploy falha, o app não sobe ou é preciso ver o log.
---

# Publicar e atualizar aplicações

Pré-requisito: leu o skill `brpanel-cli` (regras e como ler `OK:`/`ERRO:`).

## Como o deploy funciona (o suficiente para não errar)

1. O painel baixa o repositório (`git clone`/`fetch`), **descobre sozinho** como compilar e iniciar,
   roda o build, cria um serviço systemd e (com domínio) configura Nginx e SSL.
2. O app **sempre recebe `PORT` e `HOST`** como variáveis. O comando de início **precisa escutar
   em `$PORT`** (e em `$HOST`, se der). Se o código usa uma porta fixa, veja "Porta fixa" abaixo.
3. O deploy roda em **segundo plano**. `apps deploy` volta na hora. Com `--wait` o comando espera e
   termina com `OK:` ou `ERRO:`.
4. Só depois de `OK:` o app está no ar com a configuração nova.

## Receita 1: publicar um repositório (sem domínio)

```sh
brpanel apps create --repo https://github.com/exemplo/loja --name loja --wait
```

- `--repo` é obrigatório. Vale `https://...`, `ssh://...` ou `git@host:dono/repo.git`.
  Sem usuário/senha dentro da URL.
- `--name` é opcional (sai do nome do repositório), mas **sempre passe**: fica previsível.
- Sem `--domain` o app abre pela porta do servidor. Veja a porta com `brpanel apps show loja`.

Confira:

```sh
brpanel apps show loja
```

Procure: `estado: active` e `status do deploy: deployed`. A linha `url:` é o endereço.

## Receita 2: publicar com domínio e SSL

Antes: o **DNS do domínio já precisa apontar para o IP do servidor** (registro A). O painel espera
o DNS aparecer (até 2 minutos) antes de pedir o certificado. Se você não sabe se o DNS está
pronto, pergunte à pessoa.

```sh
brpanel apps create --repo https://github.com/exemplo/loja --name loja --domain loja.example.com --wait
```

- Domínio em minúsculas, sem `https://`, sem `/` no fim.
- Se o certificado falhar, o site continua em HTTP e o **próximo deploy tenta de novo**. Não é
  preciso apagar nada: corrija o DNS e rode `brpanel apps deploy loja --wait`.
- Mais de um domínio, `www`, redirecionamentos: skill `brpanel-site`.

## Receita 3: atualizar depois de mudar o código

```sh
brpanel apps deploy loja --wait
```

O painel só baixa a diferença e reaproveita as dependências. Se o site quebrou por dependências
velhas ou cache estranho, refaça do zero (demora mais, não apaga dados nem armazenamento):

```sh
brpanel apps deploy loja --clean --wait
```

## Receita 4: mudar a configuração

`apps set` só muda o que você passar. **Depois, sempre faça o deploy.**

```sh
brpanel apps set loja --branch develop --build 'npm ci && npm run build' --start 'npm start'
brpanel apps deploy loja --wait
```

Opções úteis: `--branch`, `--domain`, `--port`, `--build`, `--start`, `--replicas`, `--memory-mb`,
`--cpu-percent`, `--auto-deploy` (deploy a cada push; só repositório do GitHub com o GitHub App
ligado pelo dono no painel). Lista completa em `brpanel help apps set`.

Comandos de build/início: **uma linha só**, até 500 caracteres, e o de início usa `$PORT`:
`--start 'node server.js --port $PORT'`. Use aspas simples para o shell não trocar `$PORT`.

## Receita 5: ligar, desligar, reiniciar

```sh
brpanel apps restart loja
brpanel apps stop loja
brpanel apps start loja
```

`stop` tira o site do ar. Só faça se a pessoa pediu.

## O deploy falhou ou o app não sobe

Siga nesta ordem. Não pule.

1. **Ler o motivo** (o erro está no fim do log):

   ```sh
   brpanel apps logs loja --kind deploy --lines 80
   ```

2. **Se o deploy passou mas o site não responde**, leia o log do app rodando:

   ```sh
   brpanel apps logs loja --lines 100
   ```

3. **Procure a causa** na tabela e corrija:

   | O log mostra | Causa | Correção |
   |---|---|---|
   | `Remote branch main not found in upstream origin` | O repositório não tem a branch `main` (o padrão da CLI); muitos usam `master` | `brpanel apps set loja --branch master` e deploy. Se não souber o nome, pergunte à pessoa. |
   | `não consegui detectar o tipo do projeto` | Não há `package.json`, `go.mod`, `requirements.txt`... na raiz | Se o projeto está numa subpasta: `brpanel apps set loja --root PASTA`. Senão informe `--build`/`--start`, ou peça à pessoa um `Procfile` ou `brpanel.json` (veja "Detecção" abaixo). |
   | `Permission denied (publickey)` / `could not read Username` | Repositório privado sem acesso | A CLI não configura acesso. O dono liga o GitHub App ou uma chave de deploy SSH no painel web; avise a pessoa. |
   | `command not found: node` / `go` / `python3` / `cargo`... | Falta o programa no servidor | Node: o painel instala com nvm sozinho. Outros: peça à pessoa para instalar no servidor. Não instale pacotes por conta própria. |
   | `EADDRINUSE` / `address already in use` | O código usa porta fixa | `brpanel apps set loja --port 3090` (a porta que o código usa) e deploy. |
   | O app sobe e cai logo; `ECONNREFUSED` no banco | Falta ligar o banco | Skill `brpanel-dados`. |
   | `undefined` ou `missing ... env` / variável não encontrada | Falta variável | `brpanel apps show loja` mostra `variáveis faltando`; depois `brpanel apps env-set loja CHAVE=valor --deploy`. |
   | `npm ERR!` / `ERESOLVE` / erro de instalação | Dependências | `brpanel apps deploy loja --clean --wait`. Se persistir, é problema do código: relate o erro. |
   | `Cannot find module` / arquivo principal não existe | Comando de início errado | `brpanel apps set loja --start 'COMANDO CERTO'` e deploy. |
   | `out of memory` / `Killed` | Pouca memória | `brpanel apps set loja --memory-mb 1024` (ou 0 para sem limite) e deploy. |
   | `nginx: ... recusou` | Valor de site inválido | Nada foi salvo. Corrija o valor e repita. |

4. **Corrija e rode o deploy de novo com `--wait`.** Se falhar 2 vezes com a mesma causa, pare e
   relate o erro exato à pessoa.

## Detecção automática e como sobrescrever

Se `--build` e `--start` ficarem vazios, o painel detecta pelo arquivo do projeto e **salva o
resultado** na app (dá para editar com `apps set`). Tabela e exemplos em
[references/deteccao.md](references/deteccao.md).

Para o repositório se descrever sozinho, ponha na raiz um `brpanel.json`:

```json
{"build": "npm ci && npm run build", "start": "node dist/server.js --port $PORT", "port": 8080}
```

ou um `Procfile` com a linha `web: node dist/server.js --port $PORT`.

## Porta fixa no código

Se o código escreve a porta no próprio fonte (`const port = 3090`) e não lê `PORT`:

```sh
brpanel apps set loja --port 3090
brpanel apps deploy loja --wait
```

Se o código também usa `localhost:3306` para o banco, isso exige "Ativar repasse" no painel web
(Banco de dados > Configurações). Não dá pela CLI: avise a pessoa.

## Monorepo (vários apps no mesmo repositório)

Cria-se **uma app por projeto**, apontando a pasta com `--root`:

```sh
brpanel apps create --repo https://github.com/exemplo/mono --name mono-web --root apps/web --watch 'packages/ui/**' --auto-deploy --wait
brpanel apps create --repo https://github.com/exemplo/mono --name mono-api --root apps/api --wait
```

- `--root`: pasta do projeto, relativa, sem `..`, sem barra no começo.
- `--watch`: pastas irmãs que também fazem esta app publicar. Uma por vírgula; `!` no começo
  ignora (`'packages/ui/**,!**/*.md'`). Sempre entre aspas simples.
- `brpanel apps show NOME` avisa `monorepo: sim; outras pastas: ...` quando detecta o caso, e você
  deve criar uma app por pasta que a pessoa quiser publicar. A descoberta automática das pastas
  é feita no painel web; pela CLI, pergunte à pessoa quais pastas publicar.
- Apps do mesmo repositório publicam uma de cada vez (fila). É normal demorar.

## Apagar uma app

```sh
brpanel apps delete loja --yes
```

**Só com a confirmação expressa da pessoa.** Apaga o serviço, o site (Nginx), os arquivos **e as
pastas de armazenamento (volumes) com os backups delas**. **Não** apaga bancos de dados.
Para só tirar o site do ar, use `brpanel apps stop loja`.

## Confira sempre ao terminar

```sh
brpanel apps show loja
```

Relate à pessoa: nome, `estado`, `status do deploy`, `url` e qualquer `variáveis faltando`.
