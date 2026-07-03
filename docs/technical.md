# 技术篇

本文面向要排查、集成或发版 ai-dispatch 的人。普通用户先看 README。

## 安装形态

推荐通过 skill 安装：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
npx skills add rennzhang/ai-dispatch -g --agent codex
npx skills add rennzhang/ai-dispatch -g --all
```

也可以用一键安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

通过 `npx skills add` 安装时，skill 目录里只有轻量 wrapper。第一次执行 wrapper 时，它会按 `VERSION` 下载对应 GitHub Release tarball，并校验 checksum。

通过 curl 安装时，安装脚本会直接下载 release tarball，把 wrapper 和当前平台的预编译二进制一起放入 skill 目录。

## 路径

Claude Code 安装入口：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch
```

Codex 安装入口：

```bash
~/.codex/skills/ai-dispatch/scripts/ai-dispatch
```

本机运行态：

```text
~/.ai-dispatch/
  config.json
  preferences.md
  runs/
  logs/
  bin/
```

`bin/` 保存 wrapper 下载的 release binary 缓存。`runs/` 保存每次调用的结构化结果。

## CLI

直接派发：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch send opus "review current diff" \
  --cwd "$PWD" --json-result --stream-progress --task-name review-opus
```

长 prompt：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch send opus \
  --prompt-file /tmp/review.md \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --task-name review-opus
```

继续上一轮：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch resume --session-id <id> "focus on the delta" \
  --json-result --stream-progress --task-name review-opus-r2
```

程序化调用应使用 `--json-result`。需要实时进度时加 `--stream-progress`。

## 结果字段

调用方不要根据请求 target 推断真实结果，必须读 JSON 字段：

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
- `failure_class`

`session_id` 可用于后续 `resume`。`degraded=true` 表示 ai-dispatch 已按路由策略换过候选。

## 模型路由

查看可用 target：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models
```

解析真实路由：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch models resolve opus --format json
```

路由顺序：

```text
显式 provider + --model
> ~/.ai-dispatch/config.json 的 models
> 内置 registry
> provider 推断
```

`preferences.md` 只决定“某场景倾向选哪个短名”。`config.json models` 才是用户已经确认并主动加入的本机模型候选池。

## Provider 扫描

主动扫描 provider CLI：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --format json
~/.claude/skills/ai-dispatch/scripts/ai-dispatch providers scan --refresh
```

扫描只更新 `~/.ai-dispatch/config.json` 的 `providers` 字段。它证明 provider CLI 看起来存在、能返回版本；不证明订阅、额度、地区封锁或 OpenRouter 单模型 endpoint 可用。

## Run history

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs list --limit 20
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs show <run-id>
~/.claude/skills/ai-dispatch/scripts/ai-dispatch runs failures --since 24h
```

`runs list` 只输出安全摘要。完整结果看 `runs show`。

## failure_class

| failure_class | 含义 |
| --- | --- |
| `config` | provider 二进制、认证、账号、地区或模型访问权问题 |
| `quota` | 额度不足 |
| `timeout` | 墙钟或活动超时 |
| `network` | 网络问题 |
| `runtime` | provider 进程或输出协议异常 |
| `input` | 调用参数或 prompt 输入不合法 |

## Doctor

`doctor` 是排查命令，不是普通安装后的必跑步骤：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch doctor --format json
```

它只输出健康摘要，不回显本机路径、完整模型路由或环境变量具体值。需要定位文件时用：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch config path
~/.claude/skills/ai-dispatch/scripts/ai-dispatch preferences path
```

## 开发验证

单元测试默认关闭真实 provider 执行：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go vet ./...
scripts/go_active_caller_check.sh
```

真实 smoke：

```bash
scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_CLAUDE=on scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_AGY=on scripts/go_agy_stress.sh
```

## 发版

```bash
scripts/release.sh vX.Y.Z
```

产物在 `dist/`：

```text
ai-dispatch-<os>-<arch>/
  SKILL.md
  VERSION
  LICENSE
  references/
  agents/
  scripts/ai-dispatch
  scripts/ai-dispatch-go
```

同时生成 `SHA256SUMS`。GitHub Release 上传四个平台包：darwin/linux × amd64/arm64。

## Provider 扩展

安装后的 skill 只负责调用已有 provider；新增 CLI provider 需要 fork 源码、实现 adapter、补测试并重新发布。接入说明见 [Provider Onboarding](provider-onboarding.md)。
