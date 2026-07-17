#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -n "${AI_DISPATCH_ACCEPTANCE_ROOT:-}" ]]; then
  WORK="$AI_DISPATCH_ACCEPTANCE_ROOT"
  case "$WORK" in
    /|"$HOME"|"$ROOT")
      echo "distribution acceptance: unsafe artifact root: $WORK" >&2
      exit 1
      ;;
  esac
  if [[ -e "$WORK" && ! -d "$WORK" ]]; then
    echo "distribution acceptance: artifact root is not a directory: $WORK" >&2
    exit 1
  fi
  if [[ -d "$WORK" && -n "$(find "$WORK" ! -path "$WORK" -print -quit)" ]]; then
    echo "distribution acceptance: artifact root must be empty: $WORK" >&2
    exit 1
  fi
  mkdir -p "$WORK"
  keep_work=1
else
  WORK="$(mktemp -d -t ai-dispatch-distribution.XXXXXX)"
  keep_work=0
fi

active_wrapper_pid=""
active_provider_pid=""
active_provider_child_pid=""

cleanup() {
  set +e
  if [[ -n "$active_wrapper_pid" ]]; then
    kill -TERM "$active_wrapper_pid" 2>/dev/null || true
    for _ in {1..100}; do
      if ! kill -0 "$active_wrapper_pid" 2>/dev/null; then
        break
      fi
      sleep 0.02
    done
    kill -KILL "$active_wrapper_pid" 2>/dev/null || true
    wait "$active_wrapper_pid" 2>/dev/null || true
  fi
  for pid in "$active_provider_pid" "$active_provider_child_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
  if [[ "$keep_work" == "0" ]]; then
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

CANDIDATE="${AI_DISPATCH_CANDIDATE_BIN:-$WORK/ai-dispatch-go}"
if [[ -z "${AI_DISPATCH_CANDIDATE_BIN:-}" ]]; then
  (
    cd "$ROOT"
    version="$(tr -d '[:space:]' <"$ROOT/skills/ai-dispatch/VERSION")"
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-buildid= -X github.com/rennzhang/ai-dispatch/internal/buildinfo.versionOverride=$version" \
      -o "$CANDIDATE" ./cmd/ai-dispatch
  )
fi
if [[ ! -x "$CANDIDATE" ]]; then
  echo "distribution acceptance: candidate binary is not executable: $CANDIDATE" >&2
  exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "distribution acceptance: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac
case "$OS" in
  darwin|linux) ;;
  *) echo "distribution acceptance: unsupported OS: $OS" >&2; exit 1 ;;
esac

package_name="ai-dispatch-$OS-$ARCH"
package_root="$WORK/package/$package_name"
mkdir -p "$WORK/extracted"
if [[ -n "${AI_DISPATCH_PACKAGE_TARBALL:-}" ]]; then
  if [[ ! -f "$AI_DISPATCH_PACKAGE_TARBALL" ]]; then
    echo "distribution acceptance: package tarball not found: $AI_DISPATCH_PACKAGE_TARBALL" >&2
    exit 1
  fi
  cp "$AI_DISPATCH_PACKAGE_TARBALL" "$WORK/$package_name.tar.gz"
else
  mkdir -p "$package_root"
  cp -R "$ROOT/skills/ai-dispatch/." "$package_root/"
  cp "$ROOT/LICENSE" "$package_root/LICENSE"
  cp "$CANDIDATE" "$package_root/scripts/ai-dispatch-go"
  chmod +x "$package_root/scripts/ai-dispatch" "$package_root/scripts/ai-dispatch-go"
  tar -C "$WORK/package" -czf "$WORK/$package_name.tar.gz" "$package_name"
fi
tar -xzf "$WORK/$package_name.tar.gz" -C "$WORK/extracted"
extracted="$WORK/extracted/$package_name"
if [[ ! -x "$extracted/scripts/ai-dispatch-go" ]] || \
  ! cmp -s "$CANDIDATE" "$extracted/scripts/ai-dispatch-go"; then
  echo "distribution acceptance: package binary differs from candidate" >&2
  exit 1
