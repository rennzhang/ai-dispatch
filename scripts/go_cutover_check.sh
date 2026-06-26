#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROADMAP="$ROOT/../../../docs/roadmap/ai-dispatch-go/implementation-status.md"

cd "$ROOT"

echo "[cutover-check] go test ./..."
go test ./...

echo "[cutover-check] active caller command-shape parity"
scripts/go_active_caller_check.sh

echo "[cutover-check] doctor"
go run ./cmd/ai-dispatch doctor --format json >/tmp/ai-dispatch-go-doctor.json

echo "[cutover-check] default wrapper doctor"
./bin/ai-dispatch doctor --format json >/tmp/ai-dispatch-go-wrapper-doctor.json

echo "[cutover-check] ren dispatch doctor"
../../../bin/ren dispatch doctor --format json >/tmp/ai-dispatch-go-ren-doctor.json

echo "[cutover-check] provider execution must be fail-closed by default"
set +e
disabled_output="$(go run ./cmd/ai-dispatch send gpt5.5 hi --json-result 2>/tmp/ai-dispatch-go-disabled.stderr)"
disabled_code=$?
set -e
if [[ "$disabled_code" -eq 0 ]]; then
  echo "[cutover-check] expected disabled send to fail before explicit smoke"
  exit 1
fi
if [[ "$disabled_output" != *'"status":"disabled"'* ]]; then
  echo "[cutover-check] disabled send did not return structured disabled result"
  echo "$disabled_output"
  exit 1
fi

if grep -q "Cutover is blocked" "$ROADMAP"; then
  echo "[cutover-check] blocked: roadmap still marks go cutover as blocked."
  echo "[cutover-check] this is expected until parity, shadow tests, smoke, rollback, and user approval are complete."
  exit 10
fi

echo "[cutover-check] pass"
