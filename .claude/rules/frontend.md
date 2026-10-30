---
paths:
  - "web/**"
---

# Frontend (Material Design 2)

- Biblioteca: **MDC Web 14** (Material Design 2) via cdnjs, versão fixa. Não migre para Material 3 nem adicione frameworks.
- A CSP proíbe `<script>` inline, handlers `onclick=` e atributos `style=`. Comportamento vai em `web/static/app.js`, estilo em `web/static/app.css` (estilos aplicados por JS via `element.style` são permitidos).
- Cada página = `layout.html` + um arquivo em `web/templates/`, registrada em `loadTemplates` (`internal/server`). Parciais `field`/`area` ficam em `layout.html`.
- Textos em português (pt-BR). Todo formulário POST inclui `<input type="hidden" name="csrf" value="{{.CSRF}}">`.
- Largura mínima 360 px: confira a tela no celular (abaixo de 600 px a barra mostra só ícones e a tabela esconde colunas).
- Depois de editar, recompile (`go build ./cmd/brpanel`): os arquivos são embutidos. Use `/screenshots` para conferir visualmente.
