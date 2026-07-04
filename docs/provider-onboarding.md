# Provider Adapter Onboarding

本文面向负责 fork 源码或向本仓库贡献 provider adapter 的 AI/开发者。安装后的 skill 只能调用已有 provider；新增 CLI provider 需要改 Go 源码、补测试并重新发布。

ai-dispatch 不关心某个外部 CLI 自己怎么设计。接入者需要自己阅读那个 CLI 的官方文档、运行 `--help`、做本地 smoke，把它的调用方式试出来。本文只规定一件事：如何把那个 CLI 接到 ai-dispatch 的统一协议层。

## 当前边界

当前内置 provider：

- `codex`：`codex exec`
- `claude`：`claude -p` 或 PTY driver
- `opencode`：`opencode run`
- `antigravity`：`agy --print`，通过内置 `__agy-driver` 包装

新 CLI，例如 Copilot CLI、Cursor CLI、Augment CLI、Kimi CLI，应该新增 provider adapter。不要把新 CLI 伪装成 `codex`、`opencode` 或其他已有 provider。

当前不开放插件式 external provider protocol。新增 provider 走源码 adapter、测试、release 或 PR。

## 统一协议层

一次调用从 CLI 到 provider 的主链路是：

```text
internal/cli
  -> contract.DispatchRequest
  -> routing.Resolve(...)
  -> routing.DispatchTarget
  -> dispatch.providerFor(...)
  -> providers.BuildRequest
  -> provider.Build(...)
  -> runtime.CommandSpec
  -> runtime.RunProcess(...)
  -> provider.Parse(...)
  -> contract.ProviderResult
```

新增 provider 必须接入这条链，不要绕过 dispatch、runtime、runstore 或 result contract。

## DispatchRequest

CLI 参数会被解析成 `contract.DispatchRequest`：

```go
type DispatchRequest struct {
    Command                string
    Target                 string
    Prompt                 string
    PromptFile             string
    OutputFile             string
    Model                  string
    CWD                    string
    SessionID              string
    SessionProvider        string
    JSONResult             bool
    StreamProgress         bool
    TimeoutSeconds         int
    ActivityTimeoutSeconds int
    TaskName               string
    ProviderOpts           map[string]map[string]string
}
```

provider adapter 不直接读取 CLI argv。它只通过 `providers.BuildRequest` 接收已经解析好的字段。

字段边界：

- `OutputFile` 由 dispatch 在 provider 成功后写入，adapter 不处理。
- `JSONResult` 由 CLI 输出层处理，adapter 不处理。
- `StreamProgress` 由 dispatch/runtime 的 stdout/stderr hook 处理，adapter 不处理。
- `TimeoutSeconds` 和 `ActivityTimeoutSeconds` 由 runtime 统一执行；adapter 只有在底层 CLI 也需要内部 timeout 参数时才读取。

## Routing Target

`routing.Resolve(target, model)` 把用户输入解析成 `routing.DispatchTarget`：

```go
type DispatchTarget struct {
    Requested  string
    Provider   string
    Model      string
    Source     string
    ModelKey   string
    ActualID   string
    Candidates []RouteCandidate
}
```

新增 provider 时必须处理这些入口：

- `internal/routing/target.go`
  - `Resolve(...)`：如果 provider 有裸 target，例如 `copilot`，在这里解析。裸 target 指只写 provider 名就能运行的入口，类似 `codex`、`claude`、`opencode`。
  - `implementedProvider(...)`：加入 provider 名。漏掉时，registry 条目会被跳过；`config.json models` 引用这个 provider 会直接报错。
  - `SupportedTargets()`：如果 provider 支持裸 target，加入列表，让 `models` 和补全能发现它。
- `internal/routing/models.json`
  - 如果要开箱支持某些模型短名，加入 registry 条目。
  - `dispatchRunner` 必须是已实现的 provider runtime 名，或 `normalizeProvider(...)` 已知的别名。当前只有 `gemini` 会归一到 `antigravity`。
  - `dispatchModel` 是传给底层 CLI 的真实模型参数。

只是在已有 provider 下新增模型时，不要新增 provider。用户本机的私有短名优先放 `~/.ai-dispatch/config.json` 的 `models` 字段；源码内置默认才改 `internal/routing/models.json`。

## Provider Adapter

新增目录：

```text
internal/providers/<provider>/
  <provider>.go
  <provider>_test.go
```

实现接口：

```go
type Provider interface {
    Name() string
    Build(BuildRequest) (runtime.CommandSpec, error)
    Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
}
```

