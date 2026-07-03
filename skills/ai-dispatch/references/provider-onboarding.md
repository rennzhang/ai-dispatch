# Provider 与模型接入

默认支持的 provider 只有四类：

- `codex`：调用 `codex exec`
- `claude`：调用 `claude -p`，必要时走 PTY
- `opencode`：调用 `opencode run`，承载 OpenRouter / OpenAI / Google 等模型
- `antigravity`：调用 `agy --print`，承载 Gemini / Antigravity

Cursor CLI、Augie、Qoder 这类新的本地 CLI 不属于默认 provider。接入它们时按"新增 provider adapter"处理，不要只往 registry 里塞一个模型名。

## 三个模型数据源的区别

模型/provider 信息有三个位置，职责不同：

| | config.json `models` | preferences.md | 内置 routing registry |
|---|---|---|---|
| **是什么** | 用户认可的可执行模型路由表：短名 → provider/model 候选数组 | 特定场景下偏好选哪些短名 | 开箱默认兜底路由 |
| **谁维护** | 用户或后续 CLI 辅助命令 | 用户/Agent 按人话维护 | 开发者随源码发布 |
| **AI 怎么用** | `models resolve` 解析真实调用链 | 真实调用前读，用于选择 target | 用户配置没命中时兜底 |

`providers` 字段不参与模型选择，只是本机 provider CLI 诊断摘要。

一句话：**config.models 管"短名怎么调用"，preferences.md 管"场景偏好"，providers 管"本机 CLI 诊断"**。

## Provider 可用性探测

每个 provider 的探测方式：

- `exec.LookPath(<binary>)` 检查 binary 是否在 PATH 中
- 找到后跑 `<binary> --version` 记录版本
- binary 名称：claude → `claude`、codex → `codex`、opencode → `opencode`、antigravity → `agy`
- opencode 额外扫描 catalog 模型数量：跑 `opencode models`，读取 `~/.local/share/opencode/auth.json` 确认已配置的 provider，只统计已认证 provider 在 catalog 中暴露的模型数量；读不到 auth.json 时只统计内置 `opencode/` catalog；这不是逐模型可运行性证明
- 探测超时/失败不阻塞：标记 `available: false`，只记录可分享的错误摘要，不记录本机绝对路径

`providers scan` 不做逐模型真实调用，不验证订阅、额度、地区封锁或 OpenRouter 单模型 endpoint。真实调用失败时再按 `failure_class` 判断：认证、地区、模型访问权归 `config`，额度归 `quota`，网络归 `network`，超时归 `timeout`。

结果存储在 `~/.ai-dispatch/config.json` 的 `providers.<name>` 字段：

```json
{
  "providers": {
    "claude":       { "available": true, "version": "2.1.2" },
    "codex":        { "available": true, "version": "0.142.0" },
    "opencode":     { "available": true, "version": "1.17.3", "catalog_model_count": 42 },
    "antigravity":  { "available": false, "error": "agy binary not found in PATH" }
  }
}
```

## 怎么重新扫描

手动初始化或主动扫描时才探测 provider：

```bash
ai-dispatch providers scan --format json
ai-dispatch providers scan --refresh   # 联网刷新 opencode 模型缓存（从 models.dev）
```

扫描后只更新 config.json 的 `providers` 字段。配置只保存可分享摘要，不保存本机绝对路径、完整模型列表或扫描时间。

## 怎么手动更新 config.json

一般不手动编辑 `providers`。它是本机探测摘要，安装新 provider CLI 或认证变化后重新运行：

```bash
ai-dispatch providers scan --format json
```

## 判断：只新增模型 vs 新增 provider

只新增模型时，必须同时满足：

- 仍然走已有 provider：`codex` / `claude` / `opencode` / `antigravity`
- 只是新增模型 key、alias、真实模型 ID 或能力说明
- 没有新的 binary、认证、session、输出协议或权限语义

开箱默认能力改内置 registry；用户机器上的短名选择改 `config.json models`。

