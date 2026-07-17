# Reasoning Effort 技术设计

状态：Implemented（核心合同、测试与真实 smoke 已完成；Codex GPT-5.6 档位矩阵压测 10/10 通过）

最后核对：2026-07-17

本文定义 ai-dispatch 如何用一个统一的 `effort` 请求语义控制 Codex、Claude、OpenCode、Grok 和 Antigravity，同时保留各 CLI 自己的默认行为。

## 结论

ai-dispatch 新增顶级参数：

```bash
ai-dispatch send <target> "prompt" --effort <level>
```

统一档位集合：

```text
auto | none | minimal | low | medium | high | xhigh | max
```

核心规则：

1. 省略 `--effort` 与显式传 `--effort auto` 完全等价。
2. `auto` 表示 ai-dispatch 不覆盖档位，让选中的 CLI、模型和已有配置使用自己的默认行为。
3. 显式档位只有在当前 provider 和模型确认支持时才传递。
4. 显式档位不受支持或无法确认时，改用 `auto`；绝不映射成相邻的较低档位。
5. 切换到 `auto` 必须出现在结构化结果中，不能静默发生。
6. 档位解析在 provider 进程启动前完成；不允许先执行失败，再用 `auto` 重跑同一任务。
7. 原始 `requested_effort` 在路由候选之间保持不变，每个候选根据自己的能力独立解析。

`auto` 不是模型真实档位。它只说明 ai-dispatch 没有施加档位覆盖。因此结果字段使用 `applied_effort`，不使用容易误导的 `effective_effort`。

## 目标

- 给 Agent、脚本和人提供同一个跨 provider 的档位入口。
- 保证请求不会被 ai-dispatch 静默降档。
- 让档位请求能够穿过模型短名、候选路由和 provider fallback。
- 保持 provider adapter 薄：核心层只理解统一语义，adapter 只负责能力判断和命令翻译。
- 保持运行结果可审计，明确区分“用户请求”“dispatch 实际传参”和“CLI 不可观测的默认行为”。

## 非目标

- 不根据 prompt 复杂度自动选择档位。
- 不维护一张全局“所有模型 × 所有档位”静态表。
- 不把 OpenCode 的任意自定义 variant 都解释为 reasoning effort。
- 不把 OpenAI Pro mode、Codex 自动委派、Grok multi-agent agent count 等执行策略混进 effort。
- 不推断或声称 CLI 在 `auto` 下最终采用了哪个真实档位。
- 不改变模型路由、账号 fallback、quota fallback 或 session 所有权。

## 用户语义

### 省略与 auto

下面两条命令语义相同：

```bash
ai-dispatch send codex "review current diff"
ai-dispatch send codex "review current diff" --effort auto
```

对于存在独立档位参数的 CLI，adapter 不添加任何档位参数：

- Codex：不传 `model_reasoning_effort` 配置覆盖。
- Claude：不传 `--effort`。
- OpenCode：不传 `--variant`。
- Grok：不传 `--reasoning-effort`。

Antigravity 没有独立档位参数，档位包含在模型标签中。它的 `auto` 语义是“不改写路由已经选中的模型标签”；不会为了匹配一个 effort 而切换到另一个标签。如果路由本身没有指定模型，则不新增 `--model` 覆盖，走 agy 默认模型。

### 显式档位

```bash
ai-dispatch send gpt5.6 "implement the fix" --effort xhigh
```

如果当前候选确认支持 `xhigh`，adapter 传递精确值。ai-dispatch 不把 `xhigh` 变成 `high`、`medium` 或其他近似值。

### 不支持时回到 auto

```text
requested_effort = xhigh
provider/model does not confirm xhigh support
applied_effort = auto
provider command is built without an effort override
```

这是一次成功可执行的 effort fallback，不是 input failure，也不是 provider 路由降级：

```json
{
  "ok": true,
  "requested_effort": "xhigh",
  "applied_effort": "auto",
  "effort_fallback_reason": "effort xhigh is not supported by grok/grok-4.5; applied auto",
  "degraded": false
}
```

`degraded` 继续只表示 provider 路由发生降级，避免 effort fallback 被 `runs failures` 误判成一次失败。调用方通过 `requested_effort != applied_effort` 判断是否发生 effort fallback，并读取 `effort_fallback_reason` 获取原因。如果同一次请求还发生了候选切换，`degraded=true` 和 `degrade_reason` 只描述路由降级，effort 字段独立保留。

## 请求与结果契约

### DispatchRequest

