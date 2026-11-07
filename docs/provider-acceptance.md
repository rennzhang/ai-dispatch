# Provider Acceptance

Provider acceptance is the reusable real-provider gate for `ai-dispatch`.
Use it when adding a new provider adapter, changing provider command
construction, changing routing or fail-closed behavior, or doing release-level
regression on an existing provider.

This gate complements unit tests. Unit tests prove adapter contracts with fake
binaries; provider acceptance proves the installed local provider CLI can work
through the public `ai-dispatch` command path.

## Quick Start

Run the generic harness:

```bash
AI_DISPATCH_ACCEPTANCE_TARGET=<target> \
AI_DISPATCH_ACCEPTANCE_PROVIDER=<provider> \
AI_DISPATCH_ACCEPTANCE_MODEL=<model> \
scripts/go_provider_acceptance.sh
```

Run a provider-specific wrapper when one exists:

```bash
scripts/go_grok_stress.sh
```

The harness writes a report directory and exits non-zero if any required case
fails. The final summary is saved as:

```text
<report_dir>/summary.json
<report_dir>/summary.jsonl
```

The harness requires `node` for JSON summary generation.
By default it tests `bin/ai-dispatch`, the development wrapper that auto-builds
the Go binary from source. Set `AI_DISPATCH_BIN` when testing an installed or
release binary instead.

If `AI_DISPATCH_ACCEPTANCE_REPORT_DIR` points at an existing directory, the
harness replaces `summary.jsonl` and `summary.json` at startup so old summary
rows cannot pollute a new run.

## Required Variables

| Variable | Meaning |
| --- | --- |
| `AI_DISPATCH_ACCEPTANCE_TARGET` | User-facing target passed to `ai-dispatch send`, for example `grok` |
| `AI_DISPATCH_ACCEPTANCE_PROVIDER` | Expected `provider_used` in successful native results |
| `AI_DISPATCH_ACCEPTANCE_MODEL` | Main model, passed explicitly as `--model` and checked against `model_used`; leave empty to exercise the provider's native default without asserting its model ID |

## Optional Variables

| Variable | Meaning |
| --- | --- |
| `AI_DISPATCH_BIN` | Command under test; defaults to `bin/ai-dispatch` |
| `AI_DISPATCH_ACCEPTANCE_REPORT_DIR` | Existing or new report directory |
| `AI_DISPATCH_ACCEPTANCE_TIMEOUT` | Wall-clock timeout per provider case; defaults to `240` |
| `AI_DISPATCH_ACCEPTANCE_ACTIVITY_TIMEOUT` | Inactivity timeout per provider case; defaults to `120` |
| `AI_DISPATCH_ACCEPTANCE_TASK_PREFIX` | Prefix used for runstore task names |
| `AI_DISPATCH_ACCEPTANCE_RESUME_TARGET` | Target used for `resume --session-provider`; defaults to the main target |
| `AI_DISPATCH_ACCEPTANCE_PROVIDER_OPTS` | Comma-separated provider opts, for example `grok.max-turns=1,grok.web-search=off` |
| `AI_DISPATCH_ACCEPTANCE_TOOL_PROVIDER_OPTS` | Provider opts for the cwd/tool-use case; defaults to normal provider opts |
| `AI_DISPATCH_ACCEPTANCE_VARIANT_TARGET` | Optional same-provider variant target, for example `grok-fast` |
| `AI_DISPATCH_ACCEPTANCE_VARIANT_PROVIDER` | Expected provider for the variant target; defaults to the main provider |
| `AI_DISPATCH_ACCEPTANCE_VARIANT_MODEL` | Expected model for the variant target |
| `AI_DISPATCH_ACCEPTANCE_FAIL_CLOSED_ENV` | Selected provider's binary override environment variable; the harness points it at a nonexistent executable and verifies failure without changing provider or model |
| `AI_DISPATCH_ACCEPTANCE_INVALID_PROVIDER_OPT` | Invalid provider option that must fail with `failure_class=input` |
| `AI_DISPATCH_ACCEPTANCE_INVALID_MODEL` | Adapter-incompatible model override that dispatch must reject with `failure_class=input` before generation; an unlisted native model ID alone is not invalid |
| `AI_DISPATCH_ACCEPTANCE_UNSUPPORTED_TARGETS` | Comma-separated targets that must fail, useful for removed aliases |

