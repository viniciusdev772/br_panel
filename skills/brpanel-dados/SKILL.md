---
name: brpanel-dados
description: Variáveis de ambiente e bancos de dados no BR Panel (`brpanel apps env-set`, `brpanel dbs create`, referências `${{pg.DATABASE_URL}}`, backup, trocar senha). Use para ligar um app a PostgreSQL, MySQL/MariaDB, MongoDB ou Redis, ou para definir segredos e configurações do app.
---

# Variáveis de ambiente e bancos de dados

Pré-requisito: leu o skill `brpanel-cli`.

## Variáveis de ambiente

### Regras

- Formato: `CHAVE=valor`. A chave tem letras, números e `_`, e não começa por número. Máximo de 100.
- `PORT` e `HOST` **não se definem**: o painel já entrega as duas a todo app.
- `apps env-set` troca só as chaves que você passar; as outras ficam. Chave repetida: vale a última.
- **Só vale no próximo deploy.** Junte `--deploy` para fazer tudo de uma vez.
- `brpanel apps env NOME` mostra **segredos**. Use só quando precisar; não repita os valores
  para a pessoa.

### Receitas

Definir e aplicar:

```sh
brpanel apps env-set loja NODE_ENV=production API_URL=https://api.example.com --deploy
```

Valor com espaço ou caractere especial: aspas simples em volta do par inteiro.

```sh
brpanel apps env-set loja 'SAUDACAO=olá, mundo' --deploy
```

Remover:

```sh
brpanel apps env-unset loja DEBUG --deploy
```

Ver o que existe:

```sh
brpanel apps env loja
```

Já na criação do app:

```sh
brpanel apps create --repo https://github.com/exemplo/api --name api --env NODE_ENV=production --env LOG_LEVEL=info --wait
```

### Variáveis que o código exige e faltam

A cada deploy o painel lê o `.env.example` e o código, e compara com as variáveis do app. O que
falta aparece em `brpanel apps show NOME` na linha **`variáveis faltando`**. Defina cada uma com
`brpanel apps env-set NOME CHAVE=valor`. **Peça o valor à pessoa** quando for um segredo (chave de
API, senha); não invente. As **opcionais** têm valor padrão no código e podem ficar de fora.
Salvar as variáveis já tira o aviso, sem novo deploy (mas o app só usa o valor novo depois do deploy).

## Bancos de dados

Cada banco é um serviço próprio (systemd), com porta, usuário e senha gerados pelo painel, ouvindo
só em `127.0.0.1`. Tipos: `postgres`, `mysql`, `mariadb`, `mongodb`, `redis`.

### Receita: criar um banco e ligar num app

1. Criar:

   ```sh
   brpanel dbs create --kind postgres --name pg
   ```

   O `OK:` quer dizer só "pedido aceito". A preparação roda em segundo plano.

2. **Esperar ficar pronto** (repita até `status` virar `ready`; pode levar alguns instantes):

   ```sh
   brpanel dbs show pg
   ```

   | `status` | O que fazer |
   |---|---|
   | `provisioning` | Espere e rode `dbs show` de novo. |
   | `ready` | Siga para o passo 3. |
   | `failed` | Veja o motivo com `brpanel events --limit 5`. Se for "initdb" ou o programa do banco não existir, **o servidor do banco não está instalado**: peça à pessoa para instalar pelo painel web (Novo banco de dados > Instalar). Não é um comando da CLI. Depois apague o banco que falhou (com a confirmação dela) e crie de novo. |

3. Copiar as referências que o `dbs show` imprime e usá-las nas variáveis do app:

   ```sh
   brpanel apps env-set api 'DATABASE_URL=${{pg.DATABASE_URL}}' --deploy
   ```

   **Aspas simples** em volta, senão o shell estraga o `${{...}}`.

4. Conferir: `brpanel apps logs api --lines 50` (sem erro de conexão) e `brpanel dbs list`
   (a coluna `USADO POR` mostra `api`).

### Referências disponíveis

`${{NOME.VARIAVEL}}`, onde `NOME` é o nome do banco. As variáveis são: `URL`, `HOST`, `PORT`,
`USER`, `PASSWORD`, `DATABASE` e a URL específica do tipo (`DATABASE_URL` no PostgreSQL e MySQL,
`MONGO_URL` no MongoDB, `REDIS_URL` no Redis). **Use exatamente as que `brpanel dbs show NOME`
imprimir**, é a lista oficial daquele banco.

O painel troca as referências pelo valor real na hora do deploy. Você nunca vê nem digita a senha.

### Outras ações

```sh
brpanel dbs list
brpanel dbs backup pg
brpanel dbs restart pg
brpanel events --limit 5
```

- `dbs backup` roda em segundo plano; confirme em `events`. Os backups são listados e restaurados
  no painel web (a CLI não restaura).
- `dbs start`, `dbs stop` e `dbs restart` ligam, desligam e reiniciam. **Parar um banco derruba
  os apps que dependem dele**; avise a pessoa antes.

### Ações perigosas (só com confirmação da pessoa)

```sh
brpanel dbs regenerate pg --yes
```

Troca a senha do banco. **Depois, faça o deploy de cada app que o usa** (a coluna `USADO POR` de
`dbs list` diz quais); até lá elas falham ao conectar.

```sh
brpanel dbs delete pg --yes
```

Apaga o banco **e todos os dados dele**, sem volta. Antes, faça `brpanel dbs backup pg`, confira
em `events` e avise a pessoa de que os apps que o usam vão quebrar.

### Código com banco em localhost fixo

Se o código escreve `localhost:3306` (ou 5432, 27017, 6379) em vez de ler variáveis, o painel
oferece "Ativar repasse" em Banco de dados > Configurações (painel web). Pela CLI não dá: avise
a pessoa.
