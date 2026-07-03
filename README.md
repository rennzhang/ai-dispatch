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

安装完成后，真实调用前先读取用户偏好再选择 target/model：

```bash
pref_path="$(~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path)"
cat "$pref_path"
```

使用安装版二进制，不要调用源码路径：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch send <target> "<task>" \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --task-name <name>
```

Codex 安装时路径为 `~/.codex/skills/ai-dispatch/scripts/ai-dispatch`。

长 prompt 先写入文件，再传 `--prompt-file`。

只有拿到上一轮真实返回的 `session_id` 时，才使用 resume：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch resume --session-id <id> "<delta>" \
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
~/.claude/skills/ai-dispatch/scripts/ai-dispatch guide models
```

README 不维护模型能力真源。用户默认选择以 `~/.ai-dispatch/preferences.md` 为准，真实路由以 `ai-dispatch models resolve` 返回为准。

假设某个 target 可用前，先查真实路由：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models resolve <target> --format json
```

## 默认支持的 Provider

当前默认实现只包含四类 provider：`codex`、`claude`、`opencode`、`antigravity`。

其他 CLI，例如 Cursor CLI、Augie、Qoder，属于新增 provider 范围。接入前先读 `references/provider-onboarding.md`，不要只往 registry 里加模型名。

| Target | 底层 CLI |
| --- | --- |
| `codex`, `gpt5.5` | `codex exec` |
| `claude`, `sonnet`, `opus` | 默认 `claude -p`；可选 PTY 模式 |
| `opencode`, `mimo-openrouter-pro`, `mimo-opencode-free`, `kimi`, OpenRouter models | `opencode run` |
| `antigravity`, `gemini`, `gemini-flash`, `gemini-pro` | `agy --print` |

## 安装

### 方式一：npx skills add（推荐）

通过 [skills](https://www.npmjs.com/package/skills) CLI 从 GitHub 仓库安装 skill。skill 只带轻量 wrapper；第一次运行 wrapper 时会按 `VERSION` 下载对应 release binary，并校验 checksum：

```bash
# Claude Code
npx skills add rennzhang/ai-dispatch -g --agent claude-code

# Codex
npx skills add rennzhang/ai-dispatch -g --agent codex

# 全部
npx skills add rennzhang/ai-dispatch -g --all
```

安装完成后验证：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
```

首次执行 wrapper 可能先准备 CLI 二进制；进入 Go CLI 后，首次 `send`/`resume` 才会初始化 `~/.ai-dispatch/` 配置（生成 config 和 preferences，不扫描 provider）。首次 JSON 结果附 `first_run` + `first_run_setup` 字段。

### 方式二：一键安装

curl 一行安装，自动检测平台、下载预编译包、解压到 skill 目录并验证：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

环境变量：

- `AI_DISPATCH_SKILL_TARGET`：`claude`、`codex` 或 `all`（默认 `all`）
- `AI_DISPATCH_VERSION`：指定版本 tag，默认 `latest`

只装一侧：

```bash
AI_DISPATCH_SKILL_TARGET=claude bash -c "$(curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh)"
```

### 安装后的目录结构

skill 只带 wrapper 和版本号；二进制缓存到本机运行态：

```text
~/.claude/skills/ai-dispatch/
  SKILL.md
  VERSION
  references/{preferences,config,provider-onboarding}.md
  agents/openai.yaml
  scripts/ai-dispatch                      # bash wrapper（按 VERSION 下载 release binary）
```

运行时状态在 `~/.ai-dispatch/`（首次使用自动创建）：

```text
~/.ai-dispatch/
  config.json
  preferences.md
  runs/
  logs/
  bin/
```

### 从旧版升级

旧版把 wrapper 放在 `~/.ai-dispatch/bin/ai-dispatch`、编译缓存在 `~/.ai-dispatch/cache/`。改版后 wrapper 只在 skill 目录，`~/.ai-dispatch/bin/` 只存按版本下载的 runtime binary。旧 wrapper 和旧缓存可以安全删除：

```bash
rm -f ~/.ai-dispatch/bin/ai-dispatch
rm -rf ~/.ai-dispatch/cache
```

重新安装 skill 或运行 curl installer 即可迁移。首次 `send`/`resume` 会补齐缺失的 `config.json` 或 `preferences.md`。

## Agent 安装指令

如果 Agent 拿到这个 README 并被要求安装 ai-dispatch，推荐用 `npx skills add`：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
```

或一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

安装完成后验证：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
```

然后读取用户偏好，根据偏好选择 target/model：

```bash
pref_path="$(~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path)"
cat "$pref_path"
```

首次调用 `send` 或 `resume` 时，Go CLI 会初始化配置目录（`~/.ai-dispatch/`），生成 config 和 preferences，不扫描 provider，并在 JSON 结果中附 `first_run`、`first_run_hint` 和 `first_run_setup` 字段。Agent 应将初始化结果简要汇报给用户，然后继续执行原任务。

## 配置

首次 send/resume 会自动做最小配置初始化。手动 `init` 会同时扫描 provider：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch init --claude-transport print
~/.claude/skills/ai-dispatch/scripts/ai-dispatch init --force   # 覆盖现有 config
```

查看配置：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch config path
~/.claude/skills/ai-dispatch/scripts/ai-dispatch config show
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences show
```

