#!/usr/bin/env bash
# SessionStart: warm the Go module cache (ephemeral cloud containers start empty) and
# tell Claude which toolchain it is working with. Output on stdout becomes context.
set -u
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
if command -v go >/dev/null 2>&1; then
  go mod download >/dev/null 2>&1 || true
  echo "Go toolchain: $(go version | awk '{print $3}') — this repo requires $(awk '/^go /{print $2}' go.mod)+. See CLAUDE.md and .claude/rules/."
fi
exit 0
