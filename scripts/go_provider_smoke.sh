#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on

run_smoke() {
  local name="$1"
  shift
  echo "[provider-smoke] $name"
  "$@"
}

run_smoke "codex gpt5.5" \
  go run ./cmd/ai-dispatch send gpt5.5 "Reply exactly: OK" --json-result --timeout 120 --activity-timeout 60

run_smoke "opencode mimo" \
  go run ./cmd/ai-dispatch send mimo "Reply exactly: OK" --json-result --timeout 120 --activity-timeout 60

echo "[provider-smoke] gemini aliases route through the Go Antigravity provider; use go_agy_stress.sh for explicit Antigravity/Gemini stress."

run_smoke "opencode output file"
tmp="$(mktemp -t ai-dispatch-output.XXXXXX.md)"
go run ./cmd/ai-dispatch send mimo "Reply exactly: OK" --json-result --output-file "$tmp" --timeout 120 --activity-timeout 60
test -s "$tmp"
rm -f "$tmp"

run_smoke "opencode stream progress" \
  go run ./cmd/ai-dispatch send mimo "Reply exactly: OK" --json-result --stream-progress --timeout 120 --activity-timeout 60

if [[ "${AI_DISPATCH_SMOKE_CLAUDE:-off}" == "on" ]]; then
  claude_target="${AI_DISPATCH_SMOKE_CLAUDE_TARGET:-claude}"
  run_smoke "claude ${claude_target}" \
    go run ./cmd/ai-dispatch send "$claude_target" "Reply exactly: OK" --json-result --timeout 45 --activity-timeout 20
  if [[ "${AI_DISPATCH_SMOKE_CLAUDE_PTY:-off}" == "on" ]]; then
    run_smoke "claude ${claude_target} pty" \
      go run ./cmd/ai-dispatch send "$claude_target" "Reply exactly: OK" --provider-opt claude.transport=pty --json-result --timeout 90 --activity-timeout 120
  else
    echo "[provider-smoke] claude pty deferred; set AI_DISPATCH_SMOKE_CLAUDE_PTY=on when tmux smoke is intended."
  fi
else
  echo "[provider-smoke] claude deferred; set AI_DISPATCH_SMOKE_CLAUDE=on after Claude CLI auth is active."
fi

if [[ "${AI_DISPATCH_SMOKE_AGY:-off}" == "on" ]]; then
  agy_target="${AI_DISPATCH_SMOKE_AGY_TARGET:-antigravity}"
  agy_model="${AI_DISPATCH_SMOKE_AGY_MODEL:-flash}"
  run_smoke "antigravity agy ${agy_model}" \
    go run ./cmd/ai-dispatch send "$agy_target" "Reply exactly: OK" --model "$agy_model" --json-result --timeout 180 --activity-timeout 90
else
  echo "[provider-smoke] antigravity agy deferred; set AI_DISPATCH_SMOKE_AGY=on when agy auth is active."
fi