`claude_transport` 可选值：

- `print`：默认值，使用 `claude -p`。
- `pty`：通过 PTY driver 调 Claude，适合订阅或本地交互态。
- `auto`：检测到 Anthropic API 环境变量时走 `print`，否则走 `pty`。
- `disabled`：Claude target 直接失败关闭。

模型路由默认使用内置 registry；用户自己的短名路由写在 `~/.ai-dispatch/config.json` 的 `models` 字段。

示例：

```json
{
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ]
  }
}
```

## Provider 扫描

重新探测本机 provider 可用性和 opencode catalog 模型数量：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --refresh  # 联网刷新 opencode 模型缓存
```

安装新 provider CLI 后或 opencode 认证变更后运行。结果只写入 `~/.ai-dispatch/config.json` 的 `providers` 字段。

扫描只证明 provider CLI 存在、能返回版本；opencode 的 `catalog_model_count` 只表示已认证 provider 在 catalog 里可见的模型数量。订阅、额度、地区封锁、OpenRouter 单模型 endpoint 是否可用，以真实 `send` 失败时的 `failure_class` 为准。

## 用户偏好

用户模型选择偏好放在 `~/.ai-dispatch/preferences.md`。Agent 在真实调用前必须先读它，再选择 target/model；用户明确指定 target/model 时，用户指定优先。

查看偏好路径和内容：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences show
```

用默认程序打开编辑：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences open
```

偏好文件只记录用户自己的默认倾向，例如哪些模型适合 review、前端 UI、Bug 查找、写文档或代码实现。偏好的维护方式看安装后 skill 的 `references/preferences.md`；公共 target 能力和真实路由看 `guide models`、`models` 和 `models resolve`。

## Skill 与文档

安装后的日常 Agent 入口是内置 skill。

安装后的 skill 包含精简运行说明和子规范：

```text
~/.codex/skills/ai-dispatch/SKILL.md
~/.codex/skills/ai-dispatch/VERSION
~/.codex/skills/ai-dispatch/references/preferences.md
~/.codex/skills/ai-dispatch/references/config.md
~/.codex/skills/ai-dispatch/references/provider-onboarding.md
```

不要直接修改 `~/.codex/skills/` 或 `~/.claude/skills/` 下的安装副本。源码入口在：

```text
skills/ai-dispatch/
```

最常用的模型指南也可以直接通过 CLI 查看：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch guide models
```

README 和内置 skill 使用同一套产品口径，但职责不同：

- README：仓库级上手入口，给需要验证、打包或排查 ai-dispatch 的人和 Agent 看。
- `SKILL.md`：安装后 Agent 日常执行任务时读取的精简运行说明。
- `references/*.md`：偏好、配置、provider 接入等子规范。

安装完成后，日常使用优先看已安装 skill 和 references，不需要反复回到仓库 README。

## 排查

先确认安装版运行时和配置：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch config show
```

`doctor` 只输出健康摘要，不回显本机路径、完整模型路由或环境变量具体值。需要定位文件时使用 `config path`、`preferences path`。

确认 target 路由：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models resolve <target> --format json
```

回看失败：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs failures --since 24h
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs show <run-id>
```

如果 provider 失败，先看 `failure_class`：`config` 通常是二进制、认证、账号、地区或环境变量问题；`quota` 是额度；`timeout` 是超时；`network` 是网络；`runtime` 是 provider 进程或输出协议异常。

## 运行历史

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs list --limit 20
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs show <run-id>
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs failures --since 24h
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
- `first_run`（仅首次配置初始化时出现）
- `first_run_hint`（仅首次配置初始化时出现）
- `first_run_setup`（仅首次配置初始化时出现，含 `initialized_at`、`home_dir`、`config_path`、`preferences_path`、`claude_transport`）

调用方必须信任 `provider_used` 和 `model_used`，不要信任请求时的 target。

## 安全

部分 provider CLI 可以编辑文件或执行 shell 命令。只在允许这种行为的工作区里使用 ai-dispatch。

## 验证安装

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch config show
~/.claude/skills/ai-dispatch/scripts/ai-dispatch guide models
```

## 开发与测试

静态和单元验证：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
scripts/go_active_caller_check.sh
```

真实 provider 执行默认开启（`AI_DISPATCH_GO_PROVIDER_EXECUTION` 默认 `on`，由二进制内部设置）。开发/测试时需显式关闭以避免真实调用：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
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

## 打包发布

交叉编译预编译包：

```bash
scripts/release.sh
# CI / GitHub Release 使用 tag 做硬校验：
scripts/release.sh vX.Y.Z
```

产出 `dist/` 下 darwin/linux × amd64/arm64 四个自包含 tarball，每个含 `SKILL.md` + `VERSION` + `LICENSE` + `references/` + `agents/` + `scripts/ai-dispatch` (wrapper) + `scripts/ai-dispatch-go` (预编译二进制)。

`npx skills add` 安装的是轻量 skill；wrapper 按 `VERSION` 下载对应 release tarball。发版前确保 `skills/ai-dispatch/VERSION` 与 tag 一致；release workflow 会强制校验，不一致直接失败。
