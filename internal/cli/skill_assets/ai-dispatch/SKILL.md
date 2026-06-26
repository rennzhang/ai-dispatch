---
name: ai-dispatch
description: Dispatch work to installed local AI coding CLIs through ai-dispatch. Use when an agent needs to call Codex, Claude, OpenCode/OpenRouter, or Antigravity/Gemini for implementation, review, research, model comparison, provider routing, session resume, or troubleshooting ai-dispatch runs.
---

# ai-dispatch

Use the installed binary, not a repository source path:

```bash
~/.ai-dispatch/bin/ai-dispatch send <target> "<task>" --cwd "$PWD" --json-result --stream-progress --task-name <name>
```

For larger prompts, write a prompt file and pass `--prompt-file`.

Use `resume` only with a real `session_id` returned by a previous result:

```bash
~/.ai-dispatch/bin/ai-dispatch resume --session-id <id> "<delta>" --json-result --stream-progress --task-name <name>-r2
```

Check run history before guessing:

```bash
~/.ai-dispatch/bin/ai-dispatch runs list --limit 20
~/.ai-dispatch/bin/ai-dispatch runs show <run-id>
~/.ai-dispatch/bin/ai-dispatch runs failures --since 24h
```

Rules:

- Always read `provider_used`, `model_used`, `route_trace`, `degraded`, and `degrade_reason`; do not infer actual execution from requested target.
- Use `--cwd` for project work.
- Use `--task-name` for anything long-running or reviewable.
- Do not paste previous turns into a fresh prompt when `session_id` resume is available.
- Do not expose prompt text, response text, secrets, or personal paths in external notifications.

Read references only when needed:

- `references/config.md` for init, config, provider, and trusted workspace behavior.
- `references/notifications.md` for notification hook integration.
- `references/provider-onboarding.md` for adding or changing providers/models.
