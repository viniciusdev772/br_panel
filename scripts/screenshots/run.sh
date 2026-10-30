#!/usr/bin/env bash
# Starts a throw-away panel with simulated systemctl/nginx/certbot/journalctl, drives it with
# Playwright and writes screenshots of every screen (desktop + mobile) to the output directory.
#
#   make build && scripts/screenshots/run.sh [output-dir]
#
# Needs: bin/brpanel, node + the `playwright` module (with Chromium), python3, git, network access
# to github.com (two public repositories are cloned). Charts use SYNTHETIC data (see seed.py).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
out="${1:-/tmp/brpanel-shots}"
work="$(mktemp -d)"
addr="127.0.0.1:${PORT:-18080}"
bin="$root/bin/brpanel"
[ -x "$bin" ] || { echo "compile primeiro: make build" >&2; exit 1; }
mkdir -p "$out" "$work"/{stubs,data,units,nginx,certs}

# --- simulated system commands (the sandbox has no systemd/nginx/certbot) ---
cat > "$work/stubs/systemctl" <<'SH'
#!/bin/sh
case "$1" in
  is-active) echo active;;
  show) UP=$(cut -d. -f1 /proc/uptime); M=$(( (UP - 7500) * 1000000 )); [ $M -lt 1000000 ] && M=1000000
        printf 'ActiveState=active\nSubState=running\nNRestarts=1\nMainPID=4121\nActiveEnterTimestampMonotonic=%s\n' "$M";;
esac
exit 0
SH
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

cleanup() { [ -n "${pid:-}" ] && kill "$pid" 2>/dev/null || true; rm -rf "$work"; }
trap cleanup EXIT

PATH="$work/stubs:$PATH" "$bin" -data "$work/data" -addr "$addr" > "$work/panel.log" 2>&1 &
pid=$!
for _ in $(seq 1 50); do [ -s "$work/data/setup_token" ] && break; sleep 0.1; done

PANEL_URL="http://$addr" WORK="$work" OUT="$out" SEED="$here/seed.py" \
  node "$here/shoot.js"
echo "Capturas em: $out"
ls -1 "$out"
