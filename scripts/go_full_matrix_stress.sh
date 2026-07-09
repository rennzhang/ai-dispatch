#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT_DIR="${AI_DISPATCH_FULL_STRESS_REPORT_DIR:-$(mktemp -d -t ai-dispatch-go-stress.XXXXXX)}"
RUNS_DIR="$REPORT_DIR/runs"
TARGETS_FILE="$REPORT_DIR/targets.json"

mkdir -p "$RUNS_DIR"
cd "$ROOT"

export AI_DISPATCH_GO_PROVIDER_EXECUTION=on
export AI_DISPATCH_RUNS_DIR="$RUNS_DIR"

printf '[stress] report_dir=%s\n' "$REPORT_DIR"

run_case() {
  local name="$1"
  shift
  local safe="${name//[^A-Za-z0-9_.-]/_}"
  local out="$REPORT_DIR/${safe}.stdout"
  local err="$REPORT_DIR/${safe}.stderr"
  local meta="$REPORT_DIR/${safe}.meta"
  local start end code

  printf '[stress] START %s\n' "$name"
  start="$(date +%s)"
  set +e
  "$@" >"$out" 2>"$err"
  code=$?
  set -e
  end="$(date +%s)"
  printf 'name=%s\ncode=%s\nduration_s=%s\n' "$name" "$code" "$((end - start))" >"$meta"
  node - "$name" "$code" "$((end - start))" "$out" "$err" <<'NODE' >>"$REPORT_DIR/summary.jsonl"
const fs = require("fs");
const [name, code, dur, outPath, errPath] = process.argv.slice(2);
const stdout = fs.readFileSync(outPath, "utf8");
const stderr = fs.readFileSync(errPath, "utf8");
let parsed = null;
for (const line of stdout.trim().split(/\n/).reverse()) {
  if (!line.trim().startsWith("{") && !line.trim().startsWith("[")) continue;
  try {
    parsed = JSON.parse(line);
    break;
  } catch {}
}
const text = parsed && typeof parsed.text === "string" ? parsed.text : "";
const expectedMatch = name.match(/EXPECT_([A-Z0-9_]+)/);
const expected = expectedMatch ? expectedMatch[1] : "";
const expectsFailure = expected === "ERROR" || expected === "TIMEOUT";
const markerOk = expected && !expectsFailure ? text.includes(expected) || stdout.includes(expected) : undefined;
const row = {
  name,
  code: Number(code),
  duration_s: Number(dur),
  ok: parsed && typeof parsed.ok === "boolean" ? parsed.ok : Number(code) === 0,
  status: parsed && parsed.status || null,
  provider: parsed && parsed.provider_used || null,
  model: parsed && parsed.model_used || null,
  session_id: parsed && parsed.session_id || null,
  marker_ok: markerOk,
  stdout_path: outPath,
  stderr_path: errPath,
  stderr_head: stderr.slice(0, 500),
};
console.log(JSON.stringify(row));
NODE
  tail -n 1 "$REPORT_DIR/summary.jsonl"
}

run_case_stdin() {
  local name="$1"
  local prompt="$2"
  shift 2
  local safe="${name//[^A-Za-z0-9_.-]/_}"
  local out="$REPORT_DIR/${safe}.stdout"
  local err="$REPORT_DIR/${safe}.stderr"
  local meta="$REPORT_DIR/${safe}.meta"
  local start end code

  printf '[stress] START %s\n' "$name"
  start="$(date +%s)"
  set +e
  printf '%s' "$prompt" | "$@" >"$out" 2>"$err"
  code=$?
  set -e
  end="$(date +%s)"
  printf 'name=%s\ncode=%s\nduration_s=%s\n' "$name" "$code" "$((end - start))" >"$meta"
  node - "$name" "$code" "$((end - start))" "$out" "$err" <<'NODE' >>"$REPORT_DIR/summary.jsonl"
const fs = require("fs");
const [name, code, dur, outPath, errPath] = process.argv.slice(2);
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
console.log(JSON.stringify({
  name,
  code: Number(code),
  duration_s: Number(dur),
  ok: parsed && typeof parsed.ok === "boolean" ? parsed.ok : Number(code) === 0,
  status: parsed && parsed.status || null,
  provider: parsed && parsed.provider_used || null,
  model: parsed && parsed.model_used || null,
  session_id: parsed && parsed.session_id || null,
  marker_ok: expected ? text.includes(expected) || stdout.includes(expected) : undefined,
  stdout_path: outPath,
  stderr_path: errPath,
  stderr_head: stderr.slice(0, 500),
}));
NODE
  tail -n 1 "$REPORT_DIR/summary.jsonl"
}

