# 配置

## 配置初始化

`ai-dispatch` CLI 负责执行命令；skill wrapper 只是 Agent 调用入口，可以按需准备同一个 CLI runtime。Go CLI 负责 `~/.ai-dispatch/` 运行态。

首次 `send`/`resume` 时，Go CLI 只确保 `config.json` 和 `preferences.md` 存在，然后继续执行原命令。这个路径不探测 provider。

如果本次创建了 `config.json` 或 `preferences.md`，返回 JSON 附 `first_run: true`、`first_run_hint` 和 `first_run_setup` 字段。后续命令不再出现这些字段。

手动 `init` 会创建配置并探测 provider：

```bash
ai-dispatch init --claude-transport print
ai-dispatch init --force
```

## config.json

路径：`~/.ai-dispatch/config.json`。

```json
{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ],
    "gpt5.5": [
      { "provider": "codex", "model": "gpt-5.5", "fast": true }
    ],
    "gpt6.1-sol": [
      {
        "provider": "codex",
        "model": "gpt-5.6-sol",
        "efforts": ["high", "xhigh", "medium", "low", "max"],
        "fast": true
      }
    ]
  },
  "providers": {
    "claude": { "available": true, "version": "2.1.2" },
    "codex": { "available": true, "version": "0.142.0" },
    "opencode": { "available": true, "version": "1.17.3", "catalog_model_count": 42 },
    "grok": { "available": true, "version": "grok 0.2.93" }
  }
}
```

### 字段说明

| 字段 | 说明 |
|---|---|
| `claude_transport` | Claude 调用方式：`print`、`pty`、`auto`、`disabled` |
| `user_facing_summary` | 是否在 send/resume 收口时写出给用户看的卡片。省略或 `false` 为关；`true` 开启。单次可用 `--wrap-up` / `--no-wrap-up` 覆盖 |
| `models` | 用户已经确认并主动加入的本机模型候选池；key 是短名，value 是按顺序尝试的候选数组 |
| `models.<name>[].provider` | 真实 provider：`codex`、`claude`、`opencode`、`omp`、`antigravity`、`grok`、`cursor` |
| `models.<name>[].model` | 传给底层 provider CLI 的真实 model id |
| `models.<name>[].efforts` | 可选。该候选声明支持的档位列表（亦为偏好挑选顺序）。不是默认 effort。省略/空 = 未声明，send 时精确 `--effort` 一律 applied auto |
| `models.<name>[].fast` | 可选。`true` = 允许（显式传 `--fast` 时才应用，不传永远标准速度）；省略/`false` = 不允许 |
| `providers` | provider CLI 诊断摘要，由 `init` 或 `providers scan` 更新 |
| `providers.<name>.available` | provider CLI 是否看起来可执行 |
| `providers.<name>.version` | provider CLI 版本 |
| `providers.opencode.catalog_model_count` | opencode 已认证 provider 的 catalog 模型数量摘要，不证明逐模型可运行 |

`models` 是可执行短名路由，也是用户自己维护的可用模型候选池；`preferences.md` 是场景偏好，`providers` 是诊断摘要。不要把三者混在一起。

调用卡片默认关闭。要在 Agent 回复里看到 `[ai-dispatch result]` 这块，在 config.json 里打开：

```json
{
  "user_facing_summary": true
}
```

单次覆盖用 `--wrap-up` 或 `--no-wrap-up`。

需要查看用户已经添加并主动维护的模型池时，读 `config.json` 的 `models` 字段：

```bash
ai-dispatch config show
```

`providers.opencode.catalog_model_count` 只是 catalog 数量摘要，不代表这些模型都经过用户确认或真实可调用。

## 执行边界

默认关闭“无输出超时”；只要 provider 没有明确失败或完成，dispatch 就继续等待。默认仍保留 7200 秒（120 分钟）总兜底，可用 `--timeout` 调整；传 `--timeout 0` 可关闭墙钟上限。只有显式传 `--activity-timeout` 才启用无活动超时。启用后，provider 的任意 stdout/stderr 增量事件都会重置计时，只有连续静默达到阈值才会终止；各 adapter 必须优先使用底层 CLI 的流式输出模式。

收到 `SIGINT`、`SIGTERM` 或 `SIGHUP` 时，dispatch 会终止当前 provider 进程树、停止候选降级，并统一返回 canceled 结果（`exit_code=130`、`next_action=done`）。这里的 130 是 ai-dispatch 的“调用已取消”契约，不用于区分信号来源。