fi
release_version="$(tr -d '[:space:]' <"$extracted/VERSION")"
if [[ -z "$release_version" || "$release_version" == "dev" ]]; then
  echo "distribution acceptance: invalid package VERSION: $release_version" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "distribution acceptance: no SHA256 tool found" >&2
    return 1
  fi
}

mkdir -p "$WORK/path" "$WORK/fallback" "$WORK/homebrew/bin" "$WORK/npm-pack" "$WORK/npm-project"

release_dir="$WORK/local-release/download/$release_version"
mkdir -p "$release_dir" "$WORK/installer-home/.codex/skills/ai-dispatch" \
  "$WORK/installer-home/.claude/skills/ai-dispatch" "$WORK/path-runtime"
cp "$WORK/$package_name.tar.gz" "$release_dir/"
cp "$CANDIDATE" "$release_dir/$package_name.bin"
printf 'stale\n' >"$WORK/installer-home/.codex/skills/ai-dispatch/stale"
printf 'stale\n' >"$WORK/installer-home/.claude/skills/ai-dispatch/stale"
printf '%s\n' '{"sentinel":"config-preserved"}' >"$WORK/path-runtime/config.json"
printf '%s\n' 'preferences-preserved' >"$WORK/path-runtime/preferences.md"
config_sha_before="$(sha256_file "$WORK/path-runtime/config.json")"
preferences_sha_before="$(sha256_file "$WORK/path-runtime/preferences.md")"
{
  printf '%s  %s\n' "$(sha256_file "$WORK/$package_name.tar.gz")" "$package_name.tar.gz"
  printf '%s  %s\n' "$(sha256_file "$CANDIDATE")" "$package_name.bin"
} >"$release_dir/SHA256SUMS"
installer_allow_dirty=1
if [[ "${AI_DISPATCH_REQUIRE_RELEASE_IDENTITY:-0}" == "1" ]]; then
  installer_allow_dirty=0
fi
env -i \
  HOME="$WORK/installer-home" \
  PATH="$PATH" \
  TMPDIR="${TMPDIR:-/tmp}" \
  AI_DISPATCH_HOME="$WORK/path-runtime" \
  AI_DISPATCH_LINK_DIR="$WORK/path" \
  AI_DISPATCH_SKILL_TARGET=all \
  AI_DISPATCH_VERSION="$release_version" \
  AI_DISPATCH_ALLOW_DIRTY_LOCAL="$installer_allow_dirty" \
  AI_DISPATCH_RELEASE_BASE_URL="file://$WORK/local-release" \
  bash "$ROOT/scripts/install-remote.sh" >"$WORK/install-remote.log"
path_binary="$WORK/path-runtime/bin/ai-dispatch-go-$release_version-$OS-$ARCH"
if [[ "$(sha256_file "$WORK/path-runtime/config.json")" != "$config_sha_before" || \
  "$(sha256_file "$WORK/path-runtime/preferences.md")" != "$preferences_sha_before" ]]; then
  echo "distribution acceptance: installer changed config or preferences" >&2
  exit 1
fi
if [[ -e "$WORK/installer-home/.codex/skills/ai-dispatch/stale" || \
  -e "$WORK/installer-home/.claude/skills/ai-dispatch/stale" ]]; then
  echo "distribution acceptance: installer did not replace stale skill content" >&2
  exit 1
fi

codex_skill="$WORK/installer-home/.codex/skills/ai-dispatch"
claude_skill="$WORK/installer-home/.claude/skills/ai-dispatch"
cp -R "$ROOT/skills/ai-dispatch/." "$WORK/fallback/"

# Exercise the formula's bin.install source/destination layout without mutating
# the developer machine's Homebrew Cellar. release.sh separately renders and
# validates the real formula against the exact release checksums.
cp "$extracted/scripts/ai-dispatch-go" "$WORK/homebrew/bin/ai-dispatch"
chmod +x "$WORK/homebrew/bin/ai-dispatch"

