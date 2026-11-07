---
name: BR Panel
description: Painel de servidor em Material 3 (Material Web), azul-petróleo, com um canvas de serviços ligados como linhas de mapa de metrô.
colors:
  primary: "#00687b"
  on-primary: "#ffffff"
  primary-container: "#aeecff"
  secondary-container: "#cee7ef"
  tertiary: "#575c7e"
  surface: "#f5fafc"
  surface-container-lowest: "#ffffff"
  surface-container-low: "#eff4f7"
  surface-container: "#e9eff1"
  surface-container-high: "#e4e9eb"
  surface-container-highest: "#dee3e6"
  on-surface: "#171c1e"
  on-surface-variant: "#3f484b"
  outline: "#70797c"
  outline-variant: "#bfc8cb"
  error: "#ba1a1a"
  error-container: "#ffdad6"
  success: "#006c53"
  success-container: "#81f8d0"
  warning: "#715d00"
  warning-container: "#ffe175"
  dark-primary: "#85d2e7"
  dark-surface: "#0f1416"
  dark-surface-container-low: "#171c1e"
  dark-surface-container-high: "#252b2d"
  dark-on-surface: "#dee3e6"
typography:
  display:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "32px"
    fontWeight: 400
    lineHeight: "40px"
  headline:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 400
    lineHeight: "28px"
  title:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 500
    lineHeight: "24px"
  body:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: "20px"
  label:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: "16px"
    letterSpacing: "0.5px"
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "13px"
    lineHeight: "20px"
rounded:
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "28px"
  full: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "56px"
components:
  button-filled:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.full}"
    height: "40px"
    padding: "0 24px"
  button-outlined:
    backgroundColor: "transparent"
    textColor: "{colors.primary}"
    rounded: "{rounded.full}"
    height: "40px"
    padding: "0 24px"
  button-text:
    backgroundColor: "transparent"
    textColor: "{colors.primary}"
    rounded: "{rounded.full}"
    height: "40px"
    padding: "0 12px"
  card:
    backgroundColor: "{colors.surface-container-low}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.lg}"
    padding: "20px"
  resource-card:
    backgroundColor: "{colors.surface-container-low}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.lg}"
    padding: "16px"
  input:
    backgroundColor: "transparent"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.md}"
    height: "56px"
    padding: "0 16px"
  chip:
    backgroundColor: "{colors.surface-container-highest}"
    textColor: "{colors.on-surface-variant}"
    rounded: "{rounded.sm}"
    height: "24px"
    padding: "0 10px"
  rail:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface-variant}"
    width: "80px"
---

# Design System: BR Panel

## Overview

**Creative North Star: "A Mesa de Controle"**

O painel é uma mesa de controle para um servidor: um canvas de pontos sutis onde cada aplicação e cada banco é um cartão, e as dependências entre eles são desenhadas como as linhas de um mapa de metrô, com estações nas pontas. É a assinatura do produto (a mesma ideia aparece na lista "usa postgres, redis" de cada cartão, que sobrevive no celular, onde as linhas somem). As demais telas são folhas de até 1040px com abas. Serve a quem só clica e a quem automatiza: o estado aparece de relance, o detalhe fica a um clique.

A base é Material 3 (Material Web 2.x) usado do jeito que o sistema manda: botões em pílula, campos contornados, abas, menus, diálogos, navegação em trilho no computador e barra inferior no celular. A identidade vem da cor (azul-petróleo, de uma semente única, com tema claro e escuro que segue o sistema), da tipografia Roboto em escala curta e da assinatura do canvas. Cor de estado (verde, âmbar, vermelho) só diz algo sobre o servidor.

Evitar: gradiente decorativo, brilho e sombra pesada em repouso, ícone colorido por enfeite, e qualquer controle que não seja o do Material 3.

**Key Characteristics:**
- Material 3 puro nos controles; identidade em cor, canvas e linhas de serviço.
- Tema claro e escuro por papéis de cor gerados (`tokens.css`); nenhuma cor solta no CSS.
- Cartões em tom (surface-container-low) com contorno de 1px; sombra só em resposta (hover, menu, diálogo).
- Trilho de navegação (≥840px) e barra inferior (<840px).
- Tudo servido pelo próprio binário: JS, Roboto, ícones; sem CDN.

## Colors

