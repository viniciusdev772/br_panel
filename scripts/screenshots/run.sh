#!/usr/bin/env bash
# Starts a throw-away panel with simulated systemctl/nginx/certbot/journalctl, drives it with
# Playwright and writes screenshots of every screen (desktop + mobile) to the output directory.
#
#   make build && scripts/screenshots/run.sh [output-dir]
#
# Needs root (database users are created), bin/brpanel, node + the `playwright` module (with Chromium), python3, git, network access
# to github.com (two public repositories are cloned); PostgreSQL/MariaDB/Redis servers are used if installed. Charts use SYNTHETIC data (see seed.py).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
out="${1:-/tmp/brpanel-shots}"
work="$(mktemp -d)"
chmod 755 "$work" # service users (databases, apps) must be able to traverse down to the data directory
addr="127.0.0.1:${PORT:-18080}"
bin="$root/bin/brpanel"
[ -x "$bin" ] || { echo "compile primeiro: make build" >&2; exit 1; }
mkdir -p "$out" "$work"/{stubs,data,units,nginx,certs}

# --- simulated system commands (the sandbox has no systemd/nginx/certbot) ---
# systemctl is a small stand-in that really starts each unit's ExecStart (as the unit's User), so
# the databases and apps in the screenshots are live processes.
mkdir -p "$work/state"; chmod 777 "$work/state"
cp "$root/internal/dbengine/testdata/fake-systemctl" "$work/stubs/systemctl"
export FAKE_UNITS="$work/units" FAKE_STATE="$work/state"
cat > "$work/stubs/journalctl" <<'SH'
#!/bin/sh
cat <<'L'
2026-10-30T16:40:01+0000 srv systemd[1]: Started BR Panel app.
2026-10-30T16:40:02+0000 srv sh[4121]: Server listening on port 3001
2026-10-30T16:40:44+0000 srv sh[4121]: WARN slow query took 1.4s
2026-10-30T16:41:17+0000 srv sh[4121]: GET /health 200 2ms
2026-10-30T16:42:03+0000 srv sh[4121]: ERROR unhandled rejection: connection reset
L
SH
printf '#!/bin/sh\nexit 0\n' > "$work/stubs/nginx"; cp "$work/stubs/nginx" "$work/stubs/certbot"
chmod +x "$work/stubs"/*

cleanup() {
  [ -n "${pid:-}" ] && kill "$pid" 2>/dev/null || true
  # stop every service the fake systemctl started (each runs in its own process group)
  for f in "$work"/state/*.pid; do [ -f "$f" ] && kill -INT -- "-$(cat "$f")" 2>/dev/null || true; done
  sleep 1; rm -rf "$work"
}
trap cleanup EXIT

PATH="$work/stubs:$PATH" "$bin" -data "$work/data" -addr "$addr" > "$work/panel.log" 2>&1 &
pid=$!
for _ in $(seq 1 50); do [ -s "$work/data/setup_token" ] && break; sleep 0.1; done

PANEL_URL="http://$addr" WORK="$work" OUT="$out" SEED="$here/seed.py" SEED_DATA="$here/seed_data.py" \
  node "$here/shoot.js"
echo "Capturas em: $out"
ls -1 "$out"
