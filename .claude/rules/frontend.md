---
paths:
  - "web/**"
---

# Frontend (Material 3 / Material Web)

- Biblioteca: **Material Web 2.x** (Material 3), empacotada em `web/static/vendor/material-web.js`. Nada vem de CDN: JS, CSS, fontes (Roboto) e ícones (Material Symbols, subconjunto) ficam em `web/static/` e são embutidos no binário. A CSP é `default-src 'self'` e não deve ser afrouxada.
- Para trocar de versão, adicionar um componente ou mudar a cor-semente, edite `scripts/vendor/` (`entry.js`, `theme.mjs`) e rode `scripts/vendor/build.sh`; commite o resultado. Ícone novo: acrescente o nome em `scripts/vendor/icons.txt` (ordem alfabética) e rode `scripts/vendor/fonts.sh`. Não adicione outros frameworks de UI.
- Cores vêm de `web/static/tokens.css` (gerado; não edite à mão). Em `app.css` use os papéis `--md-sys-color-*` (ou os apelidos `--text`, `--muted`, `--line`, `--accent`, `--low`…), nunca um hexadecimal solto. O tema claro/escuro segue o sistema; `data-theme` no `<html>` força um deles.
- A CSP proíbe `<script>` inline, handlers `onclick=` e atributos `style=`. Comportamento vai em `web/static/app.js`, estilo em `web/static/app.css` (estilos aplicados por JS via `element.style` são permitidos).
- Controles são componentes `md-*`. Use os parciais de `layout.html`: `field`/`area` (campos), `check` (caixa de seleção), `btn` (botões: `filled`, `tonal`, `outlined`, `text`; `danger` para destrutivo), `logtools`. Ícone: `<md-icon>nome</md-icon>`.
- Cada página = `layout.html` + um arquivo em `web/templates/`, registrada em `loadTemplates` (`internal/server`). Textos em português (pt-BR). Todo formulário POST inclui `<input type="hidden" name="csrf" value="{{.CSRF}}">`. Ação destrutiva usa `data-confirm` no formulário (abre o `md-dialog`).
- Largura mínima 360 px: abaixo de 840 px a navegação vira barra inferior; confira a tela no celular.
- Depois de editar, recompile (`go build -o bin/brpanel ./cmd/brpanel`): os arquivos são embutidos. Para conferir visualmente, use `make screenshots` ou suba o painel local e abra no navegador.
