#!/usr/bin/env bash
# Rebuilds the vendored front-end assets (no CDN at runtime):
#   web/static/vendor/material-web.js  Material Web 2.x (Material 3) bundle
#   web/static/tokens.css              Material 3 color roles generated from the seed in theme.mjs
# The fonts in web/static/vendor/fonts/ are Roboto (latin, latin-ext) and a Material Symbols Outlined
# subset downloaded from Google Fonts (SIL OFL); see web/static/vendor/README.md to refresh the subset.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
cp "$here"/{package.json,entry.js,theme.mjs} "$work"/
cd "$work" && npm install --silent
npx esbuild entry.js --bundle --minify --format=esm --target=es2022 --legal-comments=none --outfile="$root/web/static/vendor/material-web.js"
npx esbuild theme.mjs --bundle --platform=node --format=esm --outfile=theme.bundle.mjs --log-level=error
node theme.bundle.mjs > "$root/web/static/tokens.css"