`Name()` 返回稳定小写 provider 名，例如 `copilot`。这个名字要和 routing、`providerFor()`、`providerOpts`、probe、registry 里的名字一致。

### Build

`Build` 只负责把统一请求转成真实命令：

```go
type BuildRequest struct {
    Prompt                 string
    PromptFile             string
    Target                 routing.DispatchTarget
    CWD                    string
    SessionID              string
    TimeoutSeconds         int
    ActivityTimeoutSeconds int
    ProviderOptions        map[string]string
}
```

返回：

```go
type CommandSpec struct {
    Args  []string
    CWD   string
    Env   []string
    Stdin []byte
}
```

要求：

- 用 `req.Target.Model` 作为底层 CLI 的 model 参数。
- 用 `req.Prompt` 或 `req.PromptFile` 传入任务。
- 如果底层 CLI 支持继续会话，用 `req.SessionID`。
- 如果底层 CLI 需要项目目录参数，用 `req.CWD`。dispatch 也会把 `spec.CWD` 设置成请求的 cwd。
- 如果要读 provider 私有参数，只读 `req.ProviderOptions`。
- 返回 `runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil)}`，除非该 provider 明确需要额外环境变量。
- 不在 `Build` 里执行命令。
- 不做 fallback。
- 不吞掉不支持的参数。参数不合法时返回 error，让 dispatch 转成 input/config failure。

prompt 文件不要随意展开到 argv 里。优先使用底层 CLI 的文件参数或 stdin，避免把长 prompt 泄露到进程列表。

如果 adapter 在 `Build` 内解析 binary，binary 缺失的 error 文本必须包含 `binary not found`。`dispatch.buildFailure(...)` 目前只用这个字面量把 Build error 归为 `config`；其他 Build error 会归为 `input`。如果 adapter 像 `codex` / `claude` 那样不在 Build 内解析 binary，则 binary 缺失会在 runtime 执行失败后由 `diagnostics.Classify(...)` 归类。

### Parse

`Parse` 把 `runtime.RunResult` 转成统一 `contract.ProviderResult`：

```go
type RunResult struct {
    Stdout          []byte
    Stderr          []byte
    ExitCode        int
    DurationMS      int64
    TimedOut        bool
    FixedTimeout    bool
    ActivityTimeout bool
    Error           string
}
```

成功结果必须设置：

- `SchemaVersion: "2.0"`
- `OK: true`
- `Status: contract.StatusSuccess`
- `Text`
- `ProviderUsed`
- `ModelUsed`
- `SessionID`，如果底层 CLI 有会话 id
- `RequestedTarget`
- `RouteTrace`
- `RouteSteps`
- `ExitCode`
- `DurationMS`
- `Warnings`
- `NextAction: contract.NextDone`

失败结果必须设置：

- `OK: false`
- `Status`
- `Stderr`
- `FailureClass`
- `NextAction`
- `ExitCode`
- `DurationMS`

推荐复用：

```go
diagnostics.Classify("<ProviderName>", stdout, stderr, run.Error)
diagnostics.TimeoutMessage(...)
diagnostics.NoResultMessage(...)
contract.NextActionForFailure(failure, "<provider>")
```

`diagnostics.Classify(...)` 里已经有少量 provider-specific 分支，例如 OpenCode 的地区/模型不可用、Antigravity 的无输出提示。新 provider 如果有独有的认证、地区、模型访问权、输出协议错误，需要同步更新 `internal/diagnostics/classify.go`，不要假设通用分类能覆盖所有失败。

调用 `diagnostics.Classify("<ProviderName>", ...)` 后，如果要把空结果升级成 `diagnostics.NoResultMessage(...)`，provider 显示名要保持一致。现有 adapter 都用同一个显示名生成并匹配 `"<ProviderName> returned no successful result"`，大小写不一致会让 NoResult 摘要失效。

常见失败分类：

- `input`：ai-dispatch 调用参数不合法。
- `config`：binary 缺失、认证、权限、账号、地区、模型访问权、provider 配置问题。
- `quota`：额度或 rate limit。
- `network`：网络、DNS、连接、TLS。
- `timeout`：墙钟或无活动超时。
- `runtime`：provider 进程异常、输出协议异常、JSON 解析失败、无可用 final result。

不要把 provider 私有 JSON 原样暴露给调用方。`ProviderResult` 是唯一对外协议。

## Provider Options

provider 私有参数统一走：

```bash
--provider-opt <provider>.<key>=<value>
```

