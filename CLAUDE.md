# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build -o brpanel .            # always pass -o: a bare `go build` drops a binary named br_panel in the repo root
go vet ./... && go test ./...    # CI-equivalent checks; tests need no root, systemd, nginx or network
go test -run TestDetectNode ./...  # single test (all code is one package, `main`)
./brpanel -data ./data -addr 127.0.0.1:8080   # run locally; the first run prints the setup token
```

Needs Go 1.26+ (set by the `modernc.org/sqlite` dependency; pure Go, no CGO). UI strings and log messages are in Portuguese (pt-BR); keep new ones consistent.

## Architecture

A single Go `package main` (stdlib `net/http` + `html/template`, MDC Web 14 / Material Design 2 from cdnjs) that manages apps as **systemd services**, not containers. The only startup inputs are the `-addr` and `-data` flags; **there are no environment variables** — everything else is stored in SQLite (`<data>/brpanel.db`, 0600).

- **Bootstrap / first run** (`main.go`, `setup.go`): until an admin hash exists in `settings`, the `gate` middleware redirects everything to `/setup`, which requires a one-time token printed to the log and written to `<data>/setup_token`. `Server.settings()` returns a snapshot of the live `Settings`; `applySettings` persists and swaps it (and rebuilds the GitHub App client). Read settings through it — never cache them.
- **Storage** (`store.go`, `settings.go`): one SQLite connection (`SetMaxOpenConns(1)`), schema via the `migrations` slice tracked in `PRAGMA user_version` — append a new entry, never edit old ones. Settings are a key/value table mapped by `Settings.fields()`. `UpdateConfig` deliberately does not touch deploy status (it races with running deploys); `BeginDeploy` is an atomic compare-and-set.
- **Deploy pipeline** (`deploy.go`, `autoconfig.go`, `detect.go`): `startDeploy` runs async and writes `<data>/logs/<app>.log`. Order: git clone/fetch → `autoConfigure` (only when the start command is empty; detected build/start/port are persisted back to the app) → build via `sh -c` → write env file + unit → `systemctl restart` → Nginx vhost/SSL. Git and build commands run as the configured app user through `runuser`. Apps always receive `PORT` and `HOST` (127.0.0.1 with a domain, else 0.0.0.0); detection output must use `$PORT`/`$HOST`. A push arriving mid-deploy sets a "pending" flag that triggers one more deploy afterwards.
- **Nginx + SSL** (`nginx.go`): `ensureSite` writes `brpanel-<key>.conf`, validates with `nginx -t` (rolls the file back on failure), reloads, then gets a Let's Encrypt cert with `certbot certonly --webroot` and rewrites the vhost for TLS. Cert failures are non-fatal by design. The panel itself is published the same way under key `_panel`.
- **GitHub** (`github.go`): `POST /webhooks/github` (HMAC-verified, outside session auth) triggers deploys for apps with `AutoDeploy` matching repo slug + branch. If a GitHub App is configured, `gitAuthEnv` mints a 1-hour repo-scoped token and passes it to **git only** via `GIT_CONFIG_*` env (never into the build env or app env, never in argv/logs).
- **Monitoring** (`metrics.go`, `monitor.go`): a sampler goroutine reads cgroup v2 files under `/sys/fs/cgroup/system.slice/brpanel-<app>.service` every 15 s into the `metrics` table (24 h retention); `/apps/{name}/metrics` and `/api/overview` feed the canvas charts in `web/static/app.js`. Host stats come from `/proc` (`procRoot` is a var so tests can fake it).
- **Auth** (`auth.go`): in-memory sessions + CSRF token (checked in `protected()` for every POST), per-IP lockout, Argon2id password hashes. `clientIP`/`isHTTPS` trust `X-Real-IP`/`X-Forwarded-Proto` only from loopback (our Nginx).
- **Frontend**: templates in `web/templates` (each page = `layout.html` + one file, registered in `loadTemplates`; shared `field`/`area` partials live in `layout.html`), static assets embedded with `go:embed`. The CSP forbids inline scripts and `style=` attributes — put behavior in `app.js` and styles in `app.css`.

## Conventions that matter

- Anything that reaches a shell, systemd unit or Nginx file must pass the validators in `validate.go`/`settings.go` first; external programs are called with separate argv, never via string-built shell, except the admin-defined build/start commands (`sh -c`).
- Tests stub the outside world instead of mocking: `httptest` for the GitHub API, temp dirs for cgroups/`/proc`, `newTestStore(t)` for SQLite. Detection tests use the `project(t, files)` helper.
- Edits to `web/` need a rebuild to show up (embedded assets).
