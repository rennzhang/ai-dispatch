# ai-dispatch Architecture

`ai-dispatch` is a provider CLI runtime, not an agent framework. It sends one request to a local provider CLI and returns a structured result.

## Public Entry

```bash
~/.ai-dispatch/bin/ai-dispatch send <target> "prompt" --json-result --stream-progress
```

Development entry:

```bash
go run ./cmd/ai-dispatch send <target> "prompt" --json-result
```

## Package Boundaries

```text
cmd/ai-dispatch/              # binary main
internal/cli/                 # CLI parsing and subcommands
internal/config/              # ~/.ai-dispatch config and state paths
internal/contract/            # request/result/progress contract
internal/dispatch/            # single request orchestration
internal/routing/             # target/provider/model resolution
internal/runtime/             # subprocess, timeout, process group
internal/providers/codex/     # Codex adapter
internal/providers/opencode/  # OpenCode adapter
internal/providers/claude/    # Claude print + PTY adapter
internal/providers/antigravity/ # agy adapter
internal/output/              # stdout/json/output-file/frontmatter
internal/runstore/            # runs list/show/tail
```

## Flow

```text
ai-dispatch send <target>
  -> internal/cli
  -> internal/dispatch
  -> internal/routing
  -> internal/providers/<provider>
  -> internal/runtime
  -> internal/output + internal/runstore
```

## State

Default state lives under `~/.ai-dispatch`:

```text
config.json
bin/ai-dispatch
runs/
cache/
logs/
hooks/
```

## Provider Rules

- Unsupported providers fail closed.
- Runtime/tool/task failures are not hidden by fallback.
- Claude defaults to `claude -p`; PTY is explicit config or provider option.
- Model routing uses the bundled registry unless `AI_DISPATCH_MODEL_REGISTRY` or `models.registry_path` overrides it.

## Validation

```bash
go test ./...
scripts/go_active_caller_check.sh
scripts/go_provider_smoke.sh
```

Provider changes also need real provider smoke or a documented external blocker.
