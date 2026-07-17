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
      { "provider": "codex", "model": "gpt-5.5" }
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
| `models` | 用户已经确认并主动加入的本机模型候选池；key 是短名，value 是按顺序尝试的候选数组 |
| `models.<name>[].provider` | 真实 provider：`codex`、`claude`、`opencode`、`antigravity`、`grok` |
| `models.<name>[].model` | 传给底层 provider CLI 的真实 model id |
| `providers` | provider CLI 诊断摘要，由 `init` 或 `providers scan` 更新 |
| `providers.<name>.available` | provider CLI 是否看起来可执行 |
| `providers.<name>.version` | provider CLI 版本 |
| `providers.opencode.catalog_model_count` | opencode 已认证 provider 的 catalog 模型数量摘要，不证明逐模型可运行 |

`models` 是可执行短名路由，也是用户自己维护的可用模型候选池；`preferences.md` 是场景偏好，`providers` 是诊断摘要。不要把三者混在一起。

需要查看用户已经添加并主动维护的模型池时，读 `config.json` 的 `models` 字段：

```bash
ai-dispatch config show
```

`providers.opencode.catalog_model_count` 只是 catalog 数量摘要，不代表这些模型都经过用户确认或真实可调用。

## 执行边界

默认关闭“无输出超时”；只要 provider 没有明确失败或完成，dispatch 就继续等待。默认仍保留 1800 秒总兜底，可用 `--timeout` 调整；只有显式传 `--activity-timeout` 才启用无活动超时。

收到 `SIGINT`、`SIGTERM` 或 `SIGHUP` 时，dispatch 会终止当前 provider 进程树、停止候选降级，并统一返回 canceled 结果（`exit_code=130`、`next_action=done`）。这里的 130 是 ai-dispatch 的“调用已取消”契约，不用于区分信号来源。

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

## 模型解析顺序

```text
显式 provider + --model
> config.json models
> 内置 registry
> provider 推断
```

`config.json models` 命中数组时，按顺序尝试候选。只有 `config`、`quota`、`network`、`timeout` 失败会继续下一个候选；`runtime` 默认不降级。

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
