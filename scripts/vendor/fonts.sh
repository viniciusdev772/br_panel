#!/usr/bin/env bash
# Downloads the self-hosted fonts into web/static/vendor/fonts: Roboto (latin, latin-ext) and a Material Symbols
# Outlined subset that holds only the icons listed in icons.txt (comma separated, alphabetical).
# Add the name there whenever a template or app.js uses a new icon, then run this script and commit the result.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$(cd "$here/../.." && pwd)/web/static/vendor/fonts"
ua='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
mkdir -p "$out"
icons="$(tr -d '\n' < "$here/icons.txt")"
sym="$(curl -fsS -A "$ua" "https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,300..500,0..1,0&icon_names=$icons" | grep -oE 'https://[^)]+' | head -1)"
curl -fsS -A "$ua" "$sym" -o "$out/material-symbols-outlined.woff2"
css="$(curl -fsS -A "$ua" "https://fonts.googleapis.com/css2?family=Roboto:wght@300..700&display=swap")"
for subset in latin latin-ext; do
  url="$(printf '%s\n' "$css" | awk -v s="/* $subset */" '$0 == s {f=1} f && /src:/ {print; exit}' | grep -oE 'https://[^)]+')"
  curl -fsS -A "$ua" "$url" -o "$out/roboto-$subset.woff2"
done
