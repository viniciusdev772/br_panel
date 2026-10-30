#!/usr/bin/env bash
# PostToolUse(Edit|Write): keep Go files gofmt-clean so diffs stay small.
set -u
EVENT="$(cat)"
. "$(dirname "$0")/_lib.sh"
file="$(json_get tool_input.file_path)"
case "$file" in
  *.go) [ -f "$file" ] && gofmt -w "$file" 2>/dev/null ;;
esac
exit 0