在 `internal/contract` 定义稳定类型并由 CLI 入口统一校验：

```go
type Effort string

const (
    EffortAuto    Effort = "auto"
    EffortNone    Effort = "none"
    EffortMinimal Effort = "minimal"
    EffortLow     Effort = "low"
    EffortMedium  Effort = "medium"
    EffortHigh    Effort = "high"
    EffortXHigh   Effort = "xhigh"
    EffortMax     Effort = "max"
)

type DispatchRequest struct {
    // existing fields...
    Effort Effort `json:"effort,omitempty"`
}
```

解析结束后，空值立即归一成 `auto`。dispatch、routing 和 provider 不再处理“空值是不是 auto”的分支。

`--effort` 是跨 provider 的通用语义，不能继续放在 `--provider-opt`。现有 `grok.effort` 从白名单移除，输入时返回明确迁移错误：

```text
grok.effort was removed; use --effort
```

不保留两套运行时入口，也不让二者产生优先级问题。

迁移提示由 CLI 在 provider option 白名单校验前检查 retired option 并返回，不能只依赖删除白名单后的通用 `unsupported provider option` 错误。

### ProviderResult

在最终结果增加：

```go
RequestedEffort      Effort `json:"requested_effort,omitempty"`
AppliedEffort        Effort `json:"applied_effort,omitempty"`
EffortFallbackReason string `json:"effort_fallback_reason,omitempty"`
```

字段含义：

- `requested_effort`：用户请求，省略时规范化为 `auto`。
- `applied_effort`：ai-dispatch 传递的精确档位，或 `auto`。
- `applied_effort=auto`：没有档位覆盖；不代表 provider 的真实默认档位名为 auto。
- `effort_fallback_reason`：只有显式档位无法精确应用并回到 `auto` 时出现。
- `degraded=true`：只表示路由候选发生切换，保持现有 `runs failures` 语义。
- `degrade_reason`：只描述路由降级，不混入 effort fallback。

为保留候选级事实，`RouteStep` 增加：

```go
AppliedEffort        Effort `json:"applied_effort,omitempty"`
EffortFallbackReason string `json:"effort_fallback_reason,omitempty"`
```

顶层 `applied_effort` 取最终执行候选的值。原始请求只在顶层保存一次。

## Provider 解析边界

provider 接口增加执行前的 effort 解析：

```go
type EffortRequest struct {
    Model     string
    Requested contract.Effort
}

type EffortResolution struct {
    Requested    contract.Effort
    Applied      contract.Effort
    AppliedModel string
    Fallback     bool
    Reason       string
}

type Provider interface {
    Name() string
    ResolveEffort(context.Context, EffortRequest) EffortResolution
    Build(BuildRequest) (runtime.CommandSpec, error)
    Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
}
```

`AppliedModel` 是交给 `Build` 的最终模型 token 或标签：普通 provider 保持原模型值；Antigravity 可以改成同模型族的精确档位标签；空值明确表示不发送模型覆盖。它避免 resolver 与 driver 各自维护一套 Antigravity 标签逻辑。

`BuildRequest.Effort` 和 `BuildRequest.Target.Model` 只接收已经解析完成的值。adapter 的 `Build` 不再决定是否降级：

```go
type BuildRequest struct {
    // existing fields...
    Effort contract.Effort
}
```

统一解析算法：

```text
requested == auto
  -> applied = auto, fallback = false

provider/model confirms exact support
  -> applied = requested, applied_model = exact model token/label, fallback = false

provider/model confirms unsupported
  -> applied = auto, applied_model = original model or empty override, fallback = true

support cannot be confirmed
  -> applied = auto, applied_model = original model or empty override, fallback = true
```

“未知即 auto”是有意的保守策略：宁可暂时不用一个新档位，也不把请求交给可能自行降档且无法观测的 CLI。provider capability 更新后即可启用精确档位，不需要修改核心层。

## 各 provider 的实现

