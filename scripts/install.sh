#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOME_DIR="${AI_DISPATCH_HOME:-${HOME}/.ai-dispatch}"
BIN_DIR="$HOME_DIR/bin"
BIN="$BIN_DIR/ai-dispatch"
CACHE_BASE="${AI_DISPATCH_CACHE_DIR:-$HOME_DIR/cache}"
CACHE_DIR="${AI_DISPATCH_GO_CACHE_DIR:-$CACHE_BASE/go-build}"
GO_BIN="$CACHE_DIR/ai-dispatch-go"
CLAUDE_TRANSPORT="${AI_DISPATCH_CLAUDE_TRANSPORT:-print}"
SKILL_TARGET="${AI_DISPATCH_SKILL_TARGET:-all}"

mkdir -p "$BIN_DIR" "$CACHE_DIR" "$HOME_DIR/runs" "$HOME_DIR/logs"

(
  cd "$ROOT"
  tmp_bin="$GO_BIN.tmp.$$"
  go build -o "$tmp_bin" ./cmd/ai-dispatch
  mv "$tmp_bin" "$GO_BIN"
)

cat >"$BIN" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

HOME_DIR="${AI_DISPATCH_HOME:-${HOME}/.ai-dispatch}"
CACHE_DIR="${AI_DISPATCH_GO_CACHE_DIR:-${AI_DISPATCH_CACHE_DIR:-$HOME_DIR/cache}/go-build}"
CACHE_BIN="$CACHE_DIR/ai-dispatch-go"

if [[ ! -x "$CACHE_BIN" ]]; then
  echo "ai-dispatch runtime missing: $CACHE_BIN" >&2
  echo "reinstall via scripts/install.sh" >&2
  exit 1
fi

export AI_DISPATCH_GO_PROVIDER_EXECUTION="${AI_DISPATCH_GO_PROVIDER_EXECUTION:-on}"
export AI_DISPATCH_ROOT="${AI_DISPATCH_ROOT:-$HOME_DIR}"
exec "$CACHE_BIN" "$@"
EOF
chmod +x "$BIN"

"$BIN" init --claude-transport "$CLAUDE_TRANSPORT"
"$BIN" skill install --target "$SKILL_TARGET"

printf 'installed %s\n' "$BIN"
