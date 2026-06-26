#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on

tmpdir="$(mktemp -d "$ROOT/tmp-pty-stress.XXXXXX")"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

run_json() {
  local name="$1"
  shift
  echo "[pty-stress] $name" >&2
  "$@"
}

assert_text() {
  local expected="$1"
  local json="$2"
  node -e '
const [expected, raw] = process.argv.slice(1);
const payload = JSON.parse(raw);
if (!payload.ok) throw new Error(`not ok: ${JSON.stringify(payload)}`);
const actual = String(payload.text || "").trim();
if (actual !== expected) {
  throw new Error(`text mismatch: expected=${JSON.stringify(expected)} actual=${JSON.stringify(actual)} payload=${JSON.stringify(payload)}`);
}
' "$expected" "$json"
}

assert_contains_text() {
  local expected="$1"
  local json="$2"
  node -e '
const [expected, raw] = process.argv.slice(1);
const payload = JSON.parse(raw);
if (!payload.ok) throw new Error(`not ok: ${JSON.stringify(payload)}`);
const actual = String(payload.text || "").trim();
if (!actual.includes(expected)) {
  throw new Error(`text does not contain ${JSON.stringify(expected)}: actual=${JSON.stringify(actual)} payload=${JSON.stringify(payload)}`);
}
' "$expected" "$json"
}

exact_token="GO-PTY-EXACT-$(date +%s)"
exact_json="$(run_json "exact-token" go run ./cmd/ai-dispatch send claude "Reply exactly: $exact_token" --provider-opt claude.transport=pty --json-result --timeout 120 --activity-timeout 150)"
assert_text "$exact_token" "$exact_json"

file_token="GO-PTY-FILE-$(date +%s)"
printf '%s\n' "$file_token" >"$tmpdir/secret.txt"
file_json="$(run_json "file-read" go run ./cmd/ai-dispatch send claude "Read ./secret.txt, then reply exactly: FILE_OK:<file contents>" --provider-opt claude.transport=pty --json-result --cwd "$tmpdir" --timeout 150 --activity-timeout 180)"
assert_text "FILE_OK:$file_token" "$file_json"

resume_token="GO-PTY-RESUME-$(date +%s)"
session_id="$(node -e '
const payload = JSON.parse(process.argv[1]);
process.stdout.write(payload.session_id || "");
' "$exact_json"
)"
if [[ -z "$session_id" ]]; then
  echo "[pty-stress] exact-token did not return session_id" >&2
  exit 1
fi
resume_json="$(run_json "resume" go run ./cmd/ai-dispatch resume --session-id "$session_id" --target claude "Reply exactly: $resume_token" --provider-opt claude.transport=pty --json-result --timeout 150 --activity-timeout 180)"
assert_text "$resume_token" "$resume_json"

stream_token="GO-PTY-STREAM-$(date +%s)"
stream_json="$(run_json "stream-progress" go run ./cmd/ai-dispatch send claude "Reply exactly: $stream_token" --provider-opt claude.transport=pty --json-result --stream-progress --timeout 120 --activity-timeout 150)"
assert_text "$stream_token" "$stream_json"

tool_summary_json="$(run_json "tool-summary" go run ./cmd/ai-dispatch send claude "List the files in the current directory and reply with a short sentence containing the filename secret.txt." --provider-opt claude.transport=pty --json-result --cwd "$tmpdir" --timeout 150 --activity-timeout 180)"
assert_contains_text "secret.txt" "$tool_summary_json"

echo "[pty-stress] pass"
