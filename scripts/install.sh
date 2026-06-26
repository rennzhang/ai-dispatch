#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOME_DIR="${AI_DISPATCH_HOME:-${HOME}/.ai-dispatch}"
BIN_DIR="$HOME_DIR/bin"
BIN="$BIN_DIR/ai-dispatch"
CLAUDE_TRANSPORT="${AI_DISPATCH_CLAUDE_TRANSPORT:-print}"
SKILL_TARGET="${AI_DISPATCH_SKILL_TARGET:-all}"

mkdir -p "$BIN_DIR" "$HOME_DIR/cache" "$HOME_DIR/runs" "$HOME_DIR/logs"

(
  cd "$ROOT"
  go build -o "$BIN" ./cmd/ai-dispatch
)

"$BIN" init --claude-transport "$CLAUDE_TRANSPORT"
"$BIN" skill install --target "$SKILL_TARGET"

printf 'installed %s\n' "$BIN"
