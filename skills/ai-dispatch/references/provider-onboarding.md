# Provider 与模型接入

默认支持的 provider 只有四类：

- `codex`：调用 `codex exec`
- `claude`：调用 `claude -p`，必要时走 PTY
- `opencode`：调用 `opencode run`，承载 OpenRouter 模型
- `antigravity`：调用 `agy --print`，承载 Gemini / Antigravity

Cursor CLI、Augie、Qoder 这类新的本地 CLI 不属于默认 provider。接入它们时按“新增 provider adapter”处理，不要只往 registry 里塞一个模型名。

## 判断：只新增模型 vs 新增 provider

只新增模型时，必须同时满足：

- 仍然走已有 provider：`codex` / `claude` / `opencode` / `antigravity`
- 只是新增模型 key、alias、真实模型 ID 或能力说明
- 没有新的 binary、认证、session、输出协议或权限语义

这种情况只改 registry 和模型路由说明，然后验证：

```bash
~/.ai-dispatch/bin/ai-dispatch models
~/.ai-dispatch/bin/ai-dispatch models resolve <alias> --format json
~/.ai-dispatch/bin/ai-dispatch guide models
```

registry 文件是 `internal/routing/models.json`。`dispatchRunner` 必须等于 provider 的 `Name()` / `providerFor()` 注册名；`dispatchModel` 是传给底层 CLI 的真实模型参数。

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
5. 如 provider 承载模型 registry，在 registry 条目里设置 `dispatchRunner` 和 `dispatchModel`。
6. 补 provider 单元测试，至少覆盖命令拼装、成功解析、失败分类。
7. 跑真实 smoke；如果账号、地区、额度或授权不可用，要写清楚失败原因。

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
go test ./...
~/.ai-dispatch/bin/ai-dispatch doctor --format json
~/.ai-dispatch/bin/ai-dispatch models resolve <alias> --format json
```

真实 smoke：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=on \
~/.ai-dispatch/bin/ai-dispatch send <target> "Reply exactly: OK" \
  --cwd "$PWD" --json-result --stream-progress --task-name provider-smoke-<provider>
```

`AI_DISPATCH_GO_PROVIDER_EXECUTION=on` 显式打开真实 provider CLI 执行，避免测试或 dry-run 环境误触发外部 CLI；正常用户通过已安装 skill 调用时不需要设置。

## 禁止项

- 不为一个新模型新建 provider。
- 不在调用方硬编码底层模型 ID。
- 不在 skill 里写 provider fallback 逻辑。
- 不用空 catch 或默认值掩盖认证、权限、quota、网络错误。
- 不把 Cursor CLI、Augie、Qoder 这类新 CLI 伪装成 `opencode` 或 `codex`。
