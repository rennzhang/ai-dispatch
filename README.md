# ai-dispatch

让当前 AI Agent，用一套命令调用本机不同的 AI coding CLI。

既可以在终端和脚本里调用，也可以作为 Claude Code / Codex 的 skill，让当前 Agent 用自然语言派发任务，例如：“用 ai-dispatch 找 Claude review 当前 diff”。

## 能做什么

- **统一调用**：用同一套命令派发任务，ai-dispatch 负责适配各家 CLI 的参数和结果格式。
- **模型路由**：给常用模型配置短名和候选顺序；调用失败时，按路由规则尝试后续候选。
- **继续会话**：上一轮返回可继续的 session 后，可以接着追问、补充要求或继续修改。
- **查看进度与结果**：提供流式进度、JSON 结果和本地调用记录，方便 Agent、脚本和自动化流程接入。

## 适用场景

| 场景 | 怎么用 |
| --- | --- |
| 独立 review | 让另一个模型检查当前 diff、找遗漏和风险 |
| 实现与复查 | 让一个模型完成修复，再让另一个模型复查并核对测试结果 |
| 排障与方案分析 | 把日志、代码或方案交给另一个模型，补充不同视角 |
| 多模型对照 | 让当前 Agent 分别派发同一个问题，再汇总各模型的共识与分歧 |
| 脚本与自动化 | 通过 CLI 派发任务、消费 JSON 结果，并按需继续会话 |

## 当前支持的 provider

ai-dispatch 当前内置支持七类本机 CLI provider：

| Provider | 底层 CLI | 常见用途 |
| --- | --- | --- |
| Codex | `codex` | 实现、修复、仓库内工程推进 |
| Claude | `claude` | 高风险 review、判断、复杂推理 |
| OpenCode | `opencode` | OpenRouter / OpenAI / Google 等模型 |
| Antigravity/Gemini | `agy` | Gemini / Antigravity 视角 |
| Grok | `grok` | Grok Build CLI，快速实现、分析、并行探索 |
| Cursor | `cursor-agent` | Cursor 订阅内 Claude/GPT/Gemini/Kimi/Composer 模型 |
| OMP (oh-my-pi) | `omp` | DeepSeek 等通过 OMP 配置的模型（如 `opencode-go/deepseek-flash`） |

这里的 provider 指被调用的本机 CLI。使用前需要安装相应 CLI，并完成登录或 API 配置；具体可用模型和额度由对应账号与配置决定。Gemini 通过 Antigravity（`agy`）接入。

