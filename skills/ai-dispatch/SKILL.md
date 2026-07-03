---
name: ai-dispatch
description: 通过 ai-dispatch 把任务派发给本机已安装的 AI coding CLI。用于调用 Codex、Claude、OpenCode/OpenRouter、Antigravity/Gemini 做实现、review、调研、模型对照、provider 路由、session resume，或排查 ai-dispatch run。
---

# ai-dispatch

使用安装版二进制，不要调用源码路径。

入口位于 skill 目录的 `scripts/` 下。这个入口是轻量 wrapper；如果 release binary 尚未缓存，它会按 skill 目录的 `VERSION` 下载对应 GitHub Release tarball，并校验 checksum：

- Claude 安装：`~/.claude/skills/ai-dispatch/scripts/ai-dispatch`
- Codex 安装：`~/.codex/skills/ai-dispatch/scripts/ai-dispatch`

以下示例用 Claude 路径；Codex 安装时替换为 `~/.codex/skills/ai-dispatch/scripts/ai-dispatch`。

首次执行 wrapper 只负责准备 CLI 二进制；进入 Go CLI 后，首次 `send`/`resume` 才会做最小配置初始化：创建 `~/.ai-dispatch/`、生成 config 和 preferences，不探测 provider。需要探测 provider 时运行 `init` 或 `providers scan`。首次配置初始化的 JSON 结果附 `first_run`、`first_run_hint` 和 `first_run_setup` 字段。

## 安装与更新

安装或重新安装（推荐 `npx skills add`）：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
```

或一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

安装后验证：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
```

## 用户偏好

真实调用前必须先运行 `preferences path` 拿到偏好文件路径并读取它，再选择 target/model。

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path
```

文件不存在时先运行：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences show
```

需要让用户编辑偏好时，运行：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences open
```

偏好只影响默认选择；用户明确指定 target/model 时，优先用户指定。偏好里写短名，短名的真实 provider/model 路由由 `config.json` 的 `models` 字段和内置 registry 解析。

## 派发任务

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch send <target> "<task>" \
  --cwd "$PWD" --json-result --stream-progress --task-name <name>
```

- prompt 超过 8KB 或 200 行时先写入文件，用 `--prompt-file <path>` 传入。
- 项目内任务必须传 `--cwd "$PWD"`。
- 长任务、review、可追踪任务必须传 `--task-name <name>`。
- 程序化调用传 `--json-result`；需要实时进度加 `--stream-progress`。
- review prompt 必须带真源锚点：文件、diff、日志、命令或精确证据。
- 主线程负责最终裁决；派发出去的模型只提供输入，不直接给最终结论。
- 以当前仓库、真实命令和返回 JSON 字段为真源。

## Resume

只有拿到上一轮真实返回的 `session_id` 时才 resume，不要把历史对话复制进新 prompt：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result --stream-progress --task-name <name>-r2
```

## 查 run

不要猜历史，先查：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs list --limit 20
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs show <run-id>
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs failures --since 24h
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
- `first_run`（首次配置初始化时出现，含 `first_run_hint`）
- `first_run_setup`（首次配置初始化时出现，含 `initialized_at`、`home_dir`、`config_path`、`preferences_path`、`claude_transport`）

不要在调用方自己实现 provider fallback；读取 `degraded` 和 `degrade_reason`。

首次配置初始化时返回 JSON 含 `first_run: true`、`first_run_hint` 和 `first_run_setup` 字段，提示用户可以在 `~/.ai-dispatch/preferences.md` 中设定模型调用偏好。后续命令不再出现这些字段。

## 模型选择

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch guide models
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models resolve <target> --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path
```

选择 target/model 时，先读用户偏好；解释真实路由时，以 `models resolve` 和返回 JSON 为准。用户机器自己的短名候选链维护在 `~/.ai-dispatch/config.json` 的 `models` 字段。

## Provider 扫描

重新探测本机 provider 可用性和 opencode catalog 模型数量：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --refresh  # 联网刷新 opencode 模型缓存
```

安装新 provider CLI 后或 opencode 认证变更后运行。结果只写入 `~/.ai-dispatch/config.json` 的 `providers` 字段。

扫描只证明 provider CLI 存在、能返回版本；opencode 的 `catalog_model_count` 只表示已认证 provider 在 catalog 里可见的模型数量。订阅、额度、地区封锁、OpenRouter 单模型 endpoint 是否可用，以真实 `send` 失败时的 `failure_class` 为准。

## 按需读取 reference

- `references/preferences.md`：偏好的用途、更新方式和边界。
- `references/config.md`：初始化、配置文件、provider 路径、本地状态目录。
- `references/provider-onboarding.md`：新增或修改 provider/model。
