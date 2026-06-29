---
name: ai-dispatch
description: 通过 ai-dispatch 把任务派发给本机已安装的 AI coding CLI。用于调用 Codex、Claude、OpenCode/OpenRouter、Antigravity/Gemini 做实现、review、调研、模型对照、provider 路由、session resume，或排查 ai-dispatch run。
---

# ai-dispatch

使用安装版二进制，不要调用源码路径。

## 派发任务

```bash
~/.ai-dispatch/bin/ai-dispatch send <target> "<task>" \
  --cwd "$PWD" --json-result --stream-progress --task-name <name>
```

- prompt 超过 8KB 或 200 行时先写入文件，用 `--prompt-file <path>` 传入。
- 项目内任务必须传 `--cwd "$PWD"`。
- 长任务、review、可追踪任务必须传 `--task-name <name>`。
- 程序化调用传 `--json-result`；需要实时进度加 `--stream-progress`。

## Resume

只有拿到上一轮真实返回的 `session_id` 时才 resume，不要把历史对话复制进新 prompt：

```bash
~/.ai-dispatch/bin/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result --stream-progress --task-name <name>-r2
```

## 查 run

不要猜历史，先查：

```bash
~/.ai-dispatch/bin/ai-dispatch runs list --limit 20
~/.ai-dispatch/bin/ai-dispatch runs show <run-id>
~/.ai-dispatch/bin/ai-dispatch runs failures --since 24h
```

## 读结果

必须读取返回 JSON 的真实执行字段，不要根据请求 target 推断结果：

- `provider_used`
- `model_used`
- `requested_target`
- `route_trace`
- `degraded`
- `degrade_reason`
- `session_id`（用于后续 resume）

不要在调用方自己实现 provider fallback；读取 `degraded` 和 `degrade_reason`。

## 模型路由

```bash
~/.ai-dispatch/bin/ai-dispatch guide models
~/.ai-dispatch/bin/ai-dispatch models
~/.ai-dispatch/bin/ai-dispatch models resolve <target> --format json
```

选择 target/model 或解释路由时，读 `references/model-routing.md`。

## 按需读取 reference

- `references/model-routing.md`：target/model 选择与路由解释。
- `references/config.md`：初始化、配置文件、provider 路径、本地状态目录。
- `references/notifications.md`：通知 hook 接入。
- `references/provider-onboarding.md`：新增或修改 provider/model。
