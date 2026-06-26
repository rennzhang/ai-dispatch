# Configuration

Run once after install:

```bash
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
```

Config lives at `~/.ai-dispatch/config.json`.

Minimal fields:

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

`claude_transport` values:

- `print`: default; run Claude with `claude -p`.
- `pty`: run Claude through the PTY driver for subscription/local interactive setups.
- `auto`: choose `print` when Anthropic API environment variables exist, otherwise `pty`.
- `disabled`: fail closed for Claude targets.

Set `trusted_workspace` only for agent-controlled workspaces where provider CLIs may edit files.

Model registry:

- By default ai-dispatch uses its bundled model registry.
- Set `models.registry_path` or `AI_DISPATCH_MODEL_REGISTRY` only when you need a local override.