顶层 `--effort` / `--fast` 对所有 provider 都是合法输入，但**能力真源是各候选的 config 声明**：

- `efforts`：声明可精确应用的档位；列表顺序供偏好挑选。未声明或不在列表内 → applied `auto` + `effort_fallback_reason`。
- `fast: true`：只是允许；仅在显式 `--fast` 时应用加速，不传 `--fast` 一律标准速度；未声明或 `false` → 标准速度 + `fast_fallback_reason`。
- 不要把 hardcoded provider 提示列表当作执行权威；它们只可用于 init / detect-and-suggest，且须经用户确认后再写入 config。
- 新增/更新/移除 models 候选时，询问用户是否配置 `efforts`/`fast`；未配置保持 auto。

`models resolve <target> --format json` 会暴露各候选已声明的 `efforts` 与 `fast`。

## Grok provider opts

Grok 的推荐入口是 `config.json models` 里的 `grok` 候选链：第一候选走本机 Grok Build CLI，后续候选可以走 OpenCode/OpenRouter 兜底。

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

```bash
ai-dispatch send grok "Reply exactly: OK" \
  --provider-opt grok.max-turns=1 \
  --provider-opt grok.web-search=off \
  --json-result
```

支持的 key：`grok.max-turns`、`grok.web-search=on|off`、`grok.subagents=on|off`、`grok.approval=always|default`。Grok 默认 `subagents=off`，只有用户明确要求被派发模型使用子代理时才传 `on`。推理档位使用顶层 `--effort`（`auto|none|minimal|low|medium|high|xhigh|max`），不要用已移除的 `grok.effort`。

默认 `grok.approval=always` 会向 Grok CLI 传 `--always-approve`，用于非交互式 dispatch。处理不可信 prompt 或不希望自动批准工具/文件操作时，传 `--provider-opt grok.approval=default`。

## OMP provider opts

`omp` 是 oh-my-pi 的一等 provider（`omp --print --mode json`），不是把 DeepSeek 塞进 codex/OpenCode。

```json
{
  "models": {
    "deepseek-flash": [
      { "provider": "omp", "model": "opencode-go/deepseek-flash", "efforts": ["high", "max"] }
    ],
    "dsf": [
      { "provider": "omp", "model": "opencode-go/deepseek-flash", "efforts": ["high", "max"] }
    ],
    "deepseek-pro": [
      { "provider": "omp", "model": "opencode-go/deepseek-v4-pro", "efforts": ["high", "max"] }
    ]
  }
}
```

```bash
ai-dispatch send deepseek-flash "…" --effort high --json-result
ai-dispatch send dsf "…" --effort high --json-result
ai-dispatch send deepseek-pro "…" --effort max --json-result
ai-dispatch send omp --model opencode-go/deepseek-flash "…" --effort max
```

支持的 key：`omp.approval=always|default`。默认 `always` 传 `--auto-approve`。`--effort` 映射为 omp `--thinking`；`none` 对应 `off`。


## 模型解析顺序

```text
显式 provider + --model
> config.json models 短名
> config 候选里的精确 model id
> 裸 provider 名
```

`config.json models` 命中数组时，按顺序尝试候选。`config`、`quota`、`network`、`timeout`、`runtime` 和无法归类的 provider 失败都会继续下一个候选；输入错误、取消、resume 和交互式工具权限拒绝不降级。只顺序遍历候选池一轮，不循环重试整个池。

查看真实解析：

```bash
ai-dispatch models resolve mimo-pro --format json
```

## Provider 扫描

```bash
ai-dispatch providers scan --format json
ai-dispatch providers scan --refresh
```

扫描只更新 `providers` 字段，不更新 `models`，不写完整模型列表，不记录扫描时间。

`providers scan` 不验证订阅、额度、地区封锁或 OpenRouter 单模型 endpoint。真实可运行性以 `send`/`resume` 的返回结果和 `failure_class` 为准。

## 本地状态目录

```text
~/.ai-dispatch/
  config.json
  preferences.md
  runs/
  logs/
  bin/
```

`bin/` 保存稳定 CLI 入口 `ai-dispatch` 和按版本缓存的 release binary。skill wrapper 可以调用自己的本地 binary，也可以按 `VERSION` 下载同一套 release binary。

## 真实 provider 执行

`AI_DISPATCH_GO_PROVIDER_EXECUTION` 默认 `on`。开发/测试时设 `off` 可关闭真实 provider CLI 执行。
