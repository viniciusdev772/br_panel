# Capturas de tela automáticas

`run.sh` sobe um painel descartável, conduz o navegador (Playwright) pelo fluxo completo e grava um PNG de cada tela
(desktop 1280 px e mobile 390 px).

```sh
make build
scripts/screenshots/run.sh /tmp/brpanel-shots
```

- `systemctl`, `journalctl`, `nginx` e `certbot` são **simulados** (o ambiente de capturas não precisa de systemd).
- Os gráficos de CPU/RAM usam **dados sintéticos** (`seed.py`).
- Dois repositórios públicos do GitHub são clonados de verdade (`octocat/Hello-World`, `heroku/node-js-getting-started`).
- Requer Node com o módulo `playwright` (Chromium instalado), `python3` e acesso à rede.
