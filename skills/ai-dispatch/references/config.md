# 配置

## 配置初始化

wrapper 负责准备 CLI 二进制；Go CLI 负责 `~/.ai-dispatch/` 运行态。

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
    "opencode": { "available": true, "version": "1.17.3", "catalog_model_count": 42 }
  }
}
```

### 字段说明

| 字段 | 说明 |
|---|---|
| `claude_transport` | Claude 调用方式：`print`、`pty`、`auto`、`disabled` |
| `models` | 用户认可的模型路由表；key 是短名，value 是按顺序尝试的候选数组 |
| `models.<name>[].provider` | 真实 provider：`codex`、`claude`、`opencode`、`antigravity` |
| `models.<name>[].model` | 传给底层 provider CLI 的真实 model id |
| `providers` | provider CLI 诊断摘要，由 `init` 或 `providers scan` 更新 |
| `providers.<name>.available` | provider CLI 是否看起来可执行 |
| `providers.<name>.version` | provider CLI 版本 |
| `providers.opencode.catalog_model_count` | opencode 已认证 provider 的 catalog 模型数量摘要，不证明逐模型可运行 |

`models` 是可执行路由，`preferences.md` 是场景偏好，`providers` 是诊断摘要。不要把三者混在一起。

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

`bin/` 只保存 wrapper 按 `VERSION` 下载的 release binary 缓存；wrapper 本身位于 skill 目录的 `scripts/` 下。

## 真实 provider 执行

`AI_DISPATCH_GO_PROVIDER_EXECUTION` 默认 `on`。开发/测试时设 `off` 可关闭真实 provider CLI 执行。