| Provider | 精确档位传递 | `auto` 行为 | 能力真源 |
| --- | --- | --- | --- |
| Codex | `-c model_reasoning_effort="<level>"` | 不添加该 config override | 精确 allowlist：仅 Sol（`gpt-5.6` / `gpt-5.6-sol`）与 Terra（`gpt-5.6-terra`）开放 `low/medium/high/xhigh/max`；最低档 `none`、`minimal`、Luna 和其他模型一律 auto |
| Claude print | 当前不传（CLI 不支持） | 不添加 `--effort` | 真实 smoke 证实 Claude Code print 拒绝 `--effort`；显式档位一律 fallback 到 auto，保留 ResolveEffort 合同以便将来 CLI 支持后本地启用 |
| Claude PTY | 当前不传（CLI 不支持） | 不添加 `--effort` | 与 Claude print 共用 resolver；同样不向真实 `claude` 子命令添加 `--effort` |
| OpenCode | `--variant <level>` | 不添加 `--variant` | `opencode models <provider> --verbose` 返回的当前模型 variants |
| Grok | `--reasoning-effort <level>` | 不添加该参数 | provider-local 模型规则；multi-agent 语义不作为 reasoning effort |
| Antigravity | 在同一模型族中选择精确匹配的 agy 模型标签 | resolver 先把已知 alias 解析为最终标签；无模型覆盖时不添加 `--model` | `agy models` 的当前标签；`antigravity.bin` 自定义时显式 effort 保守回 auto |

OpenCode 的 `variant` 是比 effort 更宽的概念。ai-dispatch 只允许公共 effort 集合里的同名 variant；用户自定义的 `fast`、`thinking` 等 variant 不通过 `--effort` 暴露。

OpenCode resolver 的确定规则：

- 执行 `opencode models <provider> --verbose`，只查询目标 provider。
- 用完整模型 ID 精确匹配返回记录；别名、缺失记录或空 variants 都视为无法确认。
- 只在目标模型的 variants key 中精确查找公共 effort 名称，不做大小写猜测或近似匹配。
- 查询进程最多使用 5 秒；超时、非零退出、输出无法解析时回到 `auto`，不阻塞 provider 执行。
- 结果按 provider 做进程内 5 分钟 TTL 缓存；缓存只保存命令返回的原始模型能力，不保存某次请求的 resolution。

Antigravity 是唯一需要改写模型标签的 provider。resolver 从 `agy models` 当前输出中，只把匹配 `^(.+) \((Low|Medium|High)\)$` 的标签视为 reasoning effort 标签；模型族是括号前完整且区分大小写的文本。只有模型族与当前已选标签完全相同、档位也精确相同时才允许替换。`Thinking` 等其他括号后缀不参与映射，找不到、命令失败或输出无法解析时回到原始标签，不能跨模型族或选择相邻档位。`agy models` 同样使用 5 秒上限和进程内 5 分钟 TTL 缓存。

已实现：`resolveModelLabel` 空值返回“无模型覆盖”；参数构造只在最终模型标签非空时添加 `--model <label>`。正常 dispatch 路径上，resolver 对非空已知模型先解析出 `baseLabel` 并写入 `AppliedModel`（auto/fallback 也返回 `baseLabel`，精确匹配返回同族目标标签）；driver 直接入口可保留同一 resolver 作防御，但不应再是正常链路的 alias 真源。

Grok resolver 只对已验证为 reasoning-depth 语义的单模型 ID 传递档位。multi-agent 模型、未知模型和动态别名一律视为无法确认并回到 `auto`；不根据名称猜测 agent 数量语义。

## Dispatch 数据流与所有权

`ExecuteWithOptions` 是 CLI 和 Go 编程式调用的共同入口，必须先把空 effort 规范化为 `auto`，保证直接调用 dispatch 时不会绕过契约。

每个候选的执行顺序固定为：

```text
resolve provider
  -> ResolveEffort with a 5s capability-query ceiling
  -> clone candidate with AppliedModel
  -> Build with Applied effort
  -> execute once
  -> Parse
  -> dispatch stamps effort fields and route step
```

effort resolution 的结构化结果由 `executeTarget` 持有，并通过一个 dispatch helper 写入成功、Build 失败、超时和取消结果。provider 的 `Parse` 不负责写 `requested_effort`、`applied_effort` 或 fallback reason；`ensureRouteMetadata` 统一给本候选的 `RouteStep` 写入同一份 resolution。这样不需要修改 `targetExecutor` 签名，也不会被 `finalizeCandidateResult` 的路由降级合并覆盖。

## 路由与 fallback

effort 属于请求，不属于 provider option。执行每个候选前重新解析：

```text
DispatchRequest(requested_effort=xhigh)
  -> candidate 1: codex/gpt-5.6-sol -> applied xhigh
  -> candidate 1 quota failure
  -> candidate 2: opencode/openrouter/... -> exact xhigh unavailable
  -> candidate 2 applied auto, execute once
```

约束：