想接入更多 CLI provider 或参与贡献，见 [issue #1](https://github.com/rennzhang/ai-dispatch/issues/1)。

## 使用示例

### 终端调用

安装后，在项目目录里调用本机已登录的 Codex CLI，检查当前变更：

```bash
ai-dispatch send codex "检查当前 diff，指出风险和遗漏，不修改文件" \
  --cwd "$PWD" --json-result
```

### 直接复制这些提示词

在安装了 ai-dispatch skill 的 Claude Code / Codex 中，可以直接用自然语言派发。涉及模型短名时，先按[模型和偏好](#模型和偏好)配置可用模型，再将示例里的模型名替换成自己的选择。

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

### 继续追问

如果上一轮外部模型返回了可继续的会话，直接让当前 Agent 追问它：

```text
继续追问刚才那个 Claude reviewer：
它提到的 P1 里哪些必须现在修，哪些可以发版后处理？
```

你不需要自己管理 session id；Agent 会从 ai-dispatch 的返回结果里拿。

## 安装

终端使用，安装 CLI（需要 Node.js 18+）：

```bash
npm install -g ai-dispatch
ai-dispatch doctor
```

<details>
<summary>Agent skill、Homebrew 和其他安装方式</summary>

在 Claude Code / Codex 中用自然语言派发时，按当前使用的 Agent 安装对应 skill：

```bash
# Claude Code
npx skills add rennzhang/ai-dispatch -g --agent claude-code

# Codex
npx skills add rennzhang/ai-dispatch -g --agent codex
```

`npx skills add` 只安装 skill；首次调用时会按需下载对应版本的 CLI。

- [Homebrew、npx、无 Node.js 或同时安装 CLI 与 skill](docs/technical.md#安装形态)
- [检查安装与实际运行的版本](docs/technical.md#检查安装)

</details>

## 模型和偏好

短名必须写进 ~/.ai-dispatch/config.json 的 models。没写进去的 kimi / qwen / gpt5.5 这类词会直接失败。

你也可以直接说 provider 名：`codex`、`claude`、`cursor`、`grok`、`opencode`、`omp`、`antigravity`（或 `gemini`）。

ai-dispatch 有两个用户态文件：

- `~/.ai-dispatch/config.json` 的 `models` 字段：你这台机器已添加的模型短名和候选池。
- `~/.ai-dispatch/config.json` 的 `user_facing_summary`：是否在回复里贴调用卡片，默认关闭。设为 `true` 开启。
- `~/.ai-dispatch/preferences.md`：你喜欢在什么场景用哪些模型。

短名不会开箱即用。先把确认能用的模型写进 config.json models，再在 preferences.md 里写场景偏好。

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

## Reasoning effort

跨 provider 可用顶层 `--effort` 控制推理档位：

```bash
ai-dispatch send gpt5.6 "implement the fix" --effort xhigh --json-result
```

Codex 短名（gpt5.6-sol 等）需要写进 config.json。裸 send codex 使用 Codex CLI 自己的默认模型。

合法值：`auto | none | minimal | low | medium | high | xhigh | max`。省略或 `auto` 表示不覆盖各 CLI 默认。精确档位只在该候选 config `efforts` 声明列表内传递；未声明或不在列表内回到 `auto`，结果里会带 `requested_effort` / `applied_effort` / `effort_fallback_reason`，不会静默降到相邻档。详见 [Reasoning Effort 设计](docs/reasoning-effort-design.md)。

## Fast mode

所有 provider 统一接受顶层 `--fast`：

```bash
ai-dispatch send gpt5.6-luna "implement the fix" --fast --json-result
```

- **支持范围**：当前 Codex GPT-5.5/5.6 target 会应用 priority service tier。
- **未生效时**：其他 provider 暂未确认有等价能力时仍按标准速度执行一次，并在结果里明确返回 `requested_fast=true`、`applied_fast=false` 和 `fast_fallback_reason`；不会静默声称已加速，也不会因此设置 `degraded` 或写入通用 `warnings`。
- **查询方式**：切换前可用 `ai-dispatch models resolve <target> --format json` 读顶层 `fast`。

## 安全

ai-dispatch 调的是你本机已安装的 AI CLI。某些 provider 可以读写文件或执行命令；只在你愿意交给这些 CLI 的工作区里使用。

Grok provider 为了非交互式执行默认使用 `grok.approval=always`。处理不可信 prompt 或不希望自动批准工具/文件操作时，传 `--provider-opt grok.approval=default`。

Cursor provider 默认只信任工作区（`--trust`），不自动批准工具/文件操作；需要全自动执行时显式传 `--provider-opt cursor.approval=always`。Cursor 模型 ID 自带推理档位后缀（如 `claude-fable-5-thinking-high`），不接收 `--effort` 精确档位；需要其它档位时换用带对应后缀的短名。

OMP provider 默认 `--auto-approve`（`omp.approval=always`）。处理不可信 prompt 或不希望自动批准工具/文件操作时，传 `--provider-opt omp.approval=default`。`--effort` 映射到 omp `--thinking`（`none` → `off`）；能力仍以 config `efforts` 声明为准。

## 技术文档

日常使用先看本 README。需要底层细节时再看：

- [技术篇](docs/technical.md)：CLI、JSON 结果、run history、provider scan、failure_class、发布。
- [架构设计](DESIGN.md)：包边界、请求流程、状态目录。
- [Changelog](CHANGELOG.md)：公开版本变更记录。
- [Provider Acceptance](docs/provider-acceptance.md)：新增或回归 provider 的真实验收 harness。
- [配置参考](skills/ai-dispatch/references/config.md)：`config.json` 字段和模型路由。
- [偏好格式](skills/ai-dispatch/references/preferences.md)：`preferences.md` 怎么维护。