mkdir -p "$WORK/npm-home"
npm_tarball="$(
  (
    cd "$ROOT/npm/ai-dispatch"
    env -i HOME="$WORK/npm-home" PATH="$PATH" TMPDIR="${TMPDIR:-/tmp}" \
      npm pack --pack-destination "$WORK/npm-pack" --silent
  ) | tail -n 1
)"
(
  cd "$WORK/npm-project"
  env -i \
    HOME="$WORK/npm-home" \
    PATH="$PATH" \
    TMPDIR="${TMPDIR:-/tmp}" \
    AI_DISPATCH_RELEASE_BASE_URL="file://$WORK/local-release" \
    npm install --no-audit --no-fund "$WORK/npm-pack/$npm_tarball" >"$WORK/npm-install.log"
)

for staged_binary in \
  "$path_binary" \
  "$codex_skill/scripts/ai-dispatch-go" \
  "$claude_skill/scripts/ai-dispatch-go" \
  "$WORK/homebrew/bin/ai-dispatch" \
  "$WORK/npm-project/node_modules/ai-dispatch/bin/native/ai-dispatch-go"; do
  if ! cmp -s "$CANDIDATE" "$staged_binary"; then
    echo "distribution acceptance: staged binary differs from candidate: $staged_binary" >&2
    exit 1
  fi
done

entry_command() {
  local entry="$1"
  ENTRY_COMMAND=()
  case "$entry" in
    direct) ENTRY_COMMAND=("$CANDIDATE") ;;
    dev) ENTRY_COMMAND=("$ROOT/bin/ai-dispatch") ;;
    path) ENTRY_COMMAND=("$WORK/path/ai-dispatch") ;;
    codex) ENTRY_COMMAND=("$codex_skill/scripts/ai-dispatch") ;;
    claude) ENTRY_COMMAND=("$claude_skill/scripts/ai-dispatch") ;;
    fallback) ENTRY_COMMAND=("$WORK/fallback/scripts/ai-dispatch") ;;
    homebrew-layout) ENTRY_COMMAND=("$WORK/homebrew/bin/ai-dispatch") ;;
    npm) ENTRY_COMMAND=("$WORK/npm-project/node_modules/.bin/ai-dispatch") ;;
    *) echo "unknown acceptance entry: $entry" >&2; return 2 ;;
  esac
}

prepare_entry_environment() {
  local entry_root="$1"
  local user_home="$entry_root/home"
  local ai_home="$entry_root/ai-dispatch"
  mkdir -p "$user_home" "$ai_home" "$entry_root/xdg-config" "$entry_root/xdg-data" \
    "$entry_root/xdg-cache" "$entry_root/codex" "$entry_root/opencode"
  printf '%s\n' '{"version":1,"claude_transport":"print","models":{},"providers":{}}' >"$ai_home/config.json"
  printf '%s\n' '# acceptance preferences' >"$ai_home/preferences.md"
  ENTRY_AI_HOME="$ai_home"
  ENTRY_ENV=(
    HOME="$user_home"
    PATH="$PATH"
    TMPDIR="${TMPDIR:-/tmp}"
    LANG="${LANG:-C}"
    XDG_CONFIG_HOME="$entry_root/xdg-config"
    XDG_DATA_HOME="$entry_root/xdg-data"
    XDG_CACHE_HOME="$entry_root/xdg-cache"
    CODEX_HOME="$entry_root/codex"
    OPENCODE_CONFIG_DIR="$entry_root/opencode"
    AI_DISPATCH_HOME="$ai_home"
    AI_DISPATCH_RUNS_DIR="$ai_home/runs"
    AI_DISPATCH_GO_PROVIDER_EXECUTION=on
    AI_DISPATCH_RELEASE_BASE_URL="file://$WORK/local-release"
  )
}

