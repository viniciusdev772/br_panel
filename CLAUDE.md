# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

BR Panel: a lightweight Go web panel that deploys Git repositories as **systemd services** (no Docker), with Nginx + Let's Encrypt, GitHub App auto-deploy, CPU/RAM monitoring and project auto-detection. UI and user-facing messages are Portuguese (pt-BR); code and comments are English.

## Commands

```sh
make check                       # gofmt check + go vet + go test -race (run before every commit/PR; there is no CI)
make build                       # -> bin/brpanel (always `-o`: a bare `go build` once committed a 12 MB binary)
make run                         # build + run with ./data on 127.0.0.1:8080; first run prints the setup token
go test -run TestDetectNode ./internal/detect   # single test
make help                        # all targets (cover, vuln, screenshots, …)
```

Needs Go 1.26+ (pure Go, no CGO, `modernc.org/sqlite`). `make cover` only works on a complete Go toolchain (it needs `covdata`).

## Layout

`cmd/brpanel` (flags, wiring) → `internal/*` (all logic) → `web/` (embedded templates/static) · `deployments/systemd/` · `docs/` (architecture, security, ADRs) · `scripts/screenshots/` · `.claude/` (rules).

Package dependencies flow one way and are listed in `.claude/rules/architecture.md` (also `docs/architecture.md` with flows and the route table). Key idea: `internal/server` is the only HTTP layer; domain logic lives in `deploy`, `dbengine`, `detect`, `nginx`, `systemd`, `monitor`, `githubapp`, `settings`, `store`.

## Things that need several files to understand

