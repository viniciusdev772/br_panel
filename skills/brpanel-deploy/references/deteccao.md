# Detecção automática do projeto

Com `--build` e `--start` vazios, o painel olha os arquivos do repositório (ou da pasta `--root`) e
escolhe os comandos. O resultado é **salvo na app**: `brpanel apps show NOME` mostra `tipo detectado`,
`build` e `início`. Para trocar, use `brpanel apps set NOME --build '...' --start '...'` e faça o deploy.

## Tabela

| Arquivo encontrado | Tipo | Build / início (resumo) |
|---|---|---|
| `package.json` | Node.js | Instala com `npm ci`, `pnpm`, `yarn` ou `bun` conforme o lockfile, roda `run build`; inicia por `start`, Next, Nuxt, Vite ou o arquivo principal |
| `go.mod` | Go | `go build -o app .` (ou `./cmd/<x>`), inicia `./app` |
| `Cargo.toml` | Rust | `cargo build --release`, inicia `./target/release/<nome>` |
| `requirements.txt`, `pyproject.toml` | Python | venv + pip; Django/Flask por gunicorn, FastAPI por uvicorn |
| `Gemfile` | Ruby | `bundle install`; Rails ou Rack |
| `composer.json` | PHP | `composer install`; servidor embutido do PHP |
| `index.html` | Site estático | `python3 -m http.server` |

Um `Procfile` (linha `web:`) define o comando de início. Um `brpanel.json` na raiz
(`{"build": "...", "start": "...", "port": 8080}`) sobrescreve tudo.

## Regras dos comandos que você escreve

- **Uma linha**, no máximo 500 caracteres. Para algo maior, ponha um script no repositório
  (`--build 'sh scripts/build.sh'`).
- No comando de início, a porta é **`$PORT`** e o endereço é **`$HOST`**. Com domínio, `HOST` é
  `127.0.0.1`; sem domínio, `0.0.0.0`.
- Escreva entre **aspas simples** na CLI (`--start 'node app.js --port $PORT'`) para o seu shell não
  trocar `$PORT` por vazio.
- Quando o build começa com `npm ci` (ou `pnpm install --frozen-lockfile`, `yarn install
  --frozen-lockfile`, `bun install`, `composer install --no-dev --optimize-autoloader`) e o
  lockfile não mudou, o painel **pula a instalação** e acelera o deploy. Por isso mantenha o
  comando nessa forma simples, sem `;` nem `||` no começo.

## Exemplos prontos

Node (Express) que lê `PORT`:

```sh
brpanel apps create --repo https://github.com/exemplo/api-node --name api-node --wait
```

Next.js (o painel detecta; só confirme depois):

```sh
brpanel apps create --repo https://github.com/exemplo/site-next --name site-next --domain site.example.com --wait
```

Python Flask com comando próprio:

```sh
brpanel apps create --repo https://github.com/exemplo/api-flask --name api-flask --build 'pip install -r requirements.txt' --start 'gunicorn -b $HOST:$PORT app:app' --wait
```

Site estático com `index.html`:

```sh
brpanel apps create --repo https://github.com/exemplo/landing --name landing --domain landing.example.com --wait
```

## Se a detecção errar

1. `brpanel apps show NOME` e veja `build` e `início`.
2. Corrija só o que está errado: `brpanel apps set NOME --start 'COMANDO CERTO'`.
3. `brpanel apps deploy NOME --wait`.
4. Ainda errado? Peça à pessoa para pôr um `brpanel.json` no repositório.

## Projetos que não dá para publicar

Projetos Android, Gradle e Flutter aparecem como "não publicáveis" no aviso de monorepo: o painel
roda serviços de rede, não apps de celular. Avise a pessoa em vez de insistir.
