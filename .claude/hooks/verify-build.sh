#!/usr/bin/env bash
# Stop: before Claude finishes, make sure the tree still compiles and passes go vet.
# Exit code 2 sends the output back to Claude; stop_hook_active prevents endless loops.
set -u
EVENT="$(cat)"
. "$(dirname "$0")/_lib.sh"
[ "$(json_get stop_hook_active)" = "True" ] && exit 0
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
command -v go >/dev/null 2>&1 || exit 0
# Only check when Go files changed relative to HEAD.
if [ -z "$(git status --porcelain -- '*.go' go.mod 2>/dev/null)" ]; then exit 0; fi
if ! out="$(go build ./... 2>&1 && go vet ./... 2>&1)"; then
  echo "go build/vet falhou — corrija antes de concluir:" >&2
  echo "$out" | head -40 >&2
  exit 2
fi
exit 0