- **No environment variables.** Only the `-addr`/`-data` flags exist; every other setting lives in SQLite (`settings` table) and is edited in the first-run wizard (`/setup`, gated by a one-time token) or `/settings`. `Server.settings()` returns a snapshot; `applySettings` persists and swaps it. Read settings through the snapshot, never cache them.
- **Deploy pipeline** (`internal/deploy`): `Deployer.Start` → git clone/fetch → `autoConfigure` (only when the start command is empty; detected commands/port are saved back to the app) → build via `sh -c` → env file + systemd unit → Nginx vhost/SSL. Apps always get `PORT` and `HOST`; detection output must use `$PORT`/`$HOST`. With `Replicas > 1` each instance is its own generated unit (`systemd.ReplicaUnitName`) listening on `Port+k`, restarted one at a time, and nginx gets an upstream group (`nginx.Site.Upstreams`); resource limits (`MemoryMB`, `CPUPercent`) go into each unit and `RateLimit`/gzip into the vhost. `internal/autoscale` (target tracking modeled on AWS Application Auto Scaling: 3-minute scale-out, 15-minute scale-in, cooldowns, warmup) reads the 1-minute metrics and calls `Deployer.Scale`, which changes the instance count without a deploy; an app reserves `store.App.Slots()` ports (the autoscale maximum when enabled). A push during a deploy sets a "pending" flag for one more run. **Build cache**: every build gets the shared `<data>/cache` through env vars (`internal/buildcache`; npm, pnpm, yarn, bun, pip, Go, Cargo, Composer); `deploy/depscache.go` skips a leading `npm ci`/`pnpm install --frozen-lockfile`/… when the lockfile fingerprint (`apps/<app>/deps.sha256`) is unchanged; `StartClean` (`apps deploy --clean`) wipes `src` and the app-private caches; `cache prune|clear` refuse while a deploy runs.
- **GitHub token isolation**: `githubapp.AuthEnv` mints a 1-hour, repo-scoped token and gives it to **git only** through `GIT_CONFIG_*` env — never to the build/app environment, argv or logs. The webhook (`/webhooks/github`) is outside session auth but HMAC-verified before any effect.
- **Databases** (`internal/dbengine`): each database is its own systemd unit (`brpaneldb-<name>`), provisioned by the `Manager` through a per-kind `engine` interface (postgres, mysql/mariadb, mongodb, redis). Apps and databases share one name space; apps reference `${{db.VAR}}` in their env (resolved at deploy by `dbengine.Resolve`). Secrets never go in argv. `internal/dbengine/integration_test.go` runs against real servers using `testdata/fake-systemctl` (skipped without root/binaries). Metrics key for a database is `db:<name>`.
- **Templates** (`internal/templates`): one-click apps (n8n, Uptime Kuma, Gitea), no Docker. A template is a preset for a normal app, not a new app type: `Template.Instantiate` → `Plan`; `server.handleTemplateInstall` validates like the app form, creates the app + `volume` mounts and starts a deploy. Files-based templates use `Repo = "template:<id>"` and `deploy.writeTemplateFiles` replaces the `git clone`; Git-based ones use the version as branch/tag (`templates.ForRepo` finds the template by repo, no DB column). `ensureRequires` installs Node via nvm when needed. The version reaches the build as `$TPL_VERSION` (validated: it goes into a shell command). New entries go in `catalog.go`, must be one-line commands (longer logic in `Files` scripts) and be tried on a real server; downloaded binaries need a published SHA-256 checked in the script.
- **CLI** (`internal/cli`): `brpanel <word> …` is a client, not the server. It sends the web UI's own form posts over the root-only Unix socket `<data>/brpanel.sock`; `Server.cliHandler` marks those requests (`viaCLI`) and `auth.Grant` gives each a throw-away session, so every state change still goes through the normal `protected()` handlers and validation. Results come from the redirect's `?msg=`; `render` answers JSON `{error, flash}` for CLI requests. Reads use `/api/apps*` and `/api/databases` (types in `cli/wire.go`). Remote use: `Authorization: Bearer brp_…` on the normal listener is handled by `cliAuth` (`server/tokens.go`, hashes in the `api_tokens` table, failures feed the login lockout); `brpanel login` stores `{server, token}` in the user's config dir and then `bearerRT` replaces the socket. `cmd/brpanel-cli` is the CLI-only binary for Windows/macOS/Linux clients, installed by `scripts/install-cli.{sh,ps1}`; release assets `brpanel-cli-<os>-<arch>` must stay in sync with them. `cli.Catalog` is the single source for `--help`, `brpanel guide`, the `/cli` page and `/cli/guide.txt`; a test checks handlers and examples against it. Output contract for weak models: `OK:`/`ERRO:` + `DICA:`/`USO:`/`EXEMPLO:`, exit 0/1/2.
- **Store**: one SQLite connection; schema changes only by appending to `migrations` (tracked by `PRAGMA user_version`). `UpdateConfig` deliberately leaves deploy status alone; `BeginDeploy` is an atomic compare-and-set.
- **Monitoring**: `monitor.Run` samples cgroup v2 files of `brpanel-<app>.service` every 15 s into `metrics` (24 h); charts are canvas code in `web/static/app.js`. `sysinfo.ProcRoot` is a variable so tests can fake `/proc`.
- **Frontend**: MDC Web 14 (Material Design 2) only. The CSP forbids inline scripts and `style=` attributes. Pages = `layout.html` + one template registered in `loadTemplates`. Assets are embedded, so rebuild after editing `web/`.

## Project rules (loaded automatically from `.claude/rules/`)

Architecture & dependency direction, Go style, security, testing, database, frontend and git workflow each have a rule file; path-scoped ones load when you touch matching files. Highlights: validate everything that reaches a shell/unit/Nginx file (`internal/validate`); every state-changing route is POST + `protected()` (CSRF) with a test; new settings are never env vars; never commit binaries, databases or keys.

## Claude Code setup in this repo

Only rules (`.claude/rules/`). No custom agents, skills, slash commands, hooks or `settings.json` — do not add any. Personal overrides go in `.claude/settings.local.json` / `CLAUDE.local.md` (git-ignored).
