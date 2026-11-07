#!/usr/bin/env bash
set -euo pipefail

# Test the acceptance harness itself; these fixtures do not certify a provider.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d -t ai-dispatch-harness.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/dispatch" <<'NODE'
#!/usr/bin/env node
const args = process.argv.slice(2);
let value = { ok: true };
let code = 0;
if (args[0] === 'models') value = { provider: 'grok', model: 'grok-4.7' };
if (args[0] === 'send') {
  const step = { provider: 'grok', model: 'grok-4.7' };
  value = {
    ok: true, degraded: false, provider_used: 'grok', model_used: 'grok-4.7',
    session_id: 'harness-session', route_steps: [step],
    text: (args.join(' ').match(/Reply exactly: (\w+)/) || [])[1] || '',
  };
  if (process.env.AI_DISPATCH_GROK_BIN) {
    code = 1;
    value.ok = false;
    if (process.env.RECEIPT === 'missing-booleans') {
      delete value.ok;
      delete value.degraded;
    }
    if (process.env.RECEIPT === 'wrong-step-model') step.model = 'wrong-model';
    if (process.env.RECEIPT === 'arraylike-steps') value.route_steps = { 0: step, length: 1 };
  }
}
console.log(JSON.stringify(value));
process.exit(code);
NODE
chmod +x "$WORK/dispatch"

run_harness() {
  local receipt="$1" model="$2" expected_code="$3"
  local report="$WORK/$receipt-${model:-empty}"
  local actual_code=0
  env -u AI_DISPATCH_GROK_BIN \
    AI_DISPATCH_BIN="$WORK/dispatch" \
    AI_DISPATCH_ACCEPTANCE_TARGET=grok AI_DISPATCH_ACCEPTANCE_PROVIDER=grok \
    AI_DISPATCH_ACCEPTANCE_MODEL="$model" \
    AI_DISPATCH_ACCEPTANCE_REPORT_DIR="$report" \
    AI_DISPATCH_ACCEPTANCE_FAIL_CLOSED_ENV=AI_DISPATCH_GROK_BIN \
    AI_DISPATCH_ACCEPTANCE_SKIP_RESUME=on AI_DISPATCH_ACCEPTANCE_SKIP_CWD=on \
    AI_DISPATCH_ACCEPTANCE_SKIP_PROMPT_FILE=on AI_DISPATCH_ACCEPTANCE_SKIP_STDIN=on \
    RECEIPT="$receipt" \
    /bin/bash "$ROOT/scripts/go_provider_acceptance.sh" >"$report.log" 2>&1 || actual_code=$?
  if [[ "$actual_code" -ne "$expected_code" ]]; then
    cat "$report.log" >&2
    echo "harness $receipt: expected exit $expected_code, got $actual_code" >&2
    exit 1
  fi
}

# /bin/bash also exercises macOS's Bash 3.2 with empty option/model arrays.
run_harness valid grok-4.7 0
run_harness valid '' 0
run_harness missing-booleans grok-4.7 1
run_harness wrong-step-model grok-4.7 1
run_harness wrong-step-model '' 1
run_harness arraylike-steps grok-4.7 1

retired_code=0
AI_DISPATCH_ACCEPTANCE_TARGET=grok AI_DISPATCH_ACCEPTANCE_PROVIDER=grok \
  AI_DISPATCH_ACCEPTANCE_FALLBACK_PROVIDER=opencode \
  /bin/bash "$ROOT/scripts/go_provider_acceptance.sh" >"$WORK/retired.log" 2>&1 || retired_code=$?
if [[ "$retired_code" -ne 2 ]] || ! grep -q 'is retired' "$WORK/retired.log"; then
  cat "$WORK/retired.log" >&2
  echo 'harness: retired fallback configuration was not rejected' >&2
  exit 1
fi
echo 'provider acceptance harness PASS (7 scenarios)'
