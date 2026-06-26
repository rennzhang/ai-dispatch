#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

rounds="${AI_DISPATCH_AGY_CRUD_ROUNDS:-3}"
timeout="${AI_DISPATCH_AGY_CRUD_TIMEOUT:-240}"
activity_timeout="${AI_DISPATCH_AGY_CRUD_ACTIVITY_TIMEOUT:-120}"
models_raw="${AI_DISPATCH_AGY_CRUD_MODELS:-flash pro}"
read -r -a models <<<"$models_raw"
scratch_root="${AI_DISPATCH_AGY_CRUD_ROOT:-$(mktemp -d -t ai-dispatch-agy-crud.XXXXXX)}"

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on

echo "[agy-crud] root=${scratch_root}"
echo "[agy-crud] rounds=${rounds} models=${models_raw}"

last_session=""
last_workdir=""

for i in $(seq 1 "$rounds"); do
  model_index=$(( (i - 1) % ${#models[@]} ))
  model="${models[$model_index]}"
  workdir="${scratch_root}/round-${i}"
  mkdir -p "${workdir}/data" "${workdir}/docs" "${workdir}/archive"
  printf '{"items":[{"id":"alpha","status":"todo"},{"id":"beta","status":"todo"}]}' >"${workdir}/data/items.json"
  printf 'Project: agy CRUD stress round %s\nRule: report must include the round marker.\n' "$i" >"${workdir}/docs/source.md"
  printf 'remove me round %s\n' "$i" >"${workdir}/archive/delete-me.tmp"

  done_marker="AGY_CRUD_${i}_DONE"
  report_marker="AGY_CRUD_${i}_REPORT"
  echo "[agy-crud] round ${i}/${rounds} model=${model}"
  out="$(mktemp -t ai-dispatch-agy-crud.XXXXXX.json)"
  err="$(mktemp -t ai-dispatch-agy-crud.XXXXXX.stderr)"
  set +e
  bin/ai-dispatch send antigravity \
    "You are operating inside the current working directory only. Perform these exact filesystem tasks: 1. Read docs/source.md. 2. Update data/items.json so alpha becomes done and add gamma with status in_progress. Keep valid JSON. 3. Delete archive/delete-me.tmp. 4. Create docs/report.md with a short summary and include the marker ${report_marker}. 5. Reply exactly with ${done_marker} when finished." \
    --model "$model" \
    --cwd "$workdir" \
    --json-result \
    --stream-progress \
    --timeout "$timeout" \
    --activity-timeout "$activity_timeout" \
    >"$out" 2>"$err"
  code=$?
  set -e
  if [[ "$code" -ne 0 ]] || ! grep -q '"ok":true' "$out" || ! grep -q "$done_marker" "$out"; then
    echo "[agy-crud] round ${i} command failed"
    cat "$out"
    echo
    cat "$err"
    exit 1
  fi
  if ! grep -q '"id":"alpha","status":"done"' "${workdir}/data/items.json"; then
    echo "[agy-crud] round ${i} did not update alpha"
    cat "${workdir}/data/items.json"
    exit 1
  fi
  if ! grep -q '"id":"gamma","status":"in_progress"' "${workdir}/data/items.json"; then
    echo "[agy-crud] round ${i} did not add gamma"
    cat "${workdir}/data/items.json"
    exit 1
  fi
  if [[ -e "${workdir}/archive/delete-me.tmp" ]]; then
    echo "[agy-crud] round ${i} did not delete archive/delete-me.tmp"
    exit 1
  fi
  if ! grep -q "$report_marker" "${workdir}/docs/report.md"; then
    echo "[agy-crud] round ${i} did not write report marker"
    cat "${workdir}/docs/report.md"
    exit 1
  fi
  last_session="$(sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p' "$out" | head -n 1)"
  last_workdir="$workdir"
  echo "[agy-crud] round ${i} pass session=${last_session:-unknown}"
  rm -f "$out" "$err"
done

if [[ -n "$last_session" && -n "$last_workdir" ]]; then
  marker="AGY_CRUD_RESUME_APPEND"
  echo "[agy-crud] resume append session=${last_session}"
  out="$(mktemp -t ai-dispatch-agy-crud-resume.XXXXXX.json)"
  err="$(mktemp -t ai-dispatch-agy-crud-resume.XXXXXX.stderr)"
  set +e
  bin/ai-dispatch resume \
    --session-id "$last_session" \
    --session-provider antigravity \
    --model "${models[0]}" \
    --cwd "$last_workdir" \
    "Append a new section to docs/report.md with marker ${marker}. Then reply exactly: ${marker}_DONE" \
    --json-result \
    --stream-progress \
    --timeout "$timeout" \
    --activity-timeout "$activity_timeout" \
    >"$out" 2>"$err"
  code=$?
  set -e
  if [[ "$code" -ne 0 ]] || ! grep -q '"ok":true' "$out" || ! grep -q "${marker}_DONE" "$out" || ! grep -q "$marker" "${last_workdir}/docs/report.md"; then
    echo "[agy-crud] resume append failed"
    cat "$out"
    echo
    cat "$err"
    exit 1
  fi
  rm -f "$out" "$err"
fi

echo "[agy-crud] pass root=${scratch_root}"
