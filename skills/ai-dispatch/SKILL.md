---
name: ai-dispatch
description: 通过 ai-dispatch 把任务派发给本机已安装的 AI coding CLI。用于调用 Codex、Claude、OpenCode/OpenRouter、Antigravity/Gemini、Grok 做实现、review、调研、模型对照、provider 路由、session resume，或排查 ai-dispatch run。
---

# ai-dispatch

使用当前 skill 目录下的 `scripts/ai-dispatch`，不要调用源码路径。

Claude、Codex、OpenCode、Antigravity/Gemini、Grok 在这里都是同级 provider。用户说“找 Claude 看一下”“让 Codex 实现”“叫 Gemini 补个视角”“OpenCode 也 review 一下”“让 Grok 快速试一版”，都可以用 ai-dispatch 派发。

## 调用前

真实调用前先读用户偏好：

```bash
scripts/ai-dispatch preferences path
```

读取返回路径里的 `preferences.md`。用户明确指定 target/model 时，用户指定优先。
文件不存在时，先运行 `scripts/ai-dispatch preferences show` 创建默认文件。

需要查看用户已经确认并主动加入的本机模型候选池时，读：

```bash
scripts/ai-dispatch config show
```

只看 `models` 字段。`models` 是可执行短名路由，不是 provider 自动扫描出的完整 catalog。

选定 target 后，必要时确认真实路由：

```bash
scripts/ai-dispatch models resolve <target> --format json
```

## 派发

把用户的自然语言意图整理成 prompt，再派给目标 provider：

```bash
scripts/ai-dispatch send <target> "<task>" \
  --cwd "$PWD" --json-result --stream-progress --task-name <name>
```

要求：

- 项目任务传 `--cwd "$PWD"`。
- review prompt 带真源锚点：diff、文件、日志、命令或明确范围。
- 长 prompt 先写文件，用 `--prompt-file <path>`。
- 长任务、review、可追踪任务传 `--task-name`。
- 外部模型只提供输入；最终裁决由当前 Agent 做。

## 继续追问

只有上一轮结果里有真实 `session_id` 时才 resume：

```bash
scripts/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result --stream-progress --task-name <name>-r2
```

不要把历史对话复制进新 prompt。追问只写新增问题或 delta。

## 读结果

返回 JSON 才是真相。汇报前读取：

- `provider_used`
- `model_used`
- `requested_target`
- `route_trace`
- `degraded`
- `degrade_reason`
- `session_id`
- `failure_class`

不要根据请求 target 猜真实执行结果。不要在调用方自己实现 fallback。

## 常用目标

可直接尝试：`codex`、`opus`、`sonnet`、`gemini-pro`、`grok`、`grok-fast`、`mimo-pro`、`glm`、`kimi`、`qwen`。

偏好和可用候选以用户本机 `preferences.md` 与 `config.json models` 为准。

## 按需读取 reference

- `references/preferences.md`：偏好的用途、更新方式和边界。
- `references/config.md`：配置文件、模型候选池、provider scan、本地状态目录。
