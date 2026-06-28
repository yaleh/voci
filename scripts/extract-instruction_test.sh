#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(dirname "$0")/extract-instruction.py"

fail() { echo "FAIL: $1"; exit 1; }

# Case 1: serve-emitted JSON line with 'rewritten' field → extract it
got=$(printf '%s' '{"rewritten":"do X","kind":"direct_prompt"}' | python3 "$SCRIPT")
[ "$got" = "do X" ] || fail "case 1: expected 'do X', got '$got'"

# Case 2: non-JSON line → raw fallback
got=$(printf '%s' 'hello world' | python3 "$SCRIPT")
[ "$got" = "hello world" ] || fail "case 2: expected 'hello world', got '$got'"

# Case 3: JSON without 'rewritten' field → raw fallback (full line)
got=$(printf '%s' '{"kind":"direct_prompt"}' | python3 "$SCRIPT")
[ "$got" = '{"kind":"direct_prompt"}' ] || fail "case 3: expected raw fallback, got '$got'"

echo "OK: all extractInstruction cases pass"
