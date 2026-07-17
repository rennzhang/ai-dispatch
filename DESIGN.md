# ai-dispatch Architecture

`ai-dispatch` is a provider CLI runtime, not an agent framework. It sends one request to a local provider CLI and returns a structured result.

## Public Entry

```bash
ai-dispatch send <target> "prompt" --json-result --stream-progress
```

The stable installed CLI lives at `~/.ai-dispatch/bin/ai-dispatch` and is usually
linked as `ai-dispatch` from `~/.local/bin`.

Installed skill entries are light Agent-facing wrappers. They can use their
bundled release binary or download the binary matching their `VERSION` into the
same local runtime cache when needed.

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
preferences.md
bin/ai-dispatch
bin/ai-dispatch-go-<version>-<platform>
runs/
logs/
```

## Provider Rules

- Unsupported providers fail closed.
- Runtime/tool/task failures are not hidden by fallback.
- Claude defaults to `claude -p`; PTY is explicit config or provider option.
- Model routing uses `config.json` `models` first, then the bundled registry, then provider inference.

## Feature Designs

- [Reasoning Effort](docs/reasoning-effort-design.md): reviewed cross-provider effort contract; omitted/auto preserves CLI defaults, and unsupported explicit levels fall back to auto without silent downshifting.

## Validation

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
go vet ./...
git diff --check
scripts/go_active_caller_check.sh
scripts/release.sh
scripts/go_provider_smoke.sh
```

Provider changes also need real provider smoke or a documented external blocker.