The retired `AI_DISPATCH_ACCEPTANCE_FALLBACK_*` variables are rejected before
running cases. Use `AI_DISPATCH_ACCEPTANCE_FAIL_CLOSED_ENV` instead; old options
must not silently skip the selected-provider failure check.

## Skip Variables

Skips are allowed only when a provider genuinely does not support a capability
or when an external account/service blocks the real smoke. Keep the skip
explicit in the command or PR notes.

| Variable | Skipped case |
| --- | --- |
| `AI_DISPATCH_ACCEPTANCE_SKIP_RESUME=on` | Resume |
| `AI_DISPATCH_ACCEPTANCE_SKIP_PROMPT_FILE=on` | `--prompt-file` and `--output-file` |
| `AI_DISPATCH_ACCEPTANCE_SKIP_STDIN=on` | Prompt from stdin |
| `AI_DISPATCH_ACCEPTANCE_SKIP_CWD=on` | `--cwd` file-read case |

## Covered Cases

The harness runs these cases when not skipped:

1. `doctor --format json`
2. `providers scan --dry-run --format json` (does not update the runtime config)
3. `models resolve <target> [--model <model>] --format json`
4. Removed or unsupported target resolution failures
5. Native `send`
6. Native `resume`
7. Explicit `--model`
8. Explicitly configured variant target (optional)
9. `--prompt-file`
10. `--output-file`
11. Prompt from stdin
12. `--cwd` with a temporary file read
13. Selected provider unavailable: fail closed
14. Invalid provider option
15. Invalid model override
16. `runs list` for the generated task prefix

For successful provider calls, the harness checks the expected provider/model,
the marker text, and required metadata such as `session_id` for native send.
For the fail-closed case, the harness overrides the selected provider's binary
path with a nonexistent executable. It requires a non-zero exit, `ok=false`,
the original `provider_used` and selected `model_used`, `degraded=false`, and
exactly one route step with that same provider and model. A successful call to
another provider is a failure of this gate, not recovery.
The receipt must explicitly contain boolean `ok` and `degraded` fields and an
array of route steps; absent fields are not inferred from a failing exit code.

## Grok Example

`scripts/go_grok_stress.sh` is a thin wrapper around the generic harness:

```bash
scripts/go_grok_stress.sh
```

It sets:

```text
AI_DISPATCH_ACCEPTANCE_TARGET=grok
AI_DISPATCH_ACCEPTANCE_PROVIDER=grok
AI_DISPATCH_ACCEPTANCE_MODEL=grok-4.7
AI_DISPATCH_ACCEPTANCE_FAIL_CLOSED_ENV=AI_DISPATCH_GROK_BIN
AI_DISPATCH_ACCEPTANCE_INVALID_MODEL=openrouter/x-ai/grok-4.7
```

No variant target or variant model is selected by default. Set both explicitly
when a configured same-provider variant is part of the acceptance scope.

The invalid-model case uses an OpenRouter selector because the native Grok
adapter deterministically rejects that format before calling Grok Build. This
does not turn the scanned model directory into an allowlist: an unlisted native
Grok model ID is still passed through, and its real availability is decided by
Grok Build.

Grok text-only cases use `grok.max-turns=1`. The cwd file-read case uses
`grok.max-turns=3` because reading a file takes a tool turn and a final answer
turn.

## PR Expectations

For provider changes, include:

- Unit tests with fake binaries or fake provider outputs.
- `AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...`.
- `go vet ./...`.
- `git diff --check`.
- `scripts/go_active_caller_check.sh`.
- A real provider acceptance run, or a clear external blocker such as missing
  auth, subscription, quota, region access, or provider outage.

Do not claim generation support from `providers scan` alone. Scan discovers
local execution entries and model catalogs, and reports missing or failed
discovery separately. A listed model does not prove account access, quota, or a
successful generation. Real acceptance verifies that the selected provider and
model work through `send`/`resume` and return the `ProviderResult` contract.
