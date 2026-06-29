# 通知 Hook

ai-dispatch 只保留通用通知接入，不内置具体平台逻辑。hook 命令必须从 stdin 接收事件 JSON，成功时退出码为 `0`。

最小事件结构：

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

通知 payload 禁止包含 prompt 正文、模型回复正文、secret、完整 stderr、个人路径或原始配置。

## 已知本地接入

- Telegram：实现一个 hook，从 stdin 读取 JSON，给配置好的 chat 发送简短状态消息。
- 飞书 / Lark：实现一个 hook，从 stdin 读取 JSON，给配置好的接收方发送简短卡片或文本。

其他平台按同一份 stdin JSON 契约接入；不要把平台私有逻辑写进 ai-dispatch。