validate_json() {
  local file="$1"
  local expression="$2"
  node - "$file" "$expression" <<'NODE'
const fs = require("fs");
const [file, expression] = process.argv.slice(2);
const value = JSON.parse(fs.readFileSync(file, "utf8"));
if (!Function("value", "return Boolean(" + expression + ")")(value)) {
  console.error(JSON.stringify(value));
  process.exit(1);
}
NODE
}

fake_provider="$WORK/fake-provider.sh"
cat >"$fake_provider" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${GROK_FAKE_ARGS_FILE:-}" ]]; then
  printf '%s\n' "$@" >"$GROK_FAKE_ARGS_FILE"
fi
if [[ "${GROK_FAKE_MODE:-success}" == "success" ]]; then
  printf '%s\n' '{"text":"distribution acceptance output","sessionId":"distribution-session"}'
  exit 0
fi
printf '%s\n' "$$" >"${GROK_FAKE_PID_FILE:?}"
sleep 3600 &
child=$!
printf '%s\n' "$child" >"${GROK_FAKE_CHILD_PID_FILE:?}"
trap 'kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true; exit 0' INT TERM HUP
wait "$child"
SH
chmod +x "$fake_provider"

wait_for_file() {
  local file="$1"
  local i
  for i in {1..500}; do
    [[ -s "$file" ]] && return 0
    sleep 0.02
  done
  echo "timed out waiting for $file" >&2
  return 1
}

wait_for_process_exit() {
  local pid="$1"
  local i
  for i in {1..500}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 0.02
  done
  return 1
}

