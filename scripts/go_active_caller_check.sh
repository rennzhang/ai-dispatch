#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

tmpdir="$(mktemp -d -t ai-dispatch-active-callers.XXXXXX)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

prompt="$tmpdir/prompt.md"
output="$tmpdir/output.md"
bin="$tmpdir/ai-dispatch-go"
printf 'Reply exactly: OK\n' >"$prompt"
go build -o "$bin" ./cmd/ai-dispatch

export AI_DISPATCH_RUNS_DIR="$tmpdir/runs"

expect_disabled() {
  local name="$1"
  shift
  echo "[active-caller] $name"
  set +e
  local stdout
  stdout="$("$bin" "$@" 2>"$tmpdir/$name.stderr")"
  local code=$?
  set -e
  if [[ "$code" -ne 3 ]]; then
    echo "[active-caller] expected disabled exit 3, got $code"
    echo "$stdout"
    cat "$tmpdir/$name.stderr"
    exit 1
  fi
  if [[ "$stdout" != *'"status":"disabled"'* ]]; then
    echo "[active-caller] expected structured disabled result"
    echo "$stdout"
    cat "$tmpdir/$name.stderr"
    exit 1
  fi
}

expect_disabled_plain() {
  local name="$1"
  shift
  echo "[active-caller] $name"
  set +e
  "$bin" "$@" >"$tmpdir/$name.stdout" 2>"$tmpdir/$name.stderr"
  local code=$?
  set -e
  if [[ "$code" -ne 3 ]]; then
    echo "[active-caller] expected disabled exit 3, got $code"
    cat "$tmpdir/$name.stdout"
    cat "$tmpdir/$name.stderr"
    exit 1
  fi
  if ! grep -Eq "disabled|provider execution" "$tmpdir/$name.stderr"; then
    echo "[active-caller] expected disabled stderr"
    cat "$tmpdir/$name.stdout"
    cat "$tmpdir/$name.stderr"
    exit 1
  fi
}

expect_success() {
  local name="$1"
  shift
  echo "[active-caller] $name"
  "$bin" "$@" >/tmp/ai-dispatch-active-caller.stdout
}

expect_disabled "autopilot-worker" \
  send \
  codex \
  --prompt-file "$prompt" \
  --cwd "$ROOT" \
  --timeout 0 \
  --activity-timeout 180 \
  --json-result \
  --stream-progress \
  --caller-module autopilot-worker \
  -o "$output"

expect_disabled_plain "autopilot-governor-claude" \
  send \
  claude \
  --prompt-file "$prompt" \
  --output-file "$output" \
  --cwd "$ROOT" \
  --timeout 0 \
  --activity-timeout 180 \
  --stream-progress \
  --caller-module autopilot-governor

expect_disabled "agent-bridge-claude-pty" \
  send \
  claude \
  --json-result \
  --stream-progress \
  --timeout 0 \
  --activity-timeout 300 \
  --model sonnet \
  --provider-opt claude.transport=pty \
  --session-id sid-bridge \
  --session-provider claude \
  --cwd "$ROOT" \
  "hello"

expect_disabled "agent-chat-codex" \
  send \
  codex \
  --json-result \
  --stream-progress \
  --timeout 0 \
  --activity-timeout 300 \
  --model gpt-5.5 \
  --session-id sid-wx \
  --session-provider codex \
  --cwd "$ROOT" \
  --caller-env chat \
  --caller-module agent-chat \
  --caller-provider codex/gpt-5.5 \
  "hello"

expect_disabled "agent-workflow-codex" \
  send \
  codex \
  --json-result \
  --stream-progress \
  --timeout 0 \
  --activity-timeout 900 \
  --model gpt-5.5 \
  --session-id sid-workflow \
  --session-provider codex \
  --cwd "$ROOT" \
  --caller-env workflow \
  --caller-module agent-workflow \
  --caller-provider codex/gpt-5.5 \
  "hello"

expect_disabled "dispatch-skill-task-name" \
  send \
  gpt5.5 \
  "hello" \
  --task-name review-r1 \
  --cwd "$ROOT" \
  --json-result \
  --activity-timeout 180

expect_disabled "resume-session" \
  resume --session-id sid-bridge \
  "continue" \
  --target gpt5.5 \
  --task-name review-r2 \
  --json-result

expect_success "runs-list-filter" \
  runs list \
  --task-name 'review-*' \
  --target gpt5.5 \
  --status disabled \
  --limit 5

echo "[active-caller] pass"