当前 CLI 在 `internal/cli/cli.go` 的 `validProviderOpt(provider, key)` 做白名单校验。新增 provider option 时必须同步更新这里，否则参数到不了 adapter。

不要为单个 provider 新增顶层 flag，除非它已经是跨 provider 的通用语义。

## 注册点

新增 provider 至少要改这些位置：

1. `internal/providers/<provider>/`
   - 新增 adapter 和测试。
2. `internal/dispatch/dispatch.go`
   - 在 `providerFor(name)` 注册 provider。
3. `internal/routing/target.go`
   - 在 `Resolve(...)`、`implementedProvider(...)`、必要时 `SupportedTargets()` 接入 provider。
4. `internal/setup/probe.go`
   - 在 `providerSpecs` 加 binary 探测。
5. `internal/cli/cli.go`
   - 如果有 provider 私有参数，更新 `validProviderOpt(...)`。
   - 更新 `printSetupSummary(...)` 里的 provider 展示顺序，否则首次初始化摘要不显示新 provider。
6. `internal/cli/providers.go`
   - 更新 `providers scan` 文本输出里的 provider 展示顺序；`--format json` 会输出 map，但默认 text 输出有硬编码顺序。
7. `internal/diagnostics/classify.go`
   - 如果新 provider 有独有错误模式，补 provider-specific 分类。
8. `internal/routing/models.json`
   - 如果需要开箱短名，新增 registry 条目。
9. docs
   - 需要用户知道的新 provider target，更新 README 或技术文档中支持范围。

如果只是已有 provider 下的新模型，通常只改 `models.json`，或者让用户写 `config.json models`，不走新增 provider。

## Provider Scan

`providers scan` 只写 `~/.ai-dispatch/config.json` 的 `providers` 诊断摘要。它不参与模型选择，也不证明模型真实可运行。

新增 provider 需要在 `internal/setup/probe.go` 加 `providerSpec`：

```go
var providerSpecs = map[string]providerSpec{
    "provider": {
        binary:      "provider-cli",
        envOverride: "AI_DISPATCH_PROVIDER_BIN",
        fallbacks:   []string{"~/.provider/bin/provider-cli"},
        listsModels: false,
    },
}
```

probe 要求：

- 用 `exec.LookPath` 或安全 fallback 找 binary。
- 支持必要的 env override。
- 当前 probe 默认跑 `<binary> --version`，失败时标记 unavailable。
- 如果某个 CLI 不支持 `--version`，先扩展 `providerSpec` / `probeProvider`，不要把 scan 逻辑塞进 provider adapter。
- error 摘要不能泄露本机绝对路径。
- 不做真实模型调用。
- 不把完整模型 catalog 写进 config。

只有像 OpenCode 这种 catalog 数量有诊断价值时，才设置 `listsModels: true` 并增加 count 类摘要。当前 list 逻辑是按 OpenCode 写死的；新 provider 需要 catalog count 时，应先扩展 probe 逻辑，而不是复用 OpenCode 的 scanner。

## Session / Resume

resume 由 `session_id` 驱动。dispatch 会优先从 runstore 查上一轮结果，并校验 provider 是否一致。

provider 支持 resume 时：

- `Build` 把 `req.SessionID` 映射到底层 CLI 的 resume/session 参数。
- `Parse` 从输出里解析新的或沿用的 `session_id`。
- 单元测试覆盖 send 输出 session id 和 resume 命令拼装。

provider 不支持 resume 时：

- 不要伪造 session id。
- `Parse` 留空 `SessionID`。
- 文档或测试里说明不支持 resume。

fallback 只发生在 `send`，不会在 `resume` 中跨 provider 降级。

## Candidate Fallback

`config.json models` 可以把一个短名映射成多个候选：

```json
{
  "models": {
    "example-pro": [
      { "provider": "opencode", "model": "openrouter/vendor/model-a" },
      { "provider": "opencode", "model": "opencode/model-b" }
    ]
  }
}
```

dispatch 会在这些 failure class 下尝试下一个候选：

- `config`
- `quota`
- `network`
- `timeout`

如果 `FailureClass == nil`，这些 status 也会触发下一个候选：

- `quota`
- `timeout`
- `disabled`
- `not_found`

`runtime` 默认不降级。权限拒绝会被短路，不降级。adapter 不要自己实现 fallback。为了避免歧义，adapter 失败时应尽量显式设置 `FailureClass`。

## AI 接入流程

给 AI 的推荐执行顺序：

