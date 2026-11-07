---
paths:
  - "scripts/install.sh"
  - "CHANGELOG.md"
  - "Makefile"
---

# Versionamento e liberação (releases e assets)

## Versões

- [SemVer](https://semver.org/lang/pt-BR/) com prefixo `v`: `vMAJOR.MINOR.PATCH` (primeira release: `v0.1.0`). Antes da 1.0, `MINOR` para funcionalidade nova ou mudança visível e `PATCH` para correção.
- Tag só em commit que está na `main` e passou em `make check`. **Nunca mova nem apague uma tag publicada**: o `install.sh` e quem já instalou dependem dela. Corrigiu algo? Libere uma versão nova.
- Tags anotadas: `git tag -a vX.Y.Z -m "vX.Y.Z"`, depois `git push origin vX.Y.Z`.

## Changelog

- Ao liberar, renomeie `## [Não lançado]` para `## [X.Y.Z] - AAAA-MM-DD`, crie de novo a seção vazia `## [Não lançado]` acima dela e atualize a frase de abertura (hoje diz que não há versões numeradas).
- O texto da release no GitHub é a seção dessa versão do `CHANGELOG.md`, sem reescrever.

## Assets

- Cada release leva exatamente estes arquivos, por arquitetura (`amd64`, `arm64`):
  - `brpanel-linux-<arch>`: binário estático do servidor, `CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go build -trimpath -ldflags "-s -w" -o dist/brpanel-linux-<arch> ./cmd/brpanel`
  - `brpanel-cli-<os>-<arch>` (`os` = `linux`, `darwin`, `windows`; `.exe` no Windows): só a CLI, para o computador de quem usa, de `./cmd/brpanel-cli`. Mais `brpanel-cli-android-arm64` (Termux; `GOOS=android`, PIE — o build `linux` estático dá `unexpected e_type: 2` lá).
  - um `.sha256` para cada um, saída de `sha256sum` rodada dentro de `dist/` (nome de arquivo sem caminho). Total: 18 arquivos (9 binários + 9 somas).
- `scripts/install.sh` (servidor) e `scripts/install-cli.sh` / `scripts/install-cli.ps1` (CLI) baixam `releases/latest/download/<nome>` e conferem o `.sha256`. **Renomear ou trocar o formato de um asset exige mudar o instalador correspondente no mesmo commit**, e uma release com assets faltando quebra a instalação de todo mundo.
- `dist/` e binários nunca entram no Git (veja `git-workflow.md`); eles vivem só na release.
- Nada de segredo, banco de dados, chave ou dado de servidor nos assets ou nas notas da release: as releases são públicas.

## Como liberar

1. `make check` verde, `CHANGELOG.md` ajustado, commit e push na `main`.
2. A release continua manual (o CI só verifica): rode `scripts/release.sh vX.Y.Z` na `main` (compila, gera os `.sha256`, cria a tag e publica com `gh`).
3. Confira a release: 18 assets, tamanhos plausíveis e `sha256sum -c` passando.
4. Teste o `scripts/install.sh` a partir da URL pública num servidor com o painel já instalado (caminho de atualização) antes de dizer que está pronta.

## `scripts/install.sh`

- POSIX `sh` (roda em dash via `curl | sh`), fim de linha LF (`.gitattributes`) e `sh -n scripts/install.sh` limpo antes de commitar. Sem dependências além de `curl`/`wget` e `git`.
- Confere o SHA-256 **antes** de instalar. Só cria a unit systemd na primeira vez: quem editou o `-addr` depois do SSL não pode perder a mudança. Guarda o binário anterior em `brpanel.bak` ao atualizar.
- Qualquer mudança no `-addr`/`-data` padrão ou na unit em `deployments/systemd/` também vai para o heredoc da unit dentro do script (é uma cópia).
