# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Dois perfis, ambos brasileiros e donos de uma VPS:

- **Quem não é especialista em servidor** (dev autônomo, pequena agência, dono de projeto): quer colocar um site ou sistema no ar sem decorar comandos, usando só a tela web em português.
- **Quem é avançado**: quer evitar Nginx/systemd/Let's Encrypt manuais e opera o painel também pela CLI (`brpanel`), pela API com token e pelo assistente de IA.

A interface precisa servir aos dois: simples para quem só clica, sem esconder o controle de quem automatiza.

## Product Purpose

Implantar repositórios Git como serviços do sistema (systemd) numa VPS, com endereço próprio e HTTPS, atualização automática a cada push no GitHub, monitoramento de CPU e memória, bancos de dados, templates de um clique (n8n, Uptime Kuma, Gitea), cópias de segurança, réplicas e autoscale. Sucesso: o usuário aponta para o código, publica, e o projeto fica no ar e se mantém sozinho.

## Positioning

Dois pontos que um vizinho (Coolify, CapRover, Dokploy) não copia com verdade, juntos:

- **Leve e sem Docker**: um binário Go único, apps como serviços systemd nativos, configuração só no SQLite (sem variáveis de ambiente), tudo em português.
- **Operável por IA e CLI**: assistente agêntico e CLI cujo contrato de saída serve até a modelos fracos, sobre as mesmas rotas e validações da interface web.

## Operating Context

O painel roda no próprio servidor do usuário, atrás de Nginx. Primeiro acesso por um assistente (`/setup`) protegido por token de uso único impresso no log. Uso diário: ver aplicações e bancos, acompanhar deploys e gráficos de 24 h, editar configurações (`/settings`), instalar templates. Fora da tela, via CLI no socket Unix local ou com token remoto.

## Capabilities and Constraints

- Interface e mensagens em pt-BR; código e comentários em inglês.
- Frontend: Material Web 2.x (Material 3), servido do próprio binário, sem CDN; a CSP proíbe scripts inline e atributos `style=`. Páginas = `layout.html` + um template; assets embutidos no binário.
- Gráficos são canvas em `web/static/app.js`.
- Toda mudança de estado é POST com proteção CSRF; a UI e a CLI compartilham os mesmos handlers.
- Sem Docker, sem variáveis de ambiente; só as flags `-addr` e `-data`.

## Brand Commitments

Nome "BR Panel". Voz em português simples, voltada a quem não é especialista ("sem precisar decorar comandos"). Nenhuma identidade visual, logotipo ou paleta foi declarada como vinculante.

## Evidence on Hand

- README, `docs/` (arquitetura, segurança, ADRs) e capturas de tela em `docs/img/` (geradas por `scripts/screenshots/`).
- Interface existente em `web/templates/` e `web/static/`.
- Não há depoimentos, clientes, números de uso nem benchmarks para citar; não invente.

## Product Principles

- Quem não é especialista deve concluir a tarefa sem ler documentação; quem é avançado nunca deve ser impedido de ver ou controlar o detalhe.
- A tela, a CLI e a IA são três portas para as mesmas ações: nada existe só em uma delas.
- Leveza é promessa de produto: poucas dependências, sem Docker, nada de peso desnecessário na interface.
- Linguagem clara em português; erros dizem o que aconteceu e o que fazer.
- Ações que alteram o servidor são explícitas e reversíveis ou confirmadas.

## Accessibility & Inclusion

Sem padrão formal exigido. Boas práticas básicas (contraste, foco visível, teclado) valem como piso.