改内置 registry 后验证：

```bash
ai-dispatch models
ai-dispatch models resolve <alias> --format json
ai-dispatch guide models
```

registry 文件是 `internal/routing/models.json`。`dispatchRunner` 必须等于 provider 的 `Name()` / `providerFor()` 注册名；`dispatchModel` 是传给底层 CLI 的真实模型参数。改完需要重新编译发布。

不要给内置 registry 加含糊短名，例如把 `mimo` 固定到某一家 provider。含糊短名应由用户在 `config.json models` 里明确选择：

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

新增 provider 时，出现任一情况就必须新增 adapter：

- 新 CLI 或本地服务，例如 `cursor`、`augie`、`qoder`
- 新认证或账号探测逻辑
- 新 session / resume 语义
- 新 stdout / stderr / JSON 事件格式
- 新 cwd、文件写入、权限确认或 sandbox 语义
- 新失败特征，需要单独分类 config / quota / timeout / network / runtime

## 新增 Provider 最小路径

1. 在 `internal/providers/<provider>/` 新增 provider adapter。文件结构参考 `internal/providers/codex/`、`claude/`、`opencode/` 或 `antigravity/`。
2. 实现 `internal/providers/provider.go` 里的接口：

```go
Name() string
Build(BuildRequest) (runtime.CommandSpec, error)
Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
```

3. 在 `internal/dispatch/dispatch.go` 的 `providerFor()` 注册 provider。
4. 在 `internal/routing/target.go` 里增加 provider target 和需要的 alias。
5. 在 `internal/setup/probe.go` 的 `providerSpecs` 里加探测逻辑。
6. 如 provider 承载模型 registry，在 registry 条目里设置 `dispatchRunner` 和 `dispatchModel`。
7. 补 provider 单元测试，至少覆盖命令拼装、成功解析、失败分类。
8. 跑真实 smoke；如果账号、地区、额度或授权不可用，要写清楚失败原因。

## Adapter 契约

`Build` 只负责拼真实命令：

- 使用 `req.Target.Model`
- 传入 `req.SessionID`
- 尊重 `req.CWD`
- 读取 `req.ProviderOptions`；不认识的 key 直接返回 input/config 错误
- 不做业务 fallback

`Parse` 负责把 provider 输出转成统一 `ProviderResult`：

- 设置 `ProviderUsed`、`ModelUsed`、`RequestedTarget`
- 解析 `session_id`
- 保留有用 stderr
- 设置 `Status`、`FailureClass`、`NextAction`
- 不返回 provider 私有结构

provider adapter 要保持薄。ai-dispatch 负责命令拼装、进程超时、结果解析、runstore 和路由元信息；它不应该膨胀成通用 agent 框架。

## CLI 参数口径

新 provider 的私有参数优先走：

```bash
--provider-opt <provider>.<key>=<value>
```

不要为单个 provider 增加顶层 flag，除非它已经是跨 provider 的通用语义。

## 验证

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
ai-dispatch doctor --format json
ai-dispatch models resolve <alias> --format json
ai-dispatch providers scan --format json
```

真实 smoke：

```bash
ai-dispatch send <target> "Reply exactly: OK" \
  --cwd "$PWD" --json-result --stream-progress --task-name provider-smoke-<provider>
```

默认通过已安装 skill 调用时，真实 provider 执行已开启（`AI_DISPATCH_GO_PROVIDER_EXECUTION=on` 由二进制内部默认设置）。开发/测试时如需关闭，设 `AI_DISPATCH_GO_PROVIDER_EXECUTION=off`。

## 禁止项

- 不为一个新模型新建 provider。
- 不在调用方硬编码底层模型 ID。
- 不在 skill 里写 provider fallback 逻辑。
- 不用空 catch 或默认值掩盖认证、权限、quota、网络错误。
- 不把 Cursor CLI、Augie、Qoder 这类新 CLI 伪装成 `opencode` 或 `codex`。