- 候选切换不能修改 `requested_effort`。
- effort 回退不能跳过当前候选。
- effort 回退不能创建第二次 provider 执行。
- `resume` 不启用候选 fallback；但本轮显式 effort 仍按同一规则解析。
- 路由降级写入 `degraded/degrade_reason`；每个候选的 effort fallback 独立写入对应 route step，最终候选同时写入顶层 effort 字段。

## 兼容性

### CLI

- 新增 `--effort` 是兼容性扩展。
- 省略参数被定义为 `auto`。
- Codex 省略 effort 不再硬编码 `high`，采用 Codex CLI 和所选模型的默认行为。这是有意的行为变化，已写入 changelog。
- `grok.effort` 已移除并给出迁移错误，不维护双入口。

### JSON

- `requested_effort`、`applied_effort` 和 `RouteStep.applied_effort` 都是新增字段。
- 现有字段不删除、不改名，schema 仍保持 `2.0`；只要调用方允许未知 JSON 字段，就可向后兼容。
- `degraded` 和 `degrade_reason` 保持原有路由降级语义，`runs failures` 无需因 effort 功能改变分类逻辑。
- effort fallback 通过新增字段表达，不会把成功执行但使用 CLI 默认档位的请求列为 failure。

### 配置与路由

- `~/.ai-dispatch/config.json` 不新增 effort 默认值，避免配置文件成为第二个隐式策略层。
- 模型短名、候选顺序和 provider inference 不变。
- 不把 capability 表写入共享模型 registry；它由 provider adapter 的实时或已验证真源负责。

## 代码落点

| 位置 | 变更责任 |
| --- | --- |
| `internal/contract/` | Effort 类型、请求字段、结果字段、route step 字段 |
| `internal/cli/cli.go` | `--effort` 解析、默认 auto、合法值校验、移除 `grok.effort` |
| `internal/providers/provider.go` | resolver 接口、EffortRequest、EffortResolution、BuildRequest.Effort |
| `internal/dispatch/dispatch.go` | 入口规范化 auto；每个候选执行前解析 effort；集中写顶层与 route step 的 effort 字段 |
| `internal/providers/{codex,claude,opencode,grok,antigravity}/` | provider-local 能力判断和命令参数映射 |
| `internal/output/`、`internal/runstore/`、`internal/cli/runs.go` | 新字段的 JSON、frontmatter、持久化和安全摘要；不改变 failures 分类 |
| `README.md`、`docs/technical.md`、`docs/provider-onboarding.md`、`skills/ai-dispatch/` | 用户入口、provider 接入合同和 skill 使用说明 |

不新增独立 capability package。只有出现至少两个 provider 共享同一种可验证能力真源时，才考虑提取公共解析代码。

## 验证计划

### 单元测试

1. CLI：省略、显式 auto、所有合法档位、非法值、send、resume、旗标重排。
2. Contract：空值规范化成 auto，JSON 新字段稳定。
3. Provider command spec：
   - auto 时不出现任何独立 effort 参数；
   - 支持时只出现请求的精确值；
   - 不支持和未知时不出现相邻档位；
   - Claude print 与 PTY 共用 resolver，且都永不传 `--effort`（当前 CLI 不支持）；
   - Antigravity 只在同一模型族内选择标签；空模型时 command spec 不包含 `--model`。
4. Routing：原始 effort 穿过候选链，每个候选独立解析。
5. Result：显式档位回到 auto 时包含 requested/applied/fallback reason，但不单独设置 `degraded=true`。
6. Invocation count：effort 回退时 provider 只执行一次。
7. 能力查询：OpenCode 与 Antigravity 的精确匹配、5 秒上限、缓存命中、超时和解析失败回到 auto。
8. 入口一致性：CLI 与直接调用 `ExecuteWithOptions` 的空值都规范化为 auto。
9. 兼容性：旧命令不带 effort 时正常运行；`grok.effort` 由 retired-option 检查返回迁移说明。
10. Codex：省略 effort 不再生成硬编码 high，并用回归测试固定这一有意行为变化。

### 真实 smoke

每个 provider 至少验证：

- `--effort auto` 的真实命令不携带档位覆盖。
- 一个已确认支持的精确档位。
- 一个不支持或无法确认的档位回到 auto，结果清楚标记。
- 有 session 能力的 provider 验证 send + resume。

真实 smoke 必须核对 `provider_used`、`model_used`、`route_trace`、`requested_effort`、`applied_effort`、`effort_fallback_reason`、`degraded` 和 `degrade_reason`，不能只看退出码。Claude 还要执行一次真实、低成本的 print smoke，不能只以 `--help` 或 changelog 作为 CLI flag 可用性的最终验收。

