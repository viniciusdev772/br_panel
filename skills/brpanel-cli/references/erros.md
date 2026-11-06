# Erros da CLI e o que fazer

Procure o começo da mensagem `ERRO:` na primeira coluna. Faça a ação da segunda. Se a resposta
trouxer `DICA:`, ela vale mais do que esta tabela (é gerada pelo programa e está sempre atual).

Código de saída **2** = você escreveu o comando errado (nada foi enviado ao painel).
Código **1** = o painel recusou ou o comando falhou.

## Você escreveu errado (código 2)

| Mensagem | O que fazer |
|---|---|
| `comando desconhecido: "..."` | Use a DICA `você quis dizer`. Ou rode `brpanel help`. |
| `opção desconhecida: --x` | A DICA lista as opções válidas daquele comando. Use uma delas. |
| `faltam argumentos` | Copie o `USO:` e o `EXEMPLO:` que vêm junto e troque os valores. |
| `faltou o valor de --x` | A opção precisa de um valor: `--x VALOR`. |
| `--x precisa de um número inteiro` | Passe só dígitos: `--replicas 2`. |
| `este comando não recebe argumentos` | Você inventou um subcomando. Rode `brpanel help`. |
| `este comando apaga dados e não pode ser desfeito` | **Pare e pergunte à pessoa.** Só com a confirmação dela repita com `--yes` no fim. |
| `a senha não vai na linha de comando` | Use `printf '%s' 'SENHA' \| brpanel apps auth-add NOME USUARIO --password-stdin`. |
| `--env precisa ser CHAVE=VALOR` | Escreva `--env NODE_ENV=production`. |
| `o segundo argumento precisa ser on ou off` | `brpanel apps maintenance NOME on` ou `off`. |
| `faltou --repo` | `apps create` exige `--repo URL`. |
| `faltou --kind e/ou --name` | `dbs create` exige os dois. |

## O painel recusou (código 1)

| Mensagem | O que fazer |
|---|---|
| `não encontrado` | O nome está errado. `brpanel apps list` e `brpanel dbs list`. |
| `banco "x" não existe` / `template "x" não existe` | A DICA lista os nomes que existem. |
| `nome inválido: use 2-32 caracteres...` | Só `a-z`, `0-9` e `-`, começando por letra. Sem maiúsculas, sem `_`, sem espaço. |
| `já existe uma aplicação com esse nome` | Escolha outro nome (apps e bancos dividem os nomes). |
| `URL do repositório inválida` / `use uma URL https://, ssh:// ou git@host:caminho` | Use `https://github.com/dono/repo` (sem espaços). |
| `não coloque credenciais na URL` | Tire usuário e senha/token da URL. Repositório privado: o dono configura chave SSH ou GitHub App no painel web. |
| `branch inválida` | Confira o nome com `git branch -r`; só letras, números, `.`, `_`, `-` e `/`. |
| `domínio inválido (ex.: app.exemplo.com.br)` | Só minúsculas, sem `http://`, sem `/`, sem porta, sem IP. |
| `domínio já usado por outra aplicação` | Outro app usa esse domínio. Veja `brpanel apps list` e escolha outro. |
| `porta deve ser 0 (nenhuma) ou entre 1024 e 65535` | Deixe sem `--port` para o painel escolher. |
| `instâncias deve ficar entre 1 e 16` | Use um número de 1 a 16. |
| `memória máxima deve ser 0 (sem limite) ou entre 32 e ...` | Use 0 ou pelo menos 32 (MB). |
| `CPU máxima deve ser 0 (sem limite) ou entre 5% e ...` | Use 0 ou pelo menos 5. |
| `escalonamento automático: ...` | Releia o skill `brpanel-site` (seção autoscale). A meta de memória exige `--memory-mb`. |
| `deploy automático exige um repositório do GitHub` | Tire `--auto-deploy`, ou use um repositório do GitHub com o GitHub App ligado no painel. |
| `Já existe um deploy em andamento` / `Aguarde...` | Espere: `brpanel apps wait NOME`. Não dispare outro. |
| `o deploy de X falhou` | `brpanel apps logs X --kind deploy --lines 80`. Corrija a causa e rode `brpanel apps deploy X --wait`. |
| Log do deploy: `Remote branch main not found` | A branch não existe. `brpanel apps set X --branch master` (ou a branch certa) e `brpanel apps deploy X --wait`. |
| Log do deploy: `não consegui detectar o tipo do projeto` | Informe `--root PASTA`, ou `--build` e `--start`. Detalhes no skill `brpanel-deploy`. |
| `o deploy de X ainda está rodando depois de N s` | Não é falha. Continue com `brpanel apps wait X`. |
| `O Nginx recusou...` | O valor foi rejeitado e **nada foi salvo**. Corrija o valor (domínio, limite) e repita. |
| `variáveis de ambiente, linha N: use CHAVE=valor` | A chave tem só letras, números e `_`, e não começa por número. |
| `máximo de 100 variáveis de ambiente` | Remova as que não usa (`apps env-unset`). |
| `o limite é de 20 tokens; revogue algum antes` | `brpanel tokens list`, depois `brpanel tokens revoke ID --yes` (com a confirmação da pessoa). |
| `Falha: ...` ou `Não salvo: ...` | O painel explica o motivo no resto da frase. Corrija e repita. |
| `o painel respondeu 5xx` | Erro do servidor. Veja `brpanel events --limit 10` e, no servidor, `systemctl status brpanel`. |
| `resposta inesperada do painel` | A CLI e o painel são de versões diferentes. Atualize a CLI (skill `brpanel-remoto`). |

## Conexão e acesso

| Mensagem | O que fazer |
|---|---|
| `sem permissão para falar com o painel` | Rode com `sudo`. |
| `não consegui falar com o painel (.../brpanel.sock)` | No servidor: `systemctl status brpanel`. Se a pasta de dados não é `/var/lib/brpanel`, passe `--data PASTA`. De outro computador, use `brpanel login`. |
| `o painel ainda não foi configurado` | A pessoa precisa abrir o painel no navegador e concluir a configuração inicial (`/setup`). Não dá para fazer pela CLI. |
| `o painel recusou a sessão local` | `systemctl restart brpanel` e tente de novo. |
| `você ainda não entrou em nenhum servidor` | Falta o login: skill `brpanel-remoto`. |
| `o servidor recusou o token (inválido ou revogado)` | Crie outro token (`sudo brpanel tokens create --name NOME` no servidor) e entre de novo. |
| `muitas tentativas recusadas; o servidor bloqueou este IP por um minuto` | Espere 1 minuto. Antes de tentar de novo, confira o token. |
| `o certificado SSL de ... não é confiável` | Use o **domínio** do painel (com SSL), não o IP. |
| `não consegui conectar a ...` | Confira endereço, porta (padrão 8080 sem SSL) e firewall do servidor. |
| `o servidor ... não usa SSL` | Prefira `https://DOMINIO`. Só em rede de confiança, repita com `--allow-http`. |
| `token inválido: ele começa com brp_` | Você colou outra coisa. O token começa com `brp_`. |

## Quando nada disso resolver

1. `brpanel events --limit 10` (o que o painel registrou).
2. `brpanel apps logs NOME --kind deploy --lines 80` e `brpanel apps logs NOME --lines 100`.
3. Skill `brpanel-diagnostico`.
4. Diga à pessoa o `ERRO:` exato e o que já tentou. Não invente uma causa.
