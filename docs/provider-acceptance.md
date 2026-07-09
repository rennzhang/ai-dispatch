# Provider Acceptance

Provider acceptance is the reusable real-provider gate for `ai-dispatch`.
Use it when adding a new provider adapter, changing provider command
construction, changing routing/fallback behavior, or doing release-level
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
| `AI_DISPATCH_ACCEPTANCE_MODEL` | Expected `model_used`; leave empty only when the provider intentionally reports no model |

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
| `AI_DISPATCH_ACCEPTANCE_FALLBACK_ENV` | Environment variable used to make the native provider unavailable |
| `AI_DISPATCH_ACCEPTANCE_FALLBACK_PROVIDER` | Expected fallback `provider_used` |
| `AI_DISPATCH_ACCEPTANCE_FALLBACK_MODEL` | Expected fallback `model_used` |
| `AI_DISPATCH_ACCEPTANCE_INVALID_PROVIDER_OPT` | Invalid provider option that must fail with `failure_class=input` |
| `AI_DISPATCH_ACCEPTANCE_INVALID_MODEL` | Invalid model override that must fail with `failure_class=input` |
| `AI_DISPATCH_ACCEPTANCE_UNSUPPORTED_TARGETS` | Comma-separated targets that must fail, useful for removed aliases |

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
2. `providers scan --format json`
3. `models resolve <target> --format json`
4. Removed or unsupported target resolution failures
5. Native `send`
6. Native `resume`
7. Explicit `--model`
8. Variant target
9. `--prompt-file`
10. `--output-file`
11. Prompt from stdin
12. `--cwd` with a temporary file read
13. Configured fallback candidate
14. Invalid provider option
15. Invalid model override
16. `runs list` for the generated task prefix

For successful provider calls, the harness checks the expected provider/model,
the marker text, and required metadata such as `session_id` for native send.
For fallback, it checks `degraded=true` and at least two route steps.

## Grok Example

`scripts/go_grok_stress.sh` is a thin wrapper around the generic harness:

```bash
scripts/go_grok_stress.sh
```

It sets:

```text
AI_DISPATCH_ACCEPTANCE_TARGET=grok
AI_DISPATCH_ACCEPTANCE_PROVIDER=grok
AI_DISPATCH_ACCEPTANCE_MODEL=grok-4.5
AI_DISPATCH_ACCEPTANCE_VARIANT_TARGET=grok-fast
AI_DISPATCH_ACCEPTANCE_VARIANT_MODEL=grok-composer-2.5-fast
AI_DISPATCH_ACCEPTANCE_FALLBACK_PROVIDER=opencode
AI_DISPATCH_ACCEPTANCE_FALLBACK_MODEL=openrouter/x-ai/grok-4.5
```

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

Do not claim provider support from `providers scan` alone. Scan proves that the
CLI looks present and can report a version; acceptance proves that the provider
works through `send`/`resume` and returns the `ProviderResult` contract.