Paleta de uma cor: azul-petróleo como único acento sobre neutros azulados. Os papéis seguem o Material 3 (`--md-sys-color-*`), gerados por `scripts/vendor/theme.mjs` a partir da semente #00677a. Sucesso e atenção são cores personalizadas harmonizadas com a semente.

### Primary
- **Azul-petróleo** (#00687b claro / #85d2e7 escuro): botão principal, aba e item de navegação ativos, links, foco, barras de uso, linhas de serviço (misturadas com o contorno).
- **Contêiner petróleo** (#aeecff / #004e5d): ícone de template, resultado da seleção.
- **Contêiner secundário** (#cee7ef / #344a51): item ativo do trilho, passo concluído, menu selecionado.

### Neutral
- **Superfície** (#f5fafc / #0f1416): fundo da página e do trilho.
- **Contêiner mais baixo** (#ffffff / #090f11): fundo do canvas pontilhado e dos logs.
- **Contêiner baixo** (#eff4f7 / #171c1e): cartões, painel de atividade.
- **Contêiner** (#e9eff1 / #1b2022): campos de valor, chips de métrica, barra inferior.
- **Contêiner alto/mais alto** (#e4e9eb, #dee3e6 / #252b2d, #303638): menus, diálogos, chips.
- **Texto** (#171c1e / #dee3e6) e **Texto secundário** (#3f484b / #bfc8cb).
- **Contorno** (#70797c / #899295) e **Contorno suave** (#bfc8cb / #3f484b): campos e divisores de 1px.

### Semantic
- **Sucesso** (#006c53 / #63dbb5), **Atenção** (#715d00 / #e7c431), **Erro** (#ba1a1a / #ffb4ab) e seus contêineres: chips, banners, linhas de log, status dos cartões.

### Named Rules
**A Regra do Papel.** Toda cor sai de um papel (`--md-sys-color-*`, `--brp-*` ou um apelido em `app.css`). Nenhum hexadecimal solto, exceto as cores de marca dos selos de linguagem e banco, sempre misturadas ao neutro.

**A Regra da Cor com Significado.** Verde, âmbar e vermelho só dizem estado. O acento petróleo só marca ação e seleção.

## Typography

**Font:** Roboto variável (300–700), com system-ui. **Mono:** ui-monospace para logs, valores copiáveis, comandos e números em código. Ícones: Material Symbols Outlined (subconjunto).

**Character:** a sans do próprio Material, neutra e legível. Um só par de famílias; a hierarquia vem de tamanho e peso.

### Hierarchy
- **Display** (400, 32/40): título de página e de recurso.
- **Headline** (400, 22/28): título de passo do assistente.
- **Title** (500, 16/24): nome do cartão de recurso, rótulos de linha.
- **Body** (400, 14/20): texto corrido, formulários.
- **Label** (500, 12/16, 0.5px): rótulos do trilho, indicadores do host, chips; títulos de seção usam 14px/500 em petróleo.
- **Mono** (13/20): logs, `code`, valores de conexão.

### Named Rules
**A Regra do Literal.** O que o usuário copia (comando, URL, variável, log) vai em monoespaçada.
**A Regra do Número Alinhado.** Números de métrica usam `tabular-nums`.

## Layout

Barra superior de 64px com a marca, o título da página e as ações (tema, sair). Trilho de navegação de 80px à esquerda a partir de 840px; abaixo disso, barra inferior de 80px com os mesmos cinco destinos (Servidor, GitHub, IA, CLI, Ajustes). O conteúdo ocupa o resto: a tela Servidor usa a largura toda, em duas colunas (canvas + atividade de 340px, que desce para baixo do canvas abaixo de 1200px); as demais usam uma coluna centrada de até 1040px.

O canvas tem grade de pontos de 24px, cantos superior esquerdo de 28px e cartões em `auto-fill` com mínimo de 280px e folga de 56px por 64px para as linhas passarem. Ritmo interno de 16–20px; 32px antes de cada título de seção. Abaixo de 760px as linhas somem e o cartão passa a ser o único portador da dependência. Largura mínima de 360px.

## Elevation & Depth

Tonal e plano em repouso: a profundidade vem dos contêineres de superfície e do contorno de 1px, nunca de sombra. Sombra existe como resposta: hover dos cartões (duas camadas suaves), menu e diálogo (contêiner alto). Foco usa o anel do Material (2px).

### Named Rules
**A Regra do Plano em Repouso.** Cartão parado não tem sombra.

## Shapes

Escala do Material 3: 8px (chips, botões de ícone em toque), 12px (campos, logs, banners internos), 16px (cartões, banners), 28px (canvas, diálogo, login). Botões e o item ativo do trilho são pílulas. Selos de linguagem e banco são quadrados arredondados de 40px (28px no menu, 56px no cabeçalho). Linhas de serviço têm 2px, cantos arredondados de 14px e uma estação (círculo de 9px) em cada ponta.

## Components

### Buttons
Pílula de 40px do Material: **filled** (azul-petróleo) para a ação principal, **tonal** para a ação de apoio, **outlined** para ações secundárias e **text** para cancelar e voltar. Destrutivo é outlined em vermelho (`danger`), nunca preenchido. Ícone à esquerda quando ajuda.

### Cards / Containers
Contêiner baixo, contorno de 1px (contorno suave), raio 16px, padding 20px. Em painel destrutivo o contorno mistura vermelho.

### Resource Card
Selo de 40px (cor da marca da linguagem ou banco misturada ao neutro) + nome (16/24 500) + subtítulo; abaixo, status com ícone preenchido e texto na cor do estado, "usa …" em texto secundário, e chips de CPU e RAM. Hover mistura 55% de petróleo no contorno e levanta a sombra; leva ao recurso.

### Inputs / Fields
`md-outlined-text-field` e `md-outlined-select` de 56px, raio 12px, texto de apoio de 12px. Erro de validação aparece no campo. Caixas de seleção `md-checkbox` dentro de rótulo clicável de 48px.

### Navigation
Trilho de 80px com ícone dentro de pílula de 56×32 (secundário quando ativo, ícone preenchido) e rótulo de 12px; em telefone, barra inferior de 80px. Abas internas: `md-tabs` primárias, com indicador petróleo; abas de log são secundárias.

### Chips (status)
Altura 24px, raio 8px: ativo (sucesso), falhou (erro), implantando (atenção, com ponto pulsante), neutro.

### Menu e Diálogo
`md-menu` com foco e setas de teclado (Criar). `md-dialog` confirma toda ação destrutiva.

### Ajuda flutuante (assistente)
`md-fab` extensível (variante terciária, raio 16px) no canto inferior direito, acima da barra inferior no celular. Abre um painel lateral de 440px (contêiner baixo, raio 28px, elevação 3) ou, abaixo de 600px, uma folha de baixo para cima com alça. Cabeçalho com selo terciário, título e contexto da tela; `md-linear-progress` terciário enquanto o modelo trabalha; sugestões em `md-assist-chip`; mensagem do usuário em contêiner terciário e do assistente em contêiner mais alto; ação que altera o servidor vira um cartão de aprovação (atenção) com `md-text-button` "Recusar" e `md-filled-button` "Aprovar e executar"; campo de texto de 24px de raio com `md-filled-tonal-icon-button` para enviar.

### Linhas de serviço (assinatura)
Rotas ortogonais de 3 segmentos entre o cartão de origem e o de destino, com cantos arredondados e uma estação em cada ponta, desenhadas em SVG sob os cartões e redesenhadas ao redimensionar.

## Do's and Don'ts

### Do:
- **Do** usar componentes `md-*` e os parciais `field`, `area`, `check`, `btn`.
- **Do** tirar toda cor de um papel de `tokens.css`; conferir claro e escuro.
- **Do** dar estado por ícone, texto e cor; manter foco visível e `prefers-reduced-motion`.
- **Do** deixar sombra só para hover, menu e diálogo.
- **Do** escrever em português claro; erros dizem o que houve e o que fazer.
- **Do** conferir a tela a 360px e com a CSP `'self'`.

### Don't:
- **Don't** carregar nada de CDN nem afrouxar a CSP.
- **Don't** colocar gradiente decorativo, brilho ou sombra em elementos parados.
- **Don't** usar verde, âmbar ou vermelho como enfeite, nem o petróleo como fundo grande.
- **Don't** inventar controles fora do Material 3.
- **Don't** usar `style=`, `<script>` inline ou `onclick=`.
- **Don't** esconder a ação principal atrás de menus nem deixar o avançado fora de alcance.