run_case "doctor_json" bin/ai-dispatch doctor --format json
run_case "models_json" bin/ai-dispatch models --format json
bin/ai-dispatch models --format json >"$TARGETS_FILE"

while IFS= read -r target; do
  run_case "resolve_${target}" bin/ai-dispatch models resolve "$target" --format json
done < <(jq -r '.targets[]' "$TARGETS_FILE")

run_case "resolve_provider_explicit_model_codex_gpt54" bin/ai-dispatch models resolve codex --model gpt5.4 --format json
run_case "resolve_gemini" bin/ai-dispatch models resolve gemini --format json
run_case "cli_input_missing_EXPECT_ERROR" bin/ai-dispatch send gpt5.5 --json-result
run_case "cli_prompt_too_short_EXPECT_ERROR" bin/ai-dispatch send gpt5.5 x --json-result
run_case "cli_bad_cwd_EXPECT_ERROR" bin/ai-dispatch send gpt5.5 "hello" --cwd "$REPORT_DIR/missing" --json-result

while IFS= read -r key; do
  provider="$(bin/ai-dispatch models resolve "$key" --format json | jq -r '.Provider // .provider')"
  if [[ "${AI_DISPATCH_SKIP_AGY:-}" == "on" && "$provider" == "antigravity" ]]; then
    continue
  fi
  if [[ "${AI_DISPATCH_SKIP_GROK:-}" == "on" && "$provider" == "grok" ]]; then
    continue
  fi
  marker="OK_${key//[^A-Za-z0-9]/_}"
  run_case "provider_${provider}_${key}_EXPECT_${marker}" \
    bin/ai-dispatch send "$key" "Reply exactly: ${marker}" \
      --json-result --stream-progress --timeout 180 --activity-timeout 90 --task-name "stress-${key}"
done < <(jq -r '.targets[]' "$TARGETS_FILE")

PROMPT_FILE="$REPORT_DIR/prompt.md"
printf 'Reply exactly: PROMPT_FILE_OK\n' >"$PROMPT_FILE"
run_case "prompt_file_output_file_EXPECT_PROMPT_FILE_OK" \
  bin/ai-dispatch send gpt5.5 --prompt-file "$PROMPT_FILE" --json-result \
    --output-file "$REPORT_DIR/out.md" --timeout 180 --activity-timeout 90

run_case_stdin "stdin_prompt_short_o_EXPECT_STDIN_OK" "Reply exactly: STDIN_OK" \
  bin/ai-dispatch send gpt5.5 --json-result -o "$REPORT_DIR/stdin.md" --timeout 180 --activity-timeout 90

WORKDIR="$REPORT_DIR/workdir"
mkdir -p "$WORKDIR"
printf 'needle-from-cwd\n' >"$WORKDIR/needle.txt"
run_case "cwd_codex_file_read_EXPECT_CWD_OK" \
  bin/ai-dispatch send gpt5.5 "Read needle.txt in cwd and reply exactly: CWD_OK" \
    --cwd "$WORKDIR" --json-result --timeout 180 --activity-timeout 90

run_case "opencode_explicit_model_m_flag_EXPECT_OPENCODE_M_OK" \
  bin/ai-dispatch send opencode "Reply exactly: OPENCODE_M_OK" \
    -m openrouter/moonshotai/kimi-k2.7-code --json-result --timeout 180 --activity-timeout 90

run_case "claude_explicit_model_flag_EXPECT_CLAUDE_MODEL_OK" \
  bin/ai-dispatch send claude "Reply exactly: CLAUDE_MODEL_OK" \
    --model sonnet --json-result --timeout 120 --activity-timeout 60

