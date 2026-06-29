# ai-dispatch

产品理念：**any provider call any provider**。

任何 AI provider 都应该能把任务派发给任何另一个本机可用的 AI provider。Codex 可以调 Claude，Claude 可以调 Codex，OpenCode 可以参与 review，Antigravity/Gemini 也可以作为同一套调度里的执行者。`ai-dispatch` 做的不是再造一个 agent 框架，而是把这些跨 provider 调用收敛成一条稳定、本地、可追踪的路径。

`ai-dispatch` 是一个很薄的本地调度运行时，用来把任务派发给本机已经安装好的 AI coding CLI。

它不是 agent 框架。它只把每个调用方都会重复实现的基础能力统一起来：

- target / model 路由
- provider 命令拼装
- 总超时和无活动超时
- 结构化 JSON 结果
- session resume
- run history
- skill 方式调用

## Agent 仓库上手

如果 Agent 拿到的是这个仓库地址，先用这份 README 理解怎么安装、验证和排查。安装完成后，日常任务执行应优先依赖已安装的 skill 和 `references/*.md` 子规范。

使用安装版二进制，不要调用源码路径：

```bash
~/.ai-dispatch/bin/ai-dispatch send <target> "<task>" \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --task-name <name>
```

长 prompt 先写入文件，再传 `--prompt-file`。

只有拿到上一轮真实返回的 `session_id` 时，才使用 resume：

```bash
~/.ai-dispatch/bin/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result \
  --stream-progress \
  --task-name <name>-r2
```

汇报结果前必须读取这些字段：

- `provider_used`
- `model_used`
- `requested_target`
- `route_trace`
- `degraded`
- `degrade_reason`
- `session_id`
- `failure_class`

不要根据请求的 target 猜真实执行结果。请求的 target 只是意图；结果字段才是真相。

## 模型指南

查看内置模型路由指南：

```bash
~/.ai-dispatch/bin/ai-dispatch guide models
```

README 只保留快速口径；模型选择真源以 `ai-dispatch guide models` 输出为准。

快速默认口径：

- `gpt5.5` / `codex`：代码实现、修复、测试、仓库内工程推进。
- `mimo`：前端 UI、视觉布局、低成本候选。
- `kimi`：前端探索、长上下文 coding、规划。
- `grok`：快速工程分析、代码理解、第二视角。
- `sonnet`：稳定 review、重构、规划、文本推理。
- `opus`：架构决策、困难 review、高风险规划。

假设某个 target 可用前，先查真实路由：

```bash
~/.ai-dispatch/bin/ai-dispatch models
~/.ai-dispatch/bin/ai-dispatch models resolve <target> --format json
```

## 默认支持的 Provider

当前默认实现只包含四类 provider：`codex`、`claude`、`opencode`、`antigravity`。

其他 CLI，例如 Cursor CLI、Augie、Qoder，属于新增 provider 范围。接入前先读 `references/provider-onboarding.md`，不要只往 registry 里加模型名。

| Target | 底层 CLI |
| --- | --- |
| `codex`, `gpt5.5` | `codex exec` |
| `claude`, `sonnet`, `opus` | 默认 `claude -p`；可选 PTY 模式 |
| `opencode`, `mimo`, `kimi`, OpenRouter models | `opencode run` |
| `antigravity`, `gemini`, `gemini-flash`, `gemini-pro` | `agy --print` |

## 安装

从源码仓库安装：

```bash
scripts/install.sh
```

安装后会生成：

```text
~/.ai-dispatch/bin/ai-dispatch
~/.ai-dispatch/config.json
~/.ai-dispatch/runs/
~/.ai-dispatch/cache/
~/.ai-dispatch/logs/
```

同时会把内置 skill 安装到用户级 skill 根目录：

```text
~/.codex/skills/ai-dispatch
~/.claude/skills/ai-dispatch
```

这是元 skill 的推荐安装方式：用户级安装一次，然后所有项目里的 Agent 都调用同一个 `~/.ai-dispatch/bin/ai-dispatch`。

手动安装：

```bash
go build -o ~/.ai-dispatch/bin/ai-dispatch ./cmd/ai-dispatch
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
~/.ai-dispatch/bin/ai-dispatch skill install --target all
```

## 配置

初始化一次：

```bash
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
```

查看配置：

```bash
~/.ai-dispatch/bin/ai-dispatch config path
~/.ai-dispatch/bin/ai-dispatch config show
```

默认配置：

```json
{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "registry_path": ""
  },
  "hooks": {
    "notify_command": ""
  }
}
```

