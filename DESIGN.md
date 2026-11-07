---
name: BR Panel
description: Painel web escuro e calmo para publicar projetos numa VPS, com cartões planos sobre um canvas pontilhado e um único acento violeta.
colors:
  bg: "#0e0d16"
  canvas: "#121120"
  surface: "#1a1828"
  surface-2: "#222035"
  border: "#2e2c44"
  text: "#f3f2f8"
  muted: "#9a97b0"
  accent: "#8b5cf6"
  accent-strong: "#7c3aed"
  link: "#b79cff"
  ok: "#34d399"
  warn: "#f5b942"
  bad: "#f87171"
  code-bg: "#0b0a12"
  brand-pink: "#ec4899"
typography:
  display:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "26px"
    fontWeight: 600
    lineHeight: 1.2
  headline:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 500
    lineHeight: 1.3
  title:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.3
  body:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.6
  label:
    fontFamily: "Roboto, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    letterSpacing: "0.8px"
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "13px"
    lineHeight: 1.55
rounded:
  sm: "4px"
  md: "8px"
  lg: "12px"
  xl: "14px"
  xxl: "16px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "56px"
components:
  button-primary:
    backgroundColor: "{colors.accent-strong}"
    textColor: "#ffffff"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 16px"
  button-outlined:
    backgroundColor: "transparent"
    textColor: "{colors.text}"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 16px"
  button-text:
    backgroundColor: "transparent"
    textColor: "{colors.link}"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 8px"
  button-danger:
    backgroundColor: "transparent"
    textColor: "{colors.bad}"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 16px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.lg}"
    padding: "16px"
  resource-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.xl}"
    padding: "16px 18px"
  input:
    backgroundColor: "transparent"
    textColor: "{colors.text}"
    rounded: "{rounded.md}"
    height: "56px"
    padding: "0 16px"
  chip:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.muted}"
    rounded: "12px"
    padding: "2px 10px"
  topbar:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    height: "56px"
    padding: "0 20px"
---

# Design System: BR Panel

## Overview

**Creative North Star: "A Mesa de Controle"**

Uma bancada escura e pontilhada onde cada serviço é um cartão que você enxerga e opera. A tela de arquitetura é um canvas de pontos sutis (22px) com cartões de recurso ligados por linhas tracejadas; as demais telas (aplicação, banco, configurações) são folhas centradas de 1040px com abas. O painel serve a quem só clica e a quem automatiza: a interface mostra o estado de relance e deixa o detalhe a um clique.

O tom é calmo e técnico. O tema escuro é o padrão (há tema claro por chave `data-theme`), as superfícies são planas com borda fina de 1px e o violeta é o único acento. Cor de estado (verde, âmbar, vermelho) só aparece para dizer algo sobre o servidor. Material Design 2 (MDC Web 14) é a base de comportamento; a aparência é reescrita por cima com variáveis CSS.

Evitar: o visual genérico de "dashboard SaaS", gradientes decorativos além da marca, sombras pesadas em repouso e excesso de cor de status.

**Key Characteristics:**
- Escuro por padrão, violeta como único acento, cor de estado só para estado.
- Cartões planos com borda de 1px; sombra e halo só em resposta (hover, foco, menu).
- Canvas pontilhado na tela de arquitetura; folhas de 1040px nas demais.
- Roboto para texto, monoespaçada para logs, valores e comandos; números tabulares.
- MDC Web 14 reescrito por variáveis; sem estilos inline (CSP).

## Colors

Paleta de uma cor com neutros arroxeados: o violeta aparece em menos de uma décima parte de qualquer tela, o resto é tom sobre tom.

