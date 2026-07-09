#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="${AI_DISPATCH_ACCEPTANCE_TARGET:?set AI_DISPATCH_ACCEPTANCE_TARGET}"
EXPECTED_PROVIDER="${AI_DISPATCH_ACCEPTANCE_PROVIDER:?set AI_DISPATCH_ACCEPTANCE_PROVIDER}"
EXPECTED_MODEL="${AI_DISPATCH_ACCEPTANCE_MODEL:-}"
REPORT_DIR="${AI_DISPATCH_ACCEPTANCE_REPORT_DIR:-$(mktemp -d -t ai-dispatch-provider-acceptance.XXXXXX)}"
RUNS_DIR="$REPORT_DIR/runs"
DISPATCH="${AI_DISPATCH_BIN:-bin/ai-dispatch}"
TIMEOUT="${AI_DISPATCH_ACCEPTANCE_TIMEOUT:-240}"
ACTIVITY_TIMEOUT="${AI_DISPATCH_ACCEPTANCE_ACTIVITY_TIMEOUT:-120}"
TASK_PREFIX="${AI_DISPATCH_ACCEPTANCE_TASK_PREFIX:-acceptance-${TARGET//[^A-Za-z0-9_.-]/_}}"
RESUME_TARGET="${AI_DISPATCH_ACCEPTANCE_RESUME_TARGET:-$TARGET}"
PROVIDER_OPTS_CSV="${AI_DISPATCH_ACCEPTANCE_PROVIDER_OPTS:-}"
TOOL_PROVIDER_OPTS_CSV="${AI_DISPATCH_ACCEPTANCE_TOOL_PROVIDER_OPTS:-$PROVIDER_OPTS_CSV}"
VARIANT_TARGET="${AI_DISPATCH_ACCEPTANCE_VARIANT_TARGET:-}"
VARIANT_PROVIDER="${AI_DISPATCH_ACCEPTANCE_VARIANT_PROVIDER:-$EXPECTED_PROVIDER}"
VARIANT_MODEL="${AI_DISPATCH_ACCEPTANCE_VARIANT_MODEL:-}"
FALLBACK_ENV="${AI_DISPATCH_ACCEPTANCE_FALLBACK_ENV:-}"
FALLBACK_PROVIDER="${AI_DISPATCH_ACCEPTANCE_FALLBACK_PROVIDER:-}"
FALLBACK_MODEL="${AI_DISPATCH_ACCEPTANCE_FALLBACK_MODEL:-}"
INVALID_PROVIDER_OPT="${AI_DISPATCH_ACCEPTANCE_INVALID_PROVIDER_OPT:-}"
INVALID_MODEL="${AI_DISPATCH_ACCEPTANCE_INVALID_MODEL:-}"
UNSUPPORTED_TARGETS="${AI_DISPATCH_ACCEPTANCE_UNSUPPORTED_TARGETS:-}"
SKIP_RESUME="${AI_DISPATCH_ACCEPTANCE_SKIP_RESUME:-off}"
SKIP_PROMPT_FILE="${AI_DISPATCH_ACCEPTANCE_SKIP_PROMPT_FILE:-off}"
SKIP_STDIN="${AI_DISPATCH_ACCEPTANCE_SKIP_STDIN:-off}"
SKIP_CWD="${AI_DISPATCH_ACCEPTANCE_SKIP_CWD:-off}"

mkdir -p "$RUNS_DIR"
cd "$ROOT"

rm -f "$REPORT_DIR/summary.jsonl" "$REPORT_DIR/summary.json"

if ! command -v node >/dev/null 2>&1; then
  echo "[provider-acceptance] node is required for JSON summary generation" >&2
  exit 127
fi

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on
export AI_DISPATCH_RUNS_DIR="$RUNS_DIR"

printf '[provider-acceptance] target=%s provider=%s model=%s report_dir=%s\n' \
  "$TARGET" "$EXPECTED_PROVIDER" "${EXPECTED_MODEL:-}" "$REPORT_DIR"