## 实施顺序

1. 增加 Effort contract、CLI 参数和输入测试。
2. 扩展 provider resolver、`AppliedModel` 与 `BuildRequest`。
3. 逐个实现五个 provider 的能力解析和命令映射。
4. 在 dispatch 候选执行前接入解析，补齐结果和 route step。
5. 移除 `grok.effort`，更新技术文档、provider onboarding 和 skill source copy。
6. 运行 unit、vet、diff check，再做真实 provider smoke。

## 验收标准

- 省略 effort 与 `--effort auto` 生成相同的 provider 档位参数。
- auto 下 ai-dispatch 不覆盖支持独立 effort 参数的 CLI。
- 显式档位只能精确传递或回到 auto，代码中不存在高低档位近似映射。
- 不支持或未知时不会先调用 provider 再重试。
- effort 在候选路由间不丢失，每个 route step 都能解释本候选是否回到 auto。
- effort fallback 不污染 `runs failures`，路由降级语义保持兼容。
- auto 结果不声称已观测到 CLI 的真实默认档位。
- 不新增全局 capability registry 或用户配置默认 effort。

## 当前事实依据

本设计以 2026-07-17 的本机 CLI、真实 smoke 和官方文档为当前事实：

- Codex CLI `0.145.0-alpha.18`：通过 `-c key=value` 覆盖配置；省略 effort 时 ai-dispatch **不再**硬编码 `high`。本地可用模型只开放 Sol 与 Terra，精确档位为 `low/medium/high/xhigh/max`；最低档 `none`、`minimal`、Luna 和其它模型一律 auto。[OpenAI GPT-5.6 model guidance](https://developers.openai.com/api/docs/guides/latest-model)
- Claude Code：真实 print smoke 显示 `claude -p ... --effort` 返回 `unknown option '--effort'`。因此 ai-dispatch **当前不对 Claude Code CLI 传 `--effort`**；显式 `--effort` 一律 applied auto 并写 fallback reason。Anthropic 文档中的 effort 描述的是 **API/model 语义**，不能当作本机 Claude Code CLI flag 可用性证明。[Anthropic effort documentation (API)](https://platform.claude.com/docs/en/build-with-claude/effort)
- OpenCode `1.17.18`：`opencode run` 提供 `--variant`，模型目录提供 variants；官方文档明确 variant 是 provider/model-specific，列表并不完整。[OpenCode models and variants](https://opencode.ai/docs/models)
- Grok CLI `0.2.93`：提供 `--reasoning-effort`；xAI 当前 `grok-4.5` 支持 low/medium/high，省略时默认 high，multi-agent 模型中的 effort 语义是 agent 数量而非推理深度。[xAI reasoning documentation](https://docs.x.ai/developers/model-capabilities/text/reasoning)
- Antigravity CLI `1.1.3`：没有独立 effort flag，`agy models` 返回包含 Low/Medium/High 的具体模型标签；resolver 输出最终标签，`antigravity.bin` 自定义时显式 effort 回 auto。[Antigravity CLI repository](https://github.com/google-antigravity/antigravity-cli)

本次真实 smoke 结果：

- Grok `low` 精确应用成功；`xhigh` 未做相邻降档，回到 `auto` 后执行成功，fallback reason 完整且 `degraded=false`。
- Antigravity `gemini-pro + low` 精确解析为 `Gemini 3.1 Pro (Low)` 并执行成功。
- Claude `opus + low` 回到 `auto` 后执行成功，确认命令中不再出现 CLI 不支持的 `--effort`。
- Codex 旧 PATH 入口 `0.142.0` 会被服务端拒绝；切换到本机 ChatGPT App 自带的 `0.145.0-alpha.18` 后，Sol/Terra 的 `low/medium/high/xhigh/max` 共 10 个并发矩阵请求全部成功，平均 14.2 秒、最慢 18.1 秒，模型、requested/applied effort 均精确一致且 `degraded=false`。`none` 另做真实 smoke，确认回到 `auto`、保留明确 reason，并成功执行。
- OpenCode 的能力查询分别确认了精确 `low` 和不支持时回 `auto`，但两次真实生成都触发 wall-clock timeout；结果契约正确，provider 成功路径仍受当前 OpenCode/OpenRouter 运行环境阻塞。

这些外部能力会变化。后续 smoke 必须以当前 CLI 的 `--help`、模型目录和真实 command spec 重新核对，不能把本文快照当永久模型目录。
