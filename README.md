# ai-dispatch

`ai-dispatch` is a thin local runtime for dispatching work to installed AI coding CLIs.

It does not try to be an agent framework. It standardizes the parts that every caller otherwise reimplements:

- target and model resolution
- provider command construction
- timeouts and activity timeouts
- structured JSON results
- session resume
- run history
- progress events
- skill-based agent usage

Supported providers:

| Target | Backing CLI |
| --- | --- |
| `codex`, `gpt5.5` | `codex exec` |
| `claude`, `sonnet`, `opus` | `claude -p` by default; optional PTY mode |
| `opencode`, `mimo`, `kimi`, OpenRouter models | `opencode run` |
| `antigravity`, `gemini`, `gemini-flash`, `gemini-pro` | `agy --print` |

## Install

From a checkout:

```bash
scripts/install.sh
```

This installs:

```text
~/.ai-dispatch/bin/ai-dispatch
~/.ai-dispatch/config.json
~/.ai-dispatch/runs/
~/.ai-dispatch/cache/
~/.ai-dispatch/logs/
```

It also installs the bundled skill into the common local skill directories:

```text
~/.codex/skills/ai-dispatch
~/.claude/skills/ai-dispatch
```

You can run the steps manually:

```bash
go build -o ~/.ai-dispatch/bin/ai-dispatch ./cmd/ai-dispatch
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
~/.ai-dispatch/bin/ai-dispatch skill install --target all
```

## Quick Use

```bash
~/.ai-dispatch/bin/ai-dispatch send gpt5.5 "Reply exactly: OK" --json-result
~/.ai-dispatch/bin/ai-dispatch models resolve sonnet --format json
~/.ai-dispatch/bin/ai-dispatch runs list --limit 20
```

For project work:

```bash
~/.ai-dispatch/bin/ai-dispatch send gpt5.5 \
  "Fix the failing test" \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --task-name fix-test
```

For resume:

```bash
~/.ai-dispatch/bin/ai-dispatch resume \
  --session-id <session-id> \
  "Continue with the next failing case" \
  --json-result \
  --stream-progress \
  --task-name fix-test-r2
```

## Config

Initialize once:

```bash
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
```

Config lives at `~/.ai-dispatch/config.json`.

```json
{
  "version": 1,
  "claude_transport": "print",
  "trusted_workspace": false,
  "models": {
    "registry_path": ""
  },
  "hooks": {
    "notify_command": ""
  }
}
```

Claude transport:

- `print`: default; use `claude -p`.
- `pty`: use the PTY driver for subscription/local interactive setups.
- `auto`: use `print` when Anthropic API environment variables exist, otherwise `pty`.
- `disabled`: fail closed for Claude targets.

Model routing uses the bundled registry by default. Override with `AI_DISPATCH_MODEL_REGISTRY` or `models.registry_path` only when needed.

## Skill

The bundled skill is the intended agent-facing interface. It tells agents to call:

```bash
~/.ai-dispatch/bin/ai-dispatch
```

Install or refresh it with:

```bash
~/.ai-dispatch/bin/ai-dispatch skill install --target all
```

Use `--target codex` or `--target claude` to install one side only.

## Result Contract

Use `--json-result` for machine callers. Important fields:

- `ok`
- `status`
- `text`
- `provider_used`
- `model_used`
- `requested_target`
- `route_trace`
- `degraded`
- `degrade_reason`
- `session_id`
- `next_action`
- `failure_class`

Callers must trust `provider_used` and `model_used`, not the requested target.

## Safety

Some provider CLIs can edit files or run shell commands. Use ai-dispatch only in workspaces where that behavior is intended.

Do not send prompt text, response text, secrets, full stderr, or personal paths through notification hooks.

## Development

```bash
go test ./...
scripts/go_active_caller_check.sh
```

Real provider smoke:

```bash
scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_CLAUDE=on scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_AGY=on scripts/go_agy_stress.sh
```

Full stress with real providers:

```bash
scripts/go_full_matrix_stress.sh
```
