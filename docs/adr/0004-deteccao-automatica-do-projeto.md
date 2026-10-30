# ADR 0004 — Detecção automática de build e início

- **Status:** aceita
- **Contexto:** pedir build, início e porta a cada app é a maior fonte de atrito.
- **Decisão:** `internal/detect` inspeciona o checkout (package.json e lockfiles, go.mod, Cargo.toml, requirements/pyproject, Gemfile, composer.json, index.html). `Procfile` (`web:`) e `brpanel.json` sobrescrevem. Com o comando de início vazio, a detecção roda no deploy e o resultado é gravado na aplicação. Os comandos usam `$PORT`/`$HOST`; a porta é escolhida automaticamente.
- **Consequências:** + criar uma app exige só o repositório; + o botão "Detectar automaticamente" mostra o resultado antes de criar. − heurísticas podem errar (por isso o resultado é editável e há `brpanel.json`); − a detecção roda uma vez, depois os comandos ficam fixos; − texto vindo do repositório entra em comandos, então exige `safeRel` e regex estritas.
