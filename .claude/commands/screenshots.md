---
description: Sobe o painel com dados de demonstração e tira capturas de todas as telas
argument-hint: [pasta de saída, padrão /tmp/brpanel-shots]
allowed-tools: Bash, Read
---

Gere capturas das telas do painel seguindo `scripts/screenshots/README.md`:

1. `make build` e execute `scripts/screenshots/run.sh ${ARGUMENTS:-/tmp/brpanel-shots}`.
2. Abra algumas imagens com Read para conferir: título visível abaixo da barra, nada cortado, versão mobile legível.
3. Informe os caminhos completos dos arquivos gerados.

Lembre ao usuário que os gráficos de CPU/RAM nas capturas usam **dados sintéticos** e que `systemctl`, `nginx` e `certbot` são simulados.