### Primary
- **Violeta de Controle** (#8b5cf6): acento no tema escuro. Item ativo da navegação, borda de foco e hover, barras de uso, ícones de seção, passo atual do assistente. No tema claro vira #7c3aed.
- **Violeta Firme** (#7c3aed): fundo dos botões preenchidos (primary) com texto branco. No tema claro vira #6d28d9.
- **Violeta de Link** (#b79cff): links e botões de texto, mais claros para ler sobre fundo escuro. No tema claro vira #6d28d9.
- **Violeta Suave** (rgba(139, 92, 246, .16)): fundo de seleção, item ativo, mensagem do usuário no chat.

### Secondary
- **Rosa da Marca** (#ec4899): existe só no degradê do ícone da marca (135°, de violeta a rosa). Não aparece em mais nenhum lugar.

### Neutral
- **Noite Profunda** (#0e0d16): fundo da página.
- **Canvas** (#121120): fundo do canvas pontilhado da arquitetura.
- **Superfície** (#1a1828): cartões, barra superior, painéis, menus.
- **Superfície Elevada** (#222035): campos, chips, células de código, hover de linha.
- **Borda Violácea** (#2e2c44): toda borda e divisor de 1px.
- **Texto** (#f3f2f8): texto principal.
- **Texto Apagado** (#9a97b0): rótulos, legendas, metadados, ícones inativos.
- **Fundo de Código** (#0b0a12): logs, blocos de código, editor de texto.
- **Estado OK** (#34d399), **Atenção** (#f5b942), **Erro** (#f87171): fundo de 12% e borda de 35% da mesma cor para chips e avisos; texto na cor cheia.

### Named Rules
**A Regra do Acento Raro.** O violeta cobre no máximo uma décima parte da tela. Se tudo é violeta, nada indica o que está ativo.

**A Regra da Cor com Significado.** Verde, âmbar e vermelho existem só para estado do servidor ou da operação. Nunca decoram.

**A Regra das Variáveis.** Toda cor sai de variável CSS (`--bg`, `--surface`, `--accent`…). Nenhum valor solto em componente, exceto as cores de marca das linguagens e bancos (selos).

## Typography

**Display Font:** Roboto (com system-ui, sans-serif)
**Body Font:** Roboto
**Label/Mono Font:** ui-monospace, SFMono-Regular, Menlo

**Character:** Uma sans neutra e legível, sem personalidade própria, que deixa o conteúdo falar. A monoespaçada marca tudo que é literal: comando, caminho, valor, log.

### Hierarchy
- **Display** (600, 26px, 1.2): título da folha de recurso (nome da aplicação ou do banco).
- **Headline** (500, 20px, 1.3): título de cada passo do assistente de criação.
- **Title** (600, 16px): nome do recurso no cartão da arquitetura.
- **Body** (400, 14–16px, 1.6): texto corrido, formulários, navegação (14px). Linha de leitura até cerca de 70 caracteres.
- **Label** (500, 11–13px, caixa alta, 0.8–0.9px de espaçamento): títulos de seção e de painel, em Texto Apagado.
- **Mono** (400, 12–13px, 1.55): logs, valores de conexão, blocos de CLI, números em tabelas com `tabular-nums`.

### Named Rules
**A Regra do Literal.** Tudo que o usuário pode copiar (comando, URL, variável, log) vai em monoespaçada.

**A Regra do Número Alinhado.** Métricas, contagens e tempos usam `font-variant-numeric: tabular-nums`.

## Layout

Barra superior fixa de 56px (borda inferior de 1px, fundo translúcido com desfoque). Conteúdo em dois modos: a Arquitetura ocupa a largura toda em duas colunas (canvas à esquerda, atividade recente de 320px à direita), e as demais telas são uma coluna centrada de até 1040px com 16px de respiro lateral.

Os cartões do canvas ficam em grade responsiva (`auto-fill`, mínimo 270px) com 56px de espaço vertical e 64px horizontal, para dar lugar às linhas tracejadas de dependência. Dentro das folhas, o ritmo é de 16–18px entre blocos e 24–30px antes de cada título de seção. Formulários usam passos (assistente) quando há JavaScript e ficam inteiros na página sem ele.

Responsivo: abaixo de 1100px a atividade desce para baixo do canvas; abaixo de 760px a barra mostra só ícones, o breadcrumb some e as tabelas escondem colunas; abaixo de 480px o nome da marca some. Largura mínima de 360px.

### Named Rules
**A Regra da Folha Estreita.** Fora da arquitetura, nada passa de 1040px. Linhas longas cansam e escondem a ação.

## Elevation & Depth

Plano em repouso, tonal em camadas. A profundidade vem da diferença entre Noite Profunda, Superfície e Superfície Elevada, mais a borda de 1px. Sombra existe só como resposta: hover de cartão, menu aberto, cartão de login. O botão preenchido carrega um brilho violeta difuso e um degradê branco de 14% no topo, o único toque de volume em repouso.

### Shadow Vocabulary
- **Sombra de Resposta** (`box-shadow: 0 12px 32px rgba(0, 0, 0, .38)`; claro: `0 10px 28px rgba(40, 30, 80, .12)`): cartão em hover, menu, login.
- **Halo de Foco** (`box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 35%, transparent)`): foco de teclado em botão.
- **Brilho do Botão** (`0 6px 16px -8px rgba(124, 58, 237, .8)`): botão preenchido; sobe para `0 8px 20px -8px rgba(124, 58, 237, .9)` no hover.
- **Brilho da Marca** (`0 4px 14px -4px rgba(139, 92, 246, .7)`): só o ícone da marca.

### Named Rules
**A Regra do Plano em Repouso.** Cartões e painéis não têm sombra parados. Sombra é reação a hover, foco ou sobreposição.

## Shapes

Cantos arredondados em escala curta: 4px (código inline), 8px (botões, campos, abas, itens de menu), 12px (cartões e painéis), 14px (cartões de recurso), 16px (login e estado vazio), 12px de raio total em chips. Selos de linguagem e banco são quadrados de 36px com cantos de 9px. Bordas são sempre de 1px; o tracejado fica para linhas de dependência no canvas e para o estado vazio. Ícones são Material Icons, 18–20px.

## Components

### Buttons
- **Shape:** cantos de 8px (8px), altura de 36px do MDC, rótulo com espaçamento de 0.6px.
- **Primary (raised/unelevated):** fundo Violeta Firme (#7c3aed), texto branco, brilho violeta difuso; no hover `brightness(1.1)`; ao pressionar, desce 1px e encolhe a 98,5%.
- **Outlined:** borda de 1px (Borda Violácea), texto em Texto; no hover a borda vira violeta.
- **Text:** sem fundo, texto em Violeta de Link.
- **Danger:** contorno e texto em Erro (#f87171, borda a 50%).
- **Foco:** halo violeta de 3px; nunca remover o indicador.

### Cards / Containers
- **Corner Style:** 12px; cartões de recurso 14px.
- **Background:** Superfície (#1a1828).
- **Shadow Strategy:** nenhuma em repouso; sombra de resposta no hover (ver Elevation).
- **Border:** 1px Borda Violácea; no hover mistura 22% de violeta (cartão comum) ou 55% (cartão de recurso, que também sobe 2px).
- **Internal Padding:** 16px; cartões de recurso 16px 18px.

### Resource Card (cartão de recurso)
O elemento de assinatura. Selo colorido de 36px (cor da linguagem ou do banco) + nome em 16px/600 + subtítulo apagado em 12,5px; no rodapé, status com ícone de 18px em cor de estado, hora à direita e métricas de CPU/RAM em 12px. Altura mínima de 132px. Fica ligado a outros por linhas tracejadas de 1,4px em Texto Apagado a 70%.

### Inputs / Fields
- **Style:** MDC outlined, altura de 56px, contorno de 1px em Borda Violácea, rótulo flutuante em Texto Apagado, cursor violeta.
- **Focus:** contorno e rótulo viram violeta; dica abaixo em 12px apagada.
- **Error / Disabled:** erro vai ao aviso do topo, não ao campo; desabilitado a 60% de opacidade.
- **Campos simples** (filtros, selects, busca): fundo Superfície Elevada, borda de 1px, 8px de raio, 6px 8px de padding.

### Chips (status)
- **Style:** 12px de raio, 12px/500, 2px 10px; padrão em Superfície Elevada com texto apagado.
- **State:** `ativo/ok` verde, `falhou` vermelho, `implantando/ativando/provisionando` âmbar com ponto pulsante de 6px, `inativo` neutro. Cada estado: fundo a 12%, borda a 35%, texto na cor cheia.

### Navigation
Barra superior de 56px. Itens de 36px com ícone de 18px e rótulo de 14px; padrão em Texto Apagado, hover com fundo Superfície Elevada, ativo com fundo Violeta Suave e sublinhado de 2px violeta na base da barra. Abaixo de 760px só os ícones. Dentro das folhas, abas com borda inferior de 2px violeta na ativa, roláveis em tela estreita. Há um botão de tema (claro/escuro) e um de sair, ambos ícones de 36px.

### Avisos e bandas
Aviso: fundo Violeta Suave com borda violeta, 10px de raio; de erro, vermelho a 12%. Banda de atenção: âmbar a 8% com borda a 50%. Confirmações simples saem sozinhas após um tempo.

### Logs e código
Fundo de Código, borda de 1px, 10px de raio, monoespaçada 12px; linhas com erro em vermelho claro sobre vermelho a 10%, atenção em âmbar. Barra de ferramentas (linhas, filtro, acompanhar, baixar) acima.

## Do's and Don'ts

### Do:
- **Do** usar as variáveis (`--accent`, `--surface`, `--border`…) para toda cor e checar os dois temas.
- **Do** manter cartões planos com borda de 1px e reservar sombra para hover, foco e menus.
- **Do** dar estado por ícone mais texto mais cor, nunca só por cor.
- **Do** manter foco de teclado visível (contorno de 2px violeta ou halo de 3px) e respeitar `prefers-reduced-motion`.
- **Do** usar monoespaçada para tudo que o usuário copia, e `tabular-nums` em números.
- **Do** escrever em português claro; erros dizem o que houve e o que fazer.
- **Do** conferir a tela em 360px de largura e sem estilos inline (CSP).

### Don't:
- **Don't** migrar para Material 3 nem adicionar framework de UI; MDC Web 14 é a base.
- **Don't** usar o violeta como fundo grande nem em mais de uma décima parte da tela.
- **Don't** usar gradientes decorativos; o degradê violeta-rosa é só da marca.
- **Don't** usar verde, âmbar ou vermelho como enfeite.
- **Don't** colocar sombra pesada em elementos parados.
- **Don't** usar `style=`, `<script>` inline ou `onclick=`; a CSP bloqueia.
- **Don't** esconder a ação principal atrás de menus ou deixar o avançado fora de alcance para quem automatiza.