run_case "task_name_metadata_EXPECT_CALLER_OK" \
  bin/ai-dispatch send gpt5.5 "Reply exactly: CALLER_OK" \
    --json-result --timeout 180 --activity-timeout 90 --task-name "stress-metadata"

run_case "timeout_short_EXPECT_TIMEOUT" \
  bin/ai-dispatch send gpt5.5 "Sleep for 10 seconds then reply exactly: SHOULD_NOT_FINISH" \
    --json-result --timeout 1 --activity-timeout 90

run_case "activity_timeout_short_EXPECT_TIMEOUT" \
  bin/ai-dispatch send gpt5.5 "Wait silently for 10 seconds then reply exactly: SHOULD_NOT_FINISH" \
    --json-result --timeout 0 --activity-timeout 1

SESSION="$(
  node -e '
const fs = require("fs");
const rows = fs.readFileSync(process.argv[1], "utf8").trim().split(/\n/).filter(Boolean).map(JSON.parse);
const row = rows.find((item) => item.name.startsWith("provider_opencode_mimo-openrouter-pro") && item.session_id);
process.stdout.write(row ? row.session_id : "");
' "$REPORT_DIR/summary.jsonl"
)"
if [[ -n "$SESSION" ]]; then
  run_case "resume_opencode_session_EXPECT_RESUME_OK" \
    bin/ai-dispatch resume --session-id "$SESSION" --session-provider opencode \
      "Reply exactly: RESUME_OK" --json-result --timeout 180 --activity-timeout 90
else
  printf '[stress] SKIP resume_opencode_session no session\n'
fi

run_case "resume_missing_EXPECT_ERROR" \
  bin/ai-dispatch resume --session-id missing-session-id "Reply exactly: RESUME_MISSING" \
    --target gpt5.5 --json-result --timeout 1

run_case "canonical_send_EXPECT_OK" \
  bin/ai-dispatch send gpt5.5 "Reply exactly: CANONICAL_OK" --json-result --timeout 180 --activity-timeout 90

run_case "runs_list" bin/ai-dispatch runs list --status success --limit 5
run_case "runs_list_filters" bin/ai-dispatch runs list --target gpt5.5 --task-name "stress-*" --limit 10

if [[ "${AI_DISPATCH_SKIP_AGY:-off}" == "on" ]]; then
  printf '[stress] SKIP antigravity provider cases because AI_DISPATCH_SKIP_AGY=on\n'
else
  for model in flash pro pro-low; do
    marker="AGY_${model//-/_}_OK"
    run_case "antigravity_${model}_EXPECT_${marker}" \
      bin/ai-dispatch send antigravity "Reply exactly: ${marker}" \
        --model "$model" --json-result --stream-progress --timeout 240 --activity-timeout 120
  done
  run_case "antigravity_bad_model_EXPECT_ERROR" \
    bin/ai-dispatch send antigravity "hello" --model unknown-model --json-result --timeout 60 --activity-timeout 30
fi

node - "$REPORT_DIR/summary.jsonl" <<'NODE'
const fs = require("fs");
const summaryPath = process.argv[2];
const rows = fs.readFileSync(summaryPath, "utf8").trim().split(/\n/).filter(Boolean).map(JSON.parse);
const failures = rows.filter((row) => {
  const expected = (row.name.match(/EXPECT_([A-Z0-9_]+)/) || [])[1] || "";
  const expectsFailure = expected === "ERROR" || expected === "TIMEOUT";
  if (expectsFailure) return row.code === 0 || row.ok === true;
  if (row.marker_ok === false) return true;
  return row.code !== 0 || row.ok === false;
});
const byProvider = {};
for (const row of rows) byProvider[row.provider || "none"] = (byProvider[row.provider || "none"] || 0) + 1;
const report = {
  report_dir: summaryPath.replace(/\/summary\.jsonl$/, ""),
  total: rows.length,
  failures: failures.length,
  byProvider,
  failure_names: failures.map((row) => row.name),
};
fs.writeFileSync(summaryPath.replace(/summary\.jsonl$/, "summary.json"), JSON.stringify(report, null, 2) + "\n");
console.log(JSON.stringify(report, null, 2));
NODE
printf '[stress] done report_dir=%s\n' "$REPORT_DIR"