for entry in direct dev path codex claude fallback homebrew-layout npm; do
  entry_root="$WORK/home-$entry"
  prepare_entry_environment "$entry_root"
  home="$ENTRY_AI_HOME"
  if [[ "$entry" == "fallback" ]]; then
    mkdir -p "$home/bin"
    fallback_binary="$home/bin/ai-dispatch-go-$release_version-$OS-$ARCH"
    cp "$CANDIDATE" "$fallback_binary"
    chmod +x "$fallback_binary"
    if ! cmp -s "$CANDIDATE" "$fallback_binary"; then
      echo "fallback: cached binary differs from candidate" >&2
      exit 1
    fi
  fi
  entry_command "$entry"

  env -i "${ENTRY_ENV[@]}" "${ENTRY_COMMAND[@]}" doctor --format json >"$WORK/$entry-doctor.json"
  validate_json "$WORK/$entry-doctor.json" \
    'value.ok === true && value.runtime === "go" && value.provider_execution === "enabled"'

  env -i "${ENTRY_ENV[@]}" "${ENTRY_COMMAND[@]}" version --format json >"$WORK/$entry-version.json"
  validate_json "$WORK/$entry-version.json" \
    'typeof value.version === "string" && value.version.length > 0 && typeof value.revision === "string" && value.revision.length >= 7 && typeof value.modified === "boolean" && typeof value.go_version === "string"'
  if [[ "$entry" == "direct" ]]; then
    cp "$WORK/$entry-version.json" "$WORK/canonical-version.json"
  elif ! cmp -s "$WORK/canonical-version.json" "$WORK/$entry-version.json"; then
    echo "$entry: build identity differs from the direct candidate" >&2
    exit 1
  fi

  env -i "${ENTRY_ENV[@]}" "${ENTRY_COMMAND[@]}" models resolve grok --format json >"$WORK/$entry-route.json"
  validate_json "$WORK/$entry-route.json" 'value.provider === "grok" && value.model === "grok-4.5"'

  output_file="$WORK/$entry-output.md"
  args_file="$WORK/$entry-provider.args"
  env -i "${ENTRY_ENV[@]}" \
    AI_DISPATCH_GROK_BIN="$fake_provider" \
    GROK_FAKE_MODE=success \
    GROK_FAKE_ARGS_FILE="$args_file" \
    "${ENTRY_COMMAND[@]}" send grok "distribution acceptance" \
      --output-file "$output_file" --json-result --timeout 10 --activity-timeout 5 \
      >"$WORK/$entry-send.json"
  validate_json "$WORK/$entry-send.json" \
    'value.ok === true && value.status === "success" && value.session_id === "distribution-session" && typeof value.output_file === "string" && value.output_file.length > 0'
  if ! grep -q 'distribution acceptance output' "$output_file"; then
    echo "$entry: output file does not contain provider result" >&2
    exit 1
  fi

  mkdir -p "$home/runs/run-corrupt"
  printf '%s\n' '{not-json' >"$home/runs/run-corrupt/run.json"
  env -i "${ENTRY_ENV[@]}" \
    "${ENTRY_COMMAND[@]}" runs list >"$WORK/$entry-runs.json" 2>"$WORK/$entry-runs.stderr"
  validate_json "$WORK/$entry-runs.json" \
    'Array.isArray(value) && value.some((record) => record.status === "success")'
  if ! grep -q 'skipped invalid run record run-corrupt' "$WORK/$entry-runs.stderr"; then
    echo "$entry: corrupt run was not isolated with a diagnostic" >&2
    exit 1
  fi

  env -i "${ENTRY_ENV[@]}" \
    AI_DISPATCH_GROK_BIN="$fake_provider" \
    GROK_FAKE_MODE=success \
    GROK_FAKE_ARGS_FILE="$args_file" \
    "${ENTRY_COMMAND[@]}" resume --session-id distribution-session \
      "continue distribution acceptance" --json-result --timeout 10 --activity-timeout 5 \
      >"$WORK/$entry-resume.json"
  validate_json "$WORK/$entry-resume.json" \
    'value.ok === true && value.session_id === "distribution-session" && value.provider_used === "grok"'
  if ! grep -qx -- '--resume' "$args_file" || ! grep -qx -- 'distribution-session' "$args_file"; then
    echo "$entry: resume did not preserve the provider session id" >&2
    exit 1
  fi

  pid_file="$WORK/$entry-provider.pid"
  child_pid_file="$WORK/$entry-provider-child.pid"
  set +e
  env -i "${ENTRY_ENV[@]}" \
    AI_DISPATCH_GROK_BIN="$fake_provider" \
    GROK_FAKE_MODE=signal \
    GROK_FAKE_PID_FILE="$pid_file" \
    GROK_FAKE_CHILD_PID_FILE="$child_pid_file" \
    "${ENTRY_COMMAND[@]}" send grok "signal acceptance" --timeout 0 --json-result \
      >"$WORK/$entry-signal.stdout" 2>"$WORK/$entry-signal.stderr" &
  wrapper_pid=$!
  active_wrapper_pid="$wrapper_pid"
  set -e
  wait_for_file "$pid_file"
  wait_for_file "$child_pid_file"
  provider_pid="$(cat "$pid_file")"
  provider_child_pid="$(cat "$child_pid_file")"
  active_provider_pid="$provider_pid"
  active_provider_child_pid="$provider_child_pid"
  kill -TERM "$wrapper_pid"
  if ! wait_for_process_exit "$wrapper_pid"; then
    echo "$entry: cancellation did not finish within 10 seconds" >&2
    exit 1
  fi
  set +e
  wait "$wrapper_pid"
  signal_code=$?
  set -e
  if [[ "$signal_code" -ne 130 ]]; then
    echo "$entry: expected normalized cancellation exit 130, got $signal_code" >&2
    exit 1
  fi
  if kill -0 "$provider_pid" 2>/dev/null; then
    echo "$entry: provider process survived cancellation: $provider_pid" >&2
    kill -KILL "$provider_pid" 2>/dev/null || true
    exit 1
  fi
  if kill -0 "$provider_child_pid" 2>/dev/null; then
    echo "$entry: provider child process survived cancellation: $provider_child_pid" >&2
    kill -KILL "$provider_child_pid" 2>/dev/null || true
    exit 1
  fi
  active_wrapper_pid=""
  active_provider_pid=""
  active_provider_child_pid=""
  printf '%s\tPASS\n' "$entry" >>"$WORK/summary.tsv"
done

printf 'distribution acceptance PASS (%s)\n' "$WORK"
