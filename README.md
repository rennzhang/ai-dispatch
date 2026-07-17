# ai-dispatch

把一个 AI Agent 的任务，顺手派给另一家本机 AI CLI。

Claude、Codex、OpenCode、Antigravity/Gemini、Grok 在 ai-dispatch 里都是同级 provider。你不用关心底层命令怎么拼，只要告诉当前 Agent：“用 ai-dispatch 找 Claude / Codex / OpenCode / Gemini / Grok 看一下”。

适合：

- 让另一个模型做严格 review。
- 让一个模型实现，另一个模型复查。
- 找第二视角，避免单模型盲区。
- 把同一问题交给多个模型对照。
- 在上一轮外部模型结果上继续追问。

ai-dispatch 是本地运行的薄 runtime：它只负责安装入口、模型短名路由、调用 provider、记录结果和继续会话，不替你做产品决策。

## 安装

有 Node.js 时，直接安装 CLI：

```bash
npm install -g ai-dispatch
ai-dispatch doctor
```

一次性执行可以用：

```bash
npx --yes ai-dispatch doctor
```

npm 包会下载与包版本一致的 GitHub Release 原生二进制，并校验 SHA-256。它只安装 CLI；Claude Code 和 Codex skill 仍按下面方式安装。

通过 Homebrew 安装：

```bash
brew install rennzhang/tap/ai-dispatch
```

没有 Node.js，或希望一次安装 CLI 和两个 skill 时：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

安装后可以直接调用：

```bash
ai-dispatch doctor
```

要确认 PATH、skill 或 npm 入口实际运行的是哪一份构建：

```bash
ai-dispatch version --format json
```

以返回的 `version`、`revision`、`modified` 为准；不要只看安装目录里的 `VERSION` 文件。

只安装 CLI：

```bash
AI_DISPATCH_SKILL_TARGET=none \
  curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

安装给 Claude Code：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
```

安装给 Codex：

```bash
npx skills add rennzhang/ai-dispatch -g --agent codex
```

两个都装：

```bash
npx skills add rennzhang/ai-dispatch -g --all
```

`npx skills add` 只安装 skill；skill 会按需准备 runtime。需要人或脚本直接调用 CLI 时，用上面的 curl 安装。

安装后，在终端、Claude 或 Codex 里都可以直接用。

## 直接复制这些提示词

让 Claude Opus 做发版前 review：

```text
请用 ai-dispatch 调 Claude Opus 对当前 diff 做交付级 review。
重点看 blocker/high risk、过度设计、漂移、兼容层和发布风险。
只输出 findings first，最后给是否可以发版。
```

让 Codex 帮你实现：

```text
请用 ai-dispatch 调 Codex 实现这个修复。
保持最小改动，完成后跑测试，把关键 diff 和验证结果带回来。
```

让多个模型一起看：

```text
请用 ai-dispatch 找 opus、glm、gemini-pro、kimi 各自独立 review 当前变更。
最后汇总共识、冲突点和必须立刻修的项。
```

## 继续追问

如果上一轮外部模型返回了可继续的会话，直接让当前 Agent 追问它：

```text
继续追问刚才那个 Claude reviewer：
它提到的 P1 里哪些必须现在修，哪些可以发版后处理？
```

你不需要自己管理 session id；Agent 会从 ai-dispatch 的返回结果里拿。

## 模型和偏好

你可以直接说 `opus`、`sonnet`、`codex`、`gemini-pro`、`grok`、`grok-fast`、`mimo-pro`、`glm`、`kimi`、`qwen` 这类短名。

ai-dispatch 有两个用户态文件：

- `~/.ai-dispatch/config.json` 的 `models` 字段：你这台机器已添加的模型短名和候选池。
- `~/.ai-dispatch/preferences.md`：你喜欢在什么场景用哪些模型。

普通使用不用先配置。等你试出哪些模型在这台机器上可用，或者发现“review 总想用这几个模型”“前端 UI 总想用另几个模型”，再让 Agent 帮你维护这两个文件即可。

更新可用模型池时，可以直接说：

```text
请帮我更新 ai-dispatch 的可用模型池。
把我确认能用的模型短名写进 ~/.ai-dispatch/config.json 的 models 字段：
<short-name> 走 <provider>/<model-id>，必要时再加一个 fallback 候选。
```

更新场景偏好时，可以直接说：

```text
请帮我更新 ai-dispatch 的模型偏好。
review 场景优先用 <review-target>；代码实现优先用 <coding-target>。
写进 ~/.ai-dispatch/preferences.md，保持简短清楚。
```

## 当前支持的 provider

ai-dispatch 当前内置支持五类本机 CLI provider：

| Provider | 底层 CLI | 常见用途 |
| --- | --- | --- |
| Codex | `codex exec` | 实现、修复、仓库内工程推进 |
| Claude | `claude -p` 或 PTY | 高风险 review、判断、复杂推理 |
| OpenCode | `opencode run` | OpenRouter / OpenAI / Google 等模型 |
| Antigravity/Gemini | `agy --print` | Gemini / Antigravity 视角 |
| Grok | `grok --output-format json` | Grok Build CLI，快速实现、分析、并行探索 |

想接入更多 CLI provider 或参与贡献，见 [issue #1](https://github.com/rennzhang/ai-dispatch/issues/1)。

## 技术文档

日常使用先看本 README。需要底层细节时再看：

- [技术篇](docs/technical.md)：CLI、JSON 结果、run history、provider scan、failure_class、发布。
- [架构设计](DESIGN.md)：包边界、请求流程、状态目录。
- [Changelog](CHANGELOG.md)：公开版本变更记录。
- [Provider Acceptance](docs/provider-acceptance.md)：新增或回归 provider 的真实验收 harness。
- [配置参考](skills/ai-dispatch/references/config.md)：`config.json` 字段和模型路由。
- [偏好格式](skills/ai-dispatch/references/preferences.md)：`preferences.md` 怎么维护。

## Reasoning effort

跨 provider 可用顶层 `--effort` 控制推理档位：

```bash
ai-dispatch send gpt5.6 "implement the fix" --effort xhigh --json-result
```

内置 Codex GPT-5.6 target 只开放 `gpt5.6`（→ `gpt-5.6-sol`）和 `gpt5.6-terra`。这两个模型允许 `low | medium | high | xhigh | max`；最低档 `none` 不开放，其他 GPT-5.6 变体也不放入可用模型列表。`codex` 默认模型仍是 GPT-5.5，不会被替换。

合法值：`auto | none | minimal | low | medium | high | xhigh | max`。省略或 `auto` 表示不覆盖各 CLI 默认。只有当前 provider/模型确认支持的精确档位才会传递；否则回到 `auto`，结果里会带 `requested_effort` / `applied_effort` / `effort_fallback_reason`，不会静默降到相邻档。详见 [Reasoning Effort 设计](docs/reasoning-effort-design.md)。

## 安全

ai-dispatch 调的是你本机已安装的 AI CLI。某些 provider 可以读写文件或执行命令；只在你愿意交给这些 CLI 的工作区里使用。

Grok provider 为了非交互式执行默认使用 `grok.approval=always`。处理不可信 prompt 或不希望自动批准工具/文件操作时，传 `--provider-opt grok.approval=default`。
