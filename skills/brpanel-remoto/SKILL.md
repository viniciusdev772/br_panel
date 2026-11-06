---
name: brpanel-remoto
description: Usar a CLI do BR Panel de OUTRO computador (notebook, Windows, macOS, Termux no Android) - instalar o `brpanel`, criar token, `brpanel login`, `whoami`, `logout`, revogar token. Use quando aparecer "você ainda não entrou em nenhum servidor" ou quando o agente não roda dentro do servidor.
---

# Controlar o servidor de outro computador

Pré-requisito: leu o skill `brpanel-cli`.

No **servidor** do painel não precisa de nada disto: `sudo brpanel ...` fala por um socket local.
Neste skill o agente roda **em outro computador** e precisa de três coisas: a CLI instalada, um
token e o login.

## Passo 1: instalar só a CLI

| Sistema | Comando |
|---|---|
| Linux, macOS, Termux (Android arm64) | `curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.sh \| sh` |
| Windows (PowerShell) | `irm https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.ps1 \| iex` |

O instalador baixa a última versão, confere o SHA-256 e instala **sem administrador**
(`~/.local/bin`, `$PREFIX/bin` no Termux ou `%LOCALAPPDATA%\Programs\brpanel` no Windows). Se o
comando `brpanel` não for achado depois, abra um novo terminal (o instalador ajusta o PATH).

Confirme:

```sh
brpanel help
```

## Passo 2: obter um token

O token dá o poder total do painel. **É a pessoa quem deve criá-lo**, de um destes jeitos:

- No painel web: aba **CLI** > Acesso remoto > criar token.
- No servidor: `sudo brpanel tokens create --name notebook`.

O segredo (começa com `brp_`) aparece **só uma vez**. Nunca invente um token, nunca o guarde em
arquivo do projeto, nunca o mostre de volta. Se a pessoa perdeu, crie outro.

## Passo 3: entrar

Use **o domínio do painel** (com SSL). A senha/token vem da entrada padrão, nunca na linha de comando:

```sh
printf '%s' 'brp_exemplo' | brpanel login painel.example.com --token-stdin
```

- `painel.example.com` é o endereço do painel. Sem `http://`: a CLI usa `https://` para domínio.
- Só com IP o painel responde em `http://IP:8080` **sem SSL**, e o token viajaria sem criptografia.
  A CLI recusa. Só aceite em rede de confiança (VPN, rede local) e **com o aval da pessoa**:

  ```sh
  printf '%s' 'brp_exemplo' | brpanel login 192.0.2.10 --token-stdin --allow-http
  ```

- Sucesso: `OK: conectado a https://... Agora os comandos controlam esse servidor.`

## Passo 4: usar

Daqui em diante os comandos são os mesmos, **sem sudo**:

```sh
brpanel status
brpanel apps list
```

Para saber qual servidor está ativo:

```sh
brpanel whoami
```

`modo: remoto` e `servidor: https://...` quer dizer que está tudo certo. `modo: local` quer dizer
que não há login salvo.

## Sair e revogar

```sh
brpanel logout
```

`logout` só esquece o login **neste computador**; o token continua válido no servidor. Para
invalidá-lo (com a confirmação da pessoa):

```sh
brpanel tokens list
brpanel tokens revoke 2 --yes
```

O `2` é o ID da primeira coluna de `tokens list`. Quem usa aquele token perde o acesso na hora.
Há no máximo 20 tokens.

## Erros típicos

| Mensagem | Causa e correção |
|---|---|
| `você ainda não entrou em nenhum servidor` | Falta o passo 3. |
| `o servidor recusou o token (inválido ou revogado)` | Token errado ou revogado: peça outro e refaça o passo 3. |
| `muitas tentativas recusadas ... por um minuto` | Espere 1 minuto; confira o token antes de tentar. |
| `o certificado SSL de ... não é confiável` | Você usou o IP ou um domínio diferente do certificado. Use o domínio do painel. |
| `não consegui conectar a ...` | Endereço, porta ou firewall. O painel sem domínio usa a porta 8080. |
| `o servidor ... não usa SSL` | Veja a regra do IP no passo 3. |
| `token inválido: ele começa com brp_` | Foi colado outro texto. |
| `unexpected e_type: 2` (Termux) | Você baixou o binário Linux. Use o instalador, que baixa o build `android`. |

## Onde o login fica

Num arquivo `brpanel/config.json` na pasta de configuração do usuário (permissão só do dono). Por
isso um computador compartilhado exige `brpanel logout` ao terminar.

## Dentro do servidor, mesmo com login salvo

Para usar o socket local em vez do servidor remoto, acrescente `--local` ao comando:

```sh
sudo brpanel status --local
```
