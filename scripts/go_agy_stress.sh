#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

rounds="${AI_DISPATCH_AGY_STRESS_ROUNDS:-5}"
model="${AI_DISPATCH_AGY_STRESS_MODEL:-flash}"
timeout="${AI_DISPATCH_AGY_STRESS_TIMEOUT:-180}"
activity_timeout="${AI_DISPATCH_AGY_STRESS_ACTIVITY_TIMEOUT:-90}"
prompt_prefix="${AI_DISPATCH_AGY_STRESS_PROMPT:-Reply exactly with AGY_OK and the round number.}"

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on

echo "[agy-stress] rounds=${rounds} model=${model} timeout=${timeout}s activity_timeout=${activity_timeout}s"

last_session=""
for i in $(seq 1 "$rounds"); do
  echo "[agy-stress] round ${i}/${rounds}"
  out="$(mktemp -t ai-dispatch-agy-stress.XXXXXX.json)"
  err="$(mktemp -t ai-dispatch-agy-stress.XXXXXX.stderr)"
  set +e
  bin/ai-dispatch send antigravity "${prompt_prefix} Round ${i}." \
    --model "$model" \
    --json-result \
    --stream-progress \
    --timeout "$timeout" \
    --activity-timeout "$activity_timeout" \
    >"$out" 2>"$err"
  code=$?
  set -e
  if [[ "$code" -ne 0 ]]; then
    echo "[agy-stress] round ${i} failed with code ${code}"
    echo "[agy-stress] stdout:"
    cat "$out"
    echo
    echo "[agy-stress] stderr:"
    cat "$err"
    echo
    exit "$code"
  fi
  if ! grep -q '"ok":true' "$out"; then
    echo "[agy-stress] round ${i} returned non-ok result"
    cat "$out"
    echo
    cat "$err"
    echo
    exit 1
  fi
  if ! grep -q 'AGY_OK' "$out"; then
    echo "[agy-stress] round ${i} did not contain expected AGY_OK marker"
    cat "$out"
    echo
    cat "$err"
    echo
    exit 1
  fi
  session_id="$(sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p' "$out" | head -n 1)"
  last_session="$session_id"
  echo "[agy-stress] round ${i} pass session=${session_id:-unknown}"
  rm -f "$out" "$err"
done

if [[ -n "$last_session" ]]; then
  for marker in AGY_RESUME_ONE AGY_RESUME_TWO; do
    echo "[agy-stress] resume ${marker} session=${last_session}"
    out="$(mktemp -t ai-dispatch-agy-resume.XXXXXX.json)"
    err="$(mktemp -t ai-dispatch-agy-resume.XXXXXX.stderr)"
    set +e
    bin/ai-dispatch resume \
      --session-id "$last_session" \
      --session-provider antigravity \
      "Reply exactly: ${marker}" \
      --model "$model" \
      --json-result \
      --timeout "$timeout" \
      --activity-timeout "$activity_timeout" \
      >"$out" 2>"$err"
    code=$?
    set -e
    if [[ "$code" -ne 0 ]] || ! grep -q '"ok":true' "$out" || ! grep -q "$marker" "$out"; then
      echo "[agy-stress] resume ${marker} failed or returned stale output"
      echo "[agy-stress] stdout:"
      cat "$out"
      echo
      echo "[agy-stress] stderr:"
      cat "$err"
      echo
      exit 1
    fi
    rm -f "$out" "$err"
  done
fi

echo "[agy-stress] pass"
