# Notification Hooks

ai-dispatch keeps notification integration generic. A hook command must accept event JSON on stdin and exit `0` on success.

Minimum event shape:

```json
{
  "source": "ai-dispatch",
  "event": "failure|timeout|fallback|complete",
  "run_id": "20260626-120000.000",
  "task_name": "review-r1",
  "provider_used": "codex",
  "model_used": "gpt-5.5",
  "status": "timeout",
  "failure_class": "timeout"
}
```

Do not include prompt text, response text, secrets, full stderr, personal paths, or raw config in notification payloads.

Known local integrations:

- Telegram: implement a hook that reads stdin JSON and sends a short status message to the configured chat.
- Feishu/Lark: implement a hook that reads stdin JSON and sends a short card or text message to the configured recipient.

For other platforms, build the same stdin JSON contract; do not add platform-specific behavior inside ai-dispatch.
