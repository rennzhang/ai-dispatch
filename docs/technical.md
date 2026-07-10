# 技术篇

本文面向要排查、集成或发版 ai-dispatch 的人。普通用户先看 README。

## 安装形态

有 Node.js 时，推荐通过 npm 安装 CLI：

```bash
npm install -g ai-dispatch
```

也可以不做全局安装：

```bash
npx --yes ai-dispatch doctor
```

npm 包不内置平台二进制。安装时它会下载与 npm 包版本对应的 GitHub Release `.bin` 资产，并按同一 release 的 `SHA256SUMS` 校验后才执行。当前支持 darwin/linux 的 amd64/arm64，要求 Node.js 18+。

Homebrew 用户通过独立 tap 安装：

```bash
brew install rennzhang/tap/ai-dispatch
```

公式下载同一 GitHub Release 的平台 tarball，并使用生成时写入的 SHA-256。公式不是手工维护版本和 hash：`scripts/render-homebrew-formula.sh` 从 release `SHA256SUMS` 生成它。

curl 仍可安装 CLI，并按需安装 skill：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

只安装 CLI：

```bash
AI_DISPATCH_SKILL_TARGET=none \
  curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

也可以只通过 skill 安装：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
npx skills add rennzhang/ai-dispatch -g --agent codex
npx skills add rennzhang/ai-dispatch -g --all
```

通过 npm 安装时，npm 的 `ai-dispatch` 命令直接代理到包内已校验的 release binary；不写入 `~/.ai-dispatch/bin/`。通过 curl 安装时，安装脚本会下载 release tarball，把稳定 CLI 写到 `~/.ai-dispatch/bin/ai-dispatch`，并把 `ai-dispatch` 链接到 `~/.local/bin/`。如果不想创建 PATH 链接，设置 `AI_DISPATCH_LINK_DIR=none`。

通过 `npx skills add` 安装时，skill 目录里只有轻量 wrapper。第一次执行 wrapper 时，它会按 `VERSION` 下载对应 GitHub Release tarball，并校验 checksum。

## 路径

Claude Code 安装入口：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch
```

Codex 安装入口：

```bash
~/.codex/skills/ai-dispatch/scripts/ai-dispatch
```

稳定 CLI 入口：

```bash
~/.ai-dispatch/bin/ai-dispatch
ai-dispatch  # 如果 ~/.local/bin 在 PATH 中
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

`bin/` 保存稳定 CLI wrapper 和按版本缓存的 release binary。`runs/` 保存每次调用的结构化结果。

## CLI

直接派发：

```bash
ai-dispatch send opus "review current diff" \
  --cwd "$PWD" --json-result --stream-progress --task-name review-opus
```

长 prompt：

```bash
ai-dispatch send opus \
  --prompt-file /tmp/review.md \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --task-name review-opus
```

继续上一轮：

```bash
ai-dispatch resume --session-id <id> "focus on the delta" \
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
ai-dispatch models
```

解析真实路由：

```bash
ai-dispatch models resolve opus --format json
```

路由顺序：

```text
显式 provider + --model
> ~/.ai-dispatch/config.json 的 models
> 内置 registry
> provider 推断
```

`config.json models` 会覆盖同名内置短名；这是用户主动维护本机候选池的入口。需要强制 provider 语义时，使用 provider 名加显式 `--model`。

`preferences.md` 只决定“某场景倾向选哪个短名”。`config.json models` 才是用户已经确认并主动加入的本机模型候选池。

## Provider 扫描

主动扫描 provider CLI：

```bash
ai-dispatch providers scan --format json
ai-dispatch providers scan --refresh
```

扫描只更新 `~/.ai-dispatch/config.json` 的 `providers` 字段。它证明 provider CLI 看起来存在、能返回版本；不证明订阅、额度、地区封锁或 OpenRouter 单模型 endpoint 可用。

## Grok provider opts

Grok 的推荐入口是 `config.json models` 里的 `grok` 候选链：第一候选走本机 Grok Build CLI，后续候选可以走 OpenCode/OpenRouter 兜底。

```bash
ai-dispatch send grok "Reply exactly: OK" --json-result
ai-dispatch send grok-fast "Reply exactly: OK" --json-result
```

推荐配置：

```json
{
  "models": {
    "grok": [
      { "provider": "grok", "model": "grok-4.5" },
      { "provider": "opencode", "model": "openrouter/x-ai/grok-4.5" }
    ]
  }
}
```

可选参数统一走 `--provider-opt`：

```bash
ai-dispatch send grok "Reply exactly: OK" \
  --provider-opt grok.max-turns=1 \
  --provider-opt grok.web-search=off \
  --json-result
```

支持的 key：`grok.max-turns`、`grok.effort`、`grok.web-search=on|off`、`grok.subagents=on|off`、`grok.approval=always|default`。

默认 `grok.approval=always` 会向 Grok CLI 传 `--always-approve`，用于非交互式 dispatch。处理不可信 prompt 或不希望自动批准工具/文件操作时，传 `--provider-opt grok.approval=default`。

旧的 `grok-build-0.1` 不再作为直接 target。需要原生 Grok Build CLI 时用 `grok` 或 `grok-fast`；需要直接走 OpenRouter 时，用 `opencode --model openrouter/x-ai/grok-4.5`。

## Run history

```bash
ai-dispatch runs list --limit 20
ai-dispatch runs show <run-id>
ai-dispatch runs failures --since 24h
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
ai-dispatch doctor --format json
```

它只输出健康摘要，不回显本机路径、完整模型路由或环境变量具体值。需要定位文件时用：

```bash
ai-dispatch config path
ai-dispatch preferences path
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
AI_DISPATCH_ACCEPTANCE_TARGET=grok \
  AI_DISPATCH_ACCEPTANCE_PROVIDER=grok \
  AI_DISPATCH_ACCEPTANCE_MODEL=grok-4.5 \
  scripts/go_provider_acceptance.sh
scripts/go_grok_stress.sh
```

新增或回归 provider 的完整验收合同见 [Provider Acceptance](provider-acceptance.md)。

## 发版

先更新 `skills/ai-dispatch/VERSION`，再本地构建验证：

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

同时生成 `SHA256SUMS`。GitHub Release 上传四个平台的 skill tarball 和 npm 用的 standalone `.bin`：darwin/linux × amd64/arm64。

正式发布由 tag 触发 GitHub Actions：

```bash
git tag vX.Y.Z
git push origin main
git push origin vX.Y.Z
```

发版后用 release 页面或 `gh release view vX.Y.Z` 确认四个平台包和 `SHA256SUMS` 都已上传。

GitHub Release 完成后，再发布同版本 npm 包和 Homebrew tap 公式：

```bash
cd npm/ai-dispatch
npm test
npm pack --dry-run
cd ../..
npm publish ./npm/ai-dispatch
```

`scripts/release.sh` 会拒绝 npm 包版本与 `skills/ai-dispatch/VERSION` 不一致的构建，并生成 `dist/ai-dispatch.rb`。将它复制到 `rennzhang/homebrew-tap` 的 `Formula/ai-dispatch.rb` 后提交推送。npm publish 与 tap 更新都保持人工显式执行，避免 tag 推送自动向外发布。

公开用户可见变更记录维护在 [Changelog](../CHANGELOG.md)。发版前先补对应版本条目，再更新 `skills/ai-dispatch/VERSION`。

## Provider 扩展

安装后的 skill 只负责调用已有 provider；新增 CLI provider 需要 fork 源码、实现 adapter、补测试并重新发布。接入说明见 [Provider Onboarding](provider-onboarding.md)。