provider_opts=()
tool_provider_opts=()

append_provider_opts_to_provider() {
  local csv="$1"
  local item
  local old_ifs="$IFS"
  IFS=','
  for item in $csv; do
    item="${item#"${item%%[![:space:]]*}"}"
    item="${item%"${item##*[![:space:]]}"}"
    if [[ -n "$item" ]]; then
      provider_opts+=(--provider-opt "$item")
    fi
  done
  IFS="$old_ifs"
}

append_provider_opts_to_tool() {
  local csv="$1"
  local item
  local old_ifs="$IFS"
  IFS=','
  for item in $csv; do
    item="${item#"${item%%[![:space:]]*}"}"
    item="${item%"${item##*[![:space:]]}"}"
    if [[ -n "$item" ]]; then
      tool_provider_opts+=(--provider-opt "$item")
    fi
  done
  IFS="$old_ifs"
}

append_provider_opts_to_provider "$PROVIDER_OPTS_CSV"
append_provider_opts_to_tool "$TOOL_PROVIDER_OPTS_CSV"

safe_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
}

last_json_field() {
  local file="$1"
  local field="$2"
  node - "$file" "$field" <<'NODE'
const fs = require("fs");
const [file, field] = process.argv.slice(2);
const lines = fs.readFileSync(file, "utf8").trim().split(/\n/).reverse();
for (const line of lines) {
  if (!line.trim().startsWith("{")) continue;
  try {
    const parsed = JSON.parse(line);
    const value = parsed[field];
    if (typeof value === "string") process.stdout.write(value);
    else if (value != null) process.stdout.write(String(value));
    process.exit(0);
  } catch {}
}
NODE
}

run_case() {
  local name="$1"
  shift
  local safe out err meta start end code
  safe="$(safe_name "$name")"
  out="$REPORT_DIR/${safe}.stdout"
  err="$REPORT_DIR/${safe}.stderr"
  meta="$REPORT_DIR/${safe}.meta"

  printf '[provider-acceptance] START %s\n' "$name"
  start="$(date +%s)"
  set +e
  "$@" >"$out" 2>"$err"
  code=$?
  set -e
  end="$(date +%s)"
  printf 'name=%s\ncode=%s\nduration_s=%s\n' "$name" "$code" "$((end - start))" >"$meta"
  summarize_case "$name" "$code" "$((end - start))" "$out" "$err"
}

run_case_stdin() {
  local name="$1"
  local prompt="$2"
  shift 2
  local safe out err meta start end code
  safe="$(safe_name "$name")"
  out="$REPORT_DIR/${safe}.stdout"
  err="$REPORT_DIR/${safe}.stderr"
  meta="$REPORT_DIR/${safe}.meta"

  printf '[provider-acceptance] START %s\n' "$name"
  start="$(date +%s)"
  set +e
  printf '%s' "$prompt" | "$@" >"$out" 2>"$err"
  code=$?
  set -e
  end="$(date +%s)"
  printf 'name=%s\ncode=%s\nduration_s=%s\n' "$name" "$code" "$((end - start))" >"$meta"
  summarize_case "$name" "$code" "$((end - start))" "$out" "$err"
}

summarize_case() {
  local name="$1"
  local code="$2"
  local duration="$3"
  local out="$4"
  local err="$5"
  node - "$name" "$code" "$duration" "$out" "$err" <<'NODE' >>"$REPORT_DIR/summary.jsonl"
const fs = require("fs");
const [name, code, duration, outPath, errPath] = process.argv.slice(2);
const stdout = fs.readFileSync(outPath, "utf8");
const stderr = fs.readFileSync(errPath, "utf8");
let parsed = null;
for (const line of stdout.trim().split(/\n/).reverse()) {
  if (!line.trim().startsWith("{")) continue;
  try {
    parsed = JSON.parse(line);
    break;
  } catch {}
}
const text = parsed && typeof parsed.text === "string" ? parsed.text : "";
const expected = (name.match(/EXPECT_([A-Z0-9_]+)/) || [])[1] || "";
const expectsFailure = expected === "ERROR";
const provider = parsed && (parsed.provider_used || parsed.provider) || null;
const model = parsed && (parsed.model_used || parsed.model) || null;
const row = {
  name,
  code: Number(code),
  duration_s: Number(duration),
  ok: parsed && typeof parsed.ok === "boolean" ? parsed.ok : Number(code) === 0,
  status: parsed && parsed.status || null,
  provider,
  model,
  requested_target: parsed && (parsed.requested_target || parsed.requested) || null,
  session_id: parsed && parsed.session_id || null,
  degraded: parsed && parsed.degraded || false,
  degrade_reason: parsed && parsed.degrade_reason || "",
  failure_class: parsed && parsed.failure_class || null,
  route_steps: parsed && parsed.route_steps || [],
  marker_ok: expected && !expectsFailure ? text.includes(expected) || stdout.includes(expected) : undefined,
  stdout_path: outPath,
  stderr_path: errPath,
  stderr_head: stderr.slice(0, 500),
};
console.log(JSON.stringify(row));
NODE
  tail -n 1 "$REPORT_DIR/summary.jsonl"
}

run_case "doctor_json" "$DISPATCH" doctor --format json
run_case "providers_scan_json" "$DISPATCH" providers scan --format json
run_case "resolve_${TARGET}" "$DISPATCH" models resolve "$TARGET" --format json

if [[ -n "$UNSUPPORTED_TARGETS" ]]; then
  old_ifs="$IFS"
  IFS=','
  for unsupported in $UNSUPPORTED_TARGETS; do
    unsupported="${unsupported#"${unsupported%%[![:space:]]*}"}"
    unsupported="${unsupported%"${unsupported##*[![:space:]]}"}"
    if [[ -n "$unsupported" ]]; then
      run_case "resolve_unsupported_${unsupported}_EXPECT_ERROR" \
        "$DISPATCH" models resolve "$unsupported" --format json
    fi
  done
  IFS="$old_ifs"
fi

run_case "native_send_EXPECT_NATIVE_OK" \
  "$DISPATCH" send "$TARGET" "Reply exactly: NATIVE_OK" \
    --json-result --stream-progress --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
    --task-name "${TASK_PREFIX}-native" "${provider_opts[@]}"

native_stdout="$REPORT_DIR/$(safe_name native_send_EXPECT_NATIVE_OK).stdout"
native_session="$(last_json_field "$native_stdout" session_id)"
if [[ "$SKIP_RESUME" != "on" && -n "$native_session" ]]; then
  run_case "resume_native_EXPECT_RESUME_OK" \
    "$DISPATCH" resume --session-id "$native_session" --session-provider "$RESUME_TARGET" \
      "Reply exactly: RESUME_OK" \
      --json-result --stream-progress --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-resume" "${provider_opts[@]}"
elif [[ "$SKIP_RESUME" != "on" ]]; then
  printf '[provider-acceptance] SKIP resume_native no native session_id\n'
fi

if [[ -n "$EXPECTED_MODEL" ]]; then
  run_case "explicit_model_EXPECT_EXPLICIT_OK" \
    "$DISPATCH" send "$TARGET" "Reply exactly: EXPLICIT_OK" \
      --model "$EXPECTED_MODEL" --json-result --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-explicit" "${provider_opts[@]}"
fi

if [[ -n "$VARIANT_TARGET" ]]; then
  run_case "variant_${VARIANT_TARGET}_EXPECT_VARIANT_OK" \
    "$DISPATCH" send "$VARIANT_TARGET" "Reply exactly: VARIANT_OK" \
      --json-result --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-variant" "${provider_opts[@]}"
fi

if [[ "$SKIP_PROMPT_FILE" != "on" ]]; then
  prompt_file="$REPORT_DIR/prompt.md"
  output_file="$REPORT_DIR/output.md"
  printf 'Reply exactly: PROMPT_FILE_OK\n' >"$prompt_file"
  run_case "prompt_file_output_file_EXPECT_PROMPT_FILE_OK" \
    "$DISPATCH" send "$TARGET" --prompt-file "$prompt_file" \
      --json-result --output-file "$output_file" --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-prompt-file" "${provider_opts[@]}"
  run_case "output_file_contains_EXPECT_PROMPT_FILE_OK" \
    bash -lc 'grep -q "PROMPT_FILE_OK" "$1" && printf "{\"ok\":true,\"text\":\"PROMPT_FILE_OK\"}\n"' bash "$output_file"
fi

if [[ "$SKIP_STDIN" != "on" ]]; then
  run_case_stdin "stdin_prompt_EXPECT_STDIN_OK" "Reply exactly: STDIN_OK" \
    "$DISPATCH" send "$TARGET" --json-result --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-stdin" "${provider_opts[@]}"
fi

if [[ "$SKIP_CWD" != "on" ]]; then
  workdir="$REPORT_DIR/workdir"
  mkdir -p "$workdir"
  printf 'needle-from-provider-acceptance\n' >"$workdir/needle.txt"
  run_case "cwd_file_read_EXPECT_CWD_OK" \
    "$DISPATCH" send "$TARGET" "Read needle.txt in cwd, confirm it contains needle-from-provider-acceptance, and reply exactly: CWD_OK" \
      --cwd "$workdir" --json-result --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-cwd" "${tool_provider_opts[@]}"
fi

if [[ -n "$FALLBACK_ENV" ]]; then
  run_case "fallback_EXPECT_FALLBACK_OK" \
    env "$FALLBACK_ENV=$REPORT_DIR/missing-${EXPECTED_PROVIDER}" "$DISPATCH" send "$TARGET" \
      "Reply exactly: FALLBACK_OK" \
      --json-result --timeout "$TIMEOUT" --activity-timeout "$ACTIVITY_TIMEOUT" \
      --task-name "${TASK_PREFIX}-fallback"
fi

if [[ -n "$INVALID_PROVIDER_OPT" ]]; then
  run_case "invalid_provider_opt_EXPECT_ERROR" \
    "$DISPATCH" send "$TARGET" "hello" --json-result --provider-opt "$INVALID_PROVIDER_OPT"
fi

if [[ -n "$INVALID_MODEL" ]]; then
  run_case "invalid_model_EXPECT_ERROR" \
    "$DISPATCH" send "$TARGET" "hello" --model "$INVALID_MODEL" --json-result
fi

run_case "runs_list_target" "$DISPATCH" runs list --task-name "${TASK_PREFIX}-*" --limit 20

node - "$REPORT_DIR/summary.jsonl" "$EXPECTED_PROVIDER" "$EXPECTED_MODEL" "$VARIANT_TARGET" "$VARIANT_PROVIDER" "$VARIANT_MODEL" "$FALLBACK_PROVIDER" "$FALLBACK_MODEL" "$INVALID_PROVIDER_OPT" "$INVALID_MODEL" <<'NODE'
const fs = require("fs");
const [
  summaryPath,
  expectedProvider,
  expectedModel,
  variantTarget,
  variantProvider,
  variantModel,
  fallbackProvider,
  fallbackModel,
  invalidProviderOpt,
  invalidModel,
] = process.argv.slice(2);
const rows = fs.readFileSync(summaryPath, "utf8").trim().split(/\n/).filter(Boolean).map(JSON.parse);
const byName = Object.fromEntries(rows.map((row) => [row.name, row]));
const failures = [];
for (const row of rows) {
  const expected = (row.name.match(/EXPECT_([A-Z0-9_]+)/) || [])[1] || "";
  const expectsFailure = expected === "ERROR";
  if (expectsFailure) {
    if (row.code === 0 || row.ok === true) failures.push(`${row.name}: expected failure`);
    continue;
  }
  if (row.marker_ok === false) failures.push(`${row.name}: marker missing`);
  if (row.code !== 0 || row.ok === false) failures.push(`${row.name}: command failed`);
}
function requireRow(name, predicate, message) {
  const row = byName[name];
  if (!row) {
    failures.push(`${name}: missing`);
  } else if (!predicate(row)) {
    failures.push(`${name}: ${message}`);
  }
}
function expectedNative(row) {
  if (row.provider !== expectedProvider) return false;
  if (expectedModel && row.model !== expectedModel) return false;
  return true;
}
const target = process.env.AI_DISPATCH_ACCEPTANCE_TARGET || "";
requireRow(`resolve_${target}`, (row) => {
  if (row.provider !== expectedProvider) return false;
  if (expectedModel && row.model !== expectedModel) return false;
  return true;
}, "expected target resolution");
requireRow("native_send_EXPECT_NATIVE_OK", (row) => expectedNative(row) && !!row.session_id, "expected native provider session");
if (byName.resume_native_EXPECT_RESUME_OK) {
  requireRow("resume_native_EXPECT_RESUME_OK", expectedNative, "expected native resume");
}
if (expectedModel) {
  requireRow("explicit_model_EXPECT_EXPLICIT_OK", expectedNative, "expected explicit model route");
}
if (variantTarget) {
  requireRow(`variant_${variantTarget}_EXPECT_VARIANT_OK`, (row) => {
    if (row.provider !== variantProvider) return false;
    if (variantModel && row.model !== variantModel) return false;
    return true;
  }, "expected variant target route");
}
if (byName.prompt_file_output_file_EXPECT_PROMPT_FILE_OK) {
  requireRow("prompt_file_output_file_EXPECT_PROMPT_FILE_OK", expectedNative, "expected prompt-file route");
}
if (byName.stdin_prompt_EXPECT_STDIN_OK) {
  requireRow("stdin_prompt_EXPECT_STDIN_OK", expectedNative, "expected stdin route");
}
if (byName.cwd_file_read_EXPECT_CWD_OK) {
  requireRow("cwd_file_read_EXPECT_CWD_OK", expectedNative, "expected cwd route");
}
if (fallbackProvider) {
  requireRow("fallback_EXPECT_FALLBACK_OK", (row) => {
    if (row.provider !== fallbackProvider) return false;
    if (fallbackModel && row.model !== fallbackModel) return false;
    return row.degraded === true && row.route_steps.length >= 2;
  }, "expected configured fallback candidate");
}
if (invalidProviderOpt) {
  requireRow("invalid_provider_opt_EXPECT_ERROR", (row) => row.failure_class === "input", "expected input failure");
}
if (invalidModel) {
  requireRow("invalid_model_EXPECT_ERROR", (row) => row.failure_class === "input", "expected input failure");
}

const byProvider = {};
for (const row of rows) byProvider[row.provider || "none"] = (byProvider[row.provider || "none"] || 0) + 1;
const report = {
  report_dir: summaryPath.replace(/\/summary\.jsonl$/, ""),
  target: process.env.AI_DISPATCH_ACCEPTANCE_TARGET || null,
  expected_provider: expectedProvider,
  expected_model: expectedModel || null,
  total: rows.length,
  failures: failures.length,
  byProvider,
  failure_names: failures,
};
fs.writeFileSync(summaryPath.replace(/summary\.jsonl$/, "summary.json"), JSON.stringify(report, null, 2) + "\n");
console.log(JSON.stringify(report, null, 2));
if (failures.length > 0) process.exit(1);
NODE

printf '[provider-acceptance] pass report_dir=%s\n' "$REPORT_DIR"
