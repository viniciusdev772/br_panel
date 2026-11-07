# Assets de terceiros (servidos pelo próprio painel)

Nada aqui é baixado em tempo de execução. Os arquivos são commitados e embutidos no binário.

| Arquivo | Origem | Licença |
|---|---|---|
| `material-web.js` | `@material/web` 2.5.0 empacotado com esbuild por `scripts/vendor/build.sh` (componentes listados em `scripts/vendor/entry.js`) | Apache-2.0 (`LICENSE-material-web`) |
| `../tokens.css` | gerado por `scripts/vendor/theme.mjs` com `@material/material-color-utilities` a partir da cor-semente | Apache-2.0 |
| `fonts/roboto-latin*.woff2` | Roboto variável (300–700), subconjuntos latin e latin-ext, do Google Fonts | SIL OFL 1.1 |
| `fonts/material-symbols-outlined.woff2` | Material Symbols Outlined, subconjunto só com os ícones usados, do Google Fonts | Apache-2.0 |

## Atualizar o subconjunto de ícones

O arquivo de ícones contém apenas os nomes de `scripts/vendor/icons.txt`. Ao usar um ícone novo (`<md-icon>nome</md-icon>`, `.ms` no `app.js` ou `Icon:` em `internal/templates/catalog.go`), acrescente o nome lá (ordem alfabética) e rode `scripts/vendor/fonts.sh`, que baixa de novo o subconjunto e o Roboto.