1. 读本文和当前四个 provider adapter，选一个最接近的 adapter 作为参考。
2. 用本机真实 CLI 或官方文档确认新 CLI 的最小调用方式：
   - 一次性 prompt 怎么传。
   - model 怎么传。
   - cwd/project 怎么传。
   - prompt file 或 stdin 怎么传。
   - session/resume 是否支持。
   - 输出是纯文本、JSON、NDJSON 还是混合日志。
   - 认证、quota、地区、模型不可用时分别输出什么。
3. 写 `internal/providers/<provider>/<provider>.go`。
4. 注册 routing、dispatch、probe、provider opts。
5. 补测试，不先跑真实 provider。
6. 用 fake binary 测命令拼装和解析。
7. 再用真实 CLI 做 smoke。
8. 最后更新文档和 issue/PR 说明验证边界。

外部 CLI 的调研细节不需要写进 ai-dispatch 文档。ai-dispatch 只保留 adapter 接入规范、真实路由和测试证据。

## 单元测试要求

每个新 provider 至少要有 `internal/providers/<provider>/<provider>_test.go`，覆盖：

- `Build` 生成的命令参数。
- `Build` 对 `Prompt` 的处理。
- `Build` 对 `PromptFile` 的处理，确认 prompt 内容不会泄露到 argv。
- `Build` 对 `SessionID` 的处理，如果支持 resume。
- `Build` 对 `ProviderOptions` 的处理。
- 如果 adapter 在 Build 内解析 binary，覆盖 binary 缺失或 override 失败，并确认 error 文本含 `binary not found`，最终归为 `FailureConfig`，且不泄露私有绝对路径。
- `Parse` 成功输出，能提取 final text。
- `Parse` 能提取 session id，如果支持。
- `Parse` 对空结果输出 `NoResultMessage` 类摘要。
- `Parse` 对 timeout 返回 `FailureTimeout`。
- `Parse` 对 auth/config/quota/network/runtime 的典型 stderr/stdout 做正确分类。

涉及 routing/setup/CLI 的变更，还要补对应测试：

- `internal/routing/target_test.go`
  - provider 裸 target 可解析。
  - registry alias 可解析。
  - `models resolve <target>` 返回正确 provider/model。
- `internal/setup/probe_test.go`
  - binary override、fallback、错误脱敏。
- `internal/cli/cli_test.go`
  - 新 `--provider-opt` 白名单。
  - help/parse 不触发真实 provider。
  - `providers scan` 的 text 输出包含新 provider。
  - 首次初始化摘要包含新 provider。
- `internal/dispatch/dispatch_test.go`
  - provider 注册后能进入正确 provider。
  - resume provider mismatch 会拒绝。
  - candidate fallback 行为符合 failure class 规则。

## 必跑验证

改完后至少跑：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
go vet ./...
git diff --check
scripts/go_active_caller_check.sh
```

检查路由：

```bash
go run ./cmd/ai-dispatch models --format json
go run ./cmd/ai-dispatch models resolve <target> --format json
go run ./cmd/ai-dispatch providers scan
go run ./cmd/ai-dispatch providers scan --format json
```

真实 provider smoke：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=on \
  go run ./cmd/ai-dispatch send <target> "Reply exactly: OK" \
  --cwd "$PWD" \
  --json-result \
  --stream-progress \
  --timeout 120 \
  --activity-timeout 60 \
  --task-name provider-smoke-<provider>
```

如果支持 resume，再跑：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=on \
  go run ./cmd/ai-dispatch resume --session-id <session-id> \
  --target <target> \
  "Reply exactly: OK_AGAIN" \
  --json-result \
  --stream-progress \
  --task-name provider-smoke-<provider>-resume
```

如果真实 CLI 受账号、订阅、地区或额度限制无法 smoke，PR 或提交说明里必须写清楚阻塞原因，并保留 fake binary 单元测试证据。

## PR 检查清单

提交前确认：

- provider 名小写稳定，所有注册点一致。
- 没有绕过 `runtime.RunProcess`。
- 没有在 adapter 里写 fallback。
- 没有新增全局顶层 CLI flag 来服务单个 provider。
- 没有把私有路径、token、完整环境变量、完整模型 catalog 写入输出或配置。
- `ProviderResult` 字段完整，`failure_class` 和 `next_action` 可用于上层判断。
- `models resolve` 能解释新增 target。
- `providers scan` 能给出不泄露隐私的诊断摘要。
- 单元测试和真实 smoke 或明确的外部阻塞说明齐全。