`claude_transport` 可选值：

- `print`：默认值，使用 `claude -p`。
- `pty`：通过 PTY driver 调 Claude，适合订阅或本地交互态。
- `auto`：检测到 Anthropic API 环境变量时走 `print`，否则走 `pty`。
- `disabled`：Claude target 直接失败关闭。

模型路由默认使用内置 registry。只有需要本地覆盖时，才设置 `AI_DISPATCH_MODEL_REGISTRY` 或 `models.registry_path`。

## Skill 与文档

安装后的日常 Agent 入口是内置 skill。安装或刷新：

```bash
~/.ai-dispatch/bin/ai-dispatch skill install --target all
```

只安装单侧时使用 `--target codex` 或 `--target claude`。

安装后的 skill 包含精简运行说明和子规范：

```text
~/.codex/skills/ai-dispatch/SKILL.md
~/.codex/skills/ai-dispatch/references/model-routing.md
~/.codex/skills/ai-dispatch/references/config.md
~/.codex/skills/ai-dispatch/references/notifications.md
~/.codex/skills/ai-dispatch/references/provider-onboarding.md
```

不要直接修改 `~/.codex/skills/` 或 `~/.claude/skills/` 下的安装副本。源码入口在：

```text
skills/ai-dispatch/
internal/cli/skill_assets/ai-dispatch/
```

`internal/cli/skill_assets/ai-dispatch/` 是 `go:embed` 使用的嵌入副本；`skill install` 和 `guide models` 都来自这里。修改 skill 文档时，两处必须同步。

最常用的模型指南也可以直接通过 CLI 查看：

```bash
~/.ai-dispatch/bin/ai-dispatch guide models
```

README 和内置 skill 使用同一套产品口径，但职责不同：

- README：仓库级上手入口，给需要从源码安装、验证、打包或排查 ai-dispatch 的人和 Agent 看。
- `SKILL.md`：安装后 Agent 日常执行任务时读取的精简运行说明。
- `references/*.md`：模型路由、配置、通知、provider 接入等子规范。

安装完成后，日常使用优先看已安装 skill 和 references，不需要反复回到仓库 README。

## 排查

先确认安装版运行时和配置：

```bash
~/.ai-dispatch/bin/ai-dispatch doctor --format json
~/.ai-dispatch/bin/ai-dispatch config show
```

确认 target 路由：

```bash
~/.ai-dispatch/bin/ai-dispatch models
~/.ai-dispatch/bin/ai-dispatch models resolve <target> --format json
```

回看失败：

```bash
~/.ai-dispatch/bin/ai-dispatch runs failures --since 24h
~/.ai-dispatch/bin/ai-dispatch runs show <run-id>
```

如果 provider 失败，先看 `failure_class`：`config` 通常是二进制、认证、账号、地区或环境变量问题；`quota` 是额度；`timeout` 是超时；`network` 是网络；`runtime` 是 provider 进程或输出协议异常。

## 运行历史

```bash
~/.ai-dispatch/bin/ai-dispatch runs list --limit 20
~/.ai-dispatch/bin/ai-dispatch runs show <run-id>
~/.ai-dispatch/bin/ai-dispatch runs failures --since 24h
```

默认 run 记录在 `~/.ai-dispatch/runs`。

## 结果契约

程序化调用使用 `--json-result`。关键字段：

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

调用方必须信任 `provider_used` 和 `model_used`，不要信任请求时的 target。

## 安全

部分 provider CLI 可以编辑文件或执行 shell 命令。只在允许这种行为的工作区里使用 ai-dispatch。

通知 hook 里不要发送 prompt 正文、模型回复正文、secret、完整 stderr、个人路径或原始配置。

## 验证安装

```bash
scripts/install.sh
~/.ai-dispatch/bin/ai-dispatch doctor --format json
~/.ai-dispatch/bin/ai-dispatch config show
~/.ai-dispatch/bin/ai-dispatch guide models
```

## 开发与测试

静态和单元验证：

```bash
go test ./...
scripts/go_active_caller_check.sh
```

真实 provider smoke：

```bash
scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_CLAUDE=on scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_AGY=on scripts/go_agy_stress.sh
```

完整压测：

```bash
AI_DISPATCH_SKIP_AGY=on scripts/go_full_matrix_stress.sh
scripts/go_full_matrix_stress.sh
```

当本机账号或地区不可用 Antigravity/Gemini 时，使用 `AI_DISPATCH_SKIP_AGY=on`。
