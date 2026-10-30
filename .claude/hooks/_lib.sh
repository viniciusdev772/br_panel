#!/usr/bin/env bash
# Shared helpers for the hooks. Hooks receive a JSON event on stdin.

# json_get <key-path>: reads a dotted path (e.g. tool_input.file_path) from $EVENT.
json_get() {
  python3 -c '
import json, sys
try:
    v = json.loads(sys.argv[1])
    for k in sys.argv[2].split("."):
        v = v.get(k) if isinstance(v, dict) else None
    print("" if v is None else v)
except Exception:
    print("")
' "$EVENT" "$1"
}
