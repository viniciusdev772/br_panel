#!/usr/bin/env bash
# PreToolUse(Edit|Write): refuse edits to files that must never be hand-written
# (exit code 2 blocks the call and shows stderr to Claude).
set -u
EVENT="$(cat)"
. "$(dirname "$0")/_lib.sh"
file="$(json_get tool_input.file_path)"
case "$file" in
  */go.sum|*/data/*|*.db|*.db-wal|*.db-shm|*.pem)
    echo "Bloqueado: $file é gerado ou sensível. Use 'go mod tidy' para go.sum; nunca grave dados/segredos à mão." >&2
    exit 2 ;;
esac
exit 0
