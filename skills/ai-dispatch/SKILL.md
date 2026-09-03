---
name: ai-dispatch
description: 通过 ai-dispatch 把任务派发给本机已安装的 AI coding CLI。用于调用 Codex、Claude、OpenCode/OpenRouter、Antigravity/Gemini、Grok 做实现、review、调研、模型对照、provider 路由、session resume，或排查 ai-dispatch run。
---

# ai-dispatch

使用当前 skill 目录下的 `scripts/ai-dispatch`，不要调用源码路径。

Claude、Codex、OpenCode、Antigravity/Gemini、Grok、Cursor 在这里都是同级 provider。用户说“找 Claude 看一下”“让 Codex 实现”“叫 Gemini 补个视角”“OpenCode 也 review 一下”“让 Grok 快速试一版”“用 Cursor 里的 Fable 试一版”，都可以用 ai-dispatch 派发。

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

## 模型名解析

用户说的模型名可能不严谨，按下面规则翻译成 CLI 参数：

- 只说模型名 → 作为 target，例如 `send opus5`
- 说「provider + 模型名」→ provider 作 target、模型名作 `--model`，例如 `cursor opus5` → `send cursor --model opus5`
- 不使用 `provider:model` 或 `model:provider` 冒号写法；CLI 会直接拒绝
- 不确定时先用同一组参数执行 `models resolve`，例如 `models resolve cursor --model opus5 --format json`

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
- 默认在 prompt 末尾加一句“不要派发子代理，直接在当前会话完成”。只有用户明确要求被派发模型使用子代理时才去掉这句；Grok 额外传 `--provider-opt grok.subagents=on`。调用多个独立模型不算要求模型内部派发子代理。
- 需要控制推理档位时传顶层 `--effort <level>`（`auto|none|minimal|low|medium|high|xhigh|max`）。省略或 `auto` 表示不覆盖 CLI 默认；不支持的精确档位会回到 `auto` 并写入 `effort_fallback_reason`，不会静默降到相邻档。不要使用已移除的 `grok.effort`。
- 需要加速时统一传顶层 `--fast`。支持的 provider 会真实应用；不支持的 provider 按标准速度执行，并写入 `fast_fallback_reason`，不会静默声称已加速。
- 外部模型只提供输入；最终裁决由当前 Agent 做。

## 继续追问

只有上一轮结果里有真实 `session_id` 时才 resume：

```bash
scripts/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result --stream-progress --task-name <name>-r2
```

不要把历史对话复制进新 prompt。追问只写新增问题或 delta。

## 读结果

返回 JSON 才是真相。JSON 里 `agent_hint` 是给模型的强制指令，不要贴给用户。给用户的最终回复必须原样贴上 `user_facing_summary`，不要改写、不要节选、不要自己重算耗时或猜测模型。

这块由 CLI 在收口时写好，包括 Target、降级（若有）、失败（若有）、Duration（`hh:mm:ss`）和 Session ID（若有）。未使用 `--stream-progress` 时 stderr 再打同一块纯文本；使用 `--stream-progress` 时 stderr 保持 NDJSON，摘要在 JSON 和 terminal progress 事件里。

不要根据请求 target 猜真实执行结果。不要在调用方自己实现 fallback。排障仍可读 JSON 里的 `provider_used`、`model_used`、`degraded`、`session_id`、`failure_class` 等字段，但那些不是给用户看的默认内容。`degraded` 只表示路由降级；effort 回退看 `requested_effort`/`applied_effort`，fast 是否生效看 `requested_fast`/`applied_fast`。

排查多个本地入口是否指向同一构建时，读取运行中 binary 的身份，不要只看 skill 的 `VERSION` 文件：

```bash
scripts/ai-dispatch version --format json
```

对比 `version`、`revision` 和 `modified`；本地未提交构建会明确带 `+dirty`。

## 按需读取 reference

- `references/preferences.md`：偏好的用途、更新方式和边界。
- `references/config.md`：配置文件、模型候选池、provider scan、本地状态目录。
