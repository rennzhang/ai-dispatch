# ai-dispatch

让当前 AI Agent，用一套命令调用本机不同的 AI coding CLI。

既可以在终端和脚本里调用，也可以作为 Claude Code / Codex 的 skill 使用。dispatch 负责入口、模型、参数和结果格式的适配；模型实际完成了什么，仍需按任务验收。

## 默认与显式选择

默认入口是 **OMP**，默认模型是 **Sol（`gpt-6.1-sol`）**。默认配置不替你安装 CLI 或登录账号；先发现本机入口和模型：

```bash
ai-dispatch init
ai-dispatch providers scan --dry-run --format json
ai-dispatch providers scan --format json
```

`--dry-run` 只预览，不写配置。普通扫描更新同一个 `~/.ai-dispatch/config.json`，不发送模型生成请求。

```bash
# 没有指定 target/model：使用 config defaults
ai-dispatch send --prompt-file /tmp/task.md --cwd "$PWD" --json-result

# 只指定模型：使用默认入口，不自动换 provider
ai-dispatch send sol "检查当前 diff，不修改文件" --cwd "$PWD" --json-result

# 明确指定 Codex App 与模型
ai-dispatch send codex "检查当前 diff，不修改文件" \
  --surface app --model gpt-6.1-sol --cwd "$PWD" --json-result

# 明确指定 Grok Build 与原生模型 ID
ai-dispatch send grok "检查当前 diff，不修改文件" \
  --model grok-4.7 --cwd "$PWD" --json-result
```

显式 provider 优先，模型别名不能占用 `omp`、`codex`、`grok` 等入口名称。`send codex` 不带模型时使用 Codex CLI 自己的模型默认值；不会把全局默认 Sol 强加给显式入口。

显式 `provider + --model` 可以传该入口的原生 ID，即使扫描目录尚未收录。未收录只表示未验证，不作为调用白名单。已知逻辑模型若没有所选入口/渠道的对应关系，会明确报错，不猜 ID、不自动换入口。每次请求只执行选中的一条路径，失败直接返回，不遍历候选或自动 fallback。

## 支持的入口

| Provider | 本机入口 | 渠道 |
| --- | --- | --- |
| OMP | `omp` | CLI |
| Codex | `codex`；App 使用其 bundled CLI | CLI、App |
| Grok | `grok`（Grok Build） | CLI |
| Claude | `claude` | CLI |
| OpenCode | `opencode` | CLI |
| Cursor | `cursor-agent` | CLI |
| Antigravity/Gemini | `agy`（`gemini` 是入口别名） | CLI |

扫描检查支持范围内的本机入口，写入实际发现的 executable 和 surfaces；未安装项只出现在扫描报告。可以列模型的入口继续发现模型；不支持列举或读取失败会明确报告，不能当成“没有模型”。扫描不会安装 CLI、修改其配置或替你登录。发现目录也不证明账号额度和每次请求一定可用。

## 一个配置文件

`~/.ai-dispatch/config.json` 使用 v2 对象结构：

- `defaults`：用户没指定时怎么选。
- `providers`：本机已发现的入口及绝对 executable 路径。
- `models`：逻辑模型、别名，以及 `targets.<provider>.<surface>` 对应的原生模型 ID 和能力。

同一个 Sol 可以同时对应 OMP、Codex CLI 和 Codex App。Magpie 仅在经过它的 OMP selector 中出现，不是模型目录的统一依赖。扫描生成的 target 标记为 `origin: "scan"`，手工维护的标记为 `origin: "manual"`；扫描保留手工条目、别名和 defaults。

配置中不保存 CLI 版本号。顶层 `version: 2` 是配置结构版本，用于明确迁移，与 provider 版本无关。详细示例见[配置参考](skills/ai-dispatch/references/config.md)。

已有 v1 候选数组配置先预览迁移：

```bash
ai-dispatch config migrate --dry-run --format json
ai-dispatch config migrate --apply --format json
ai-dispatch providers scan --dry-run --format json
```

迁移会报告取消候选顺序、保留映射和冲突。冲突不写入；应用时先保留原文件备份。新 runtime 不在调用时悄悄兼容 v1，也不会在迁移时顺便升级二进制。

`~/.ai-dispatch/preferences.md` 仍可保存用户的场景偏好，供 Agent 阅读；它不承载可执行路由，不覆盖用户明确要求或 config defaults。不要从偏好文档反推模型全集。

## Codex App

新建 App 对话显式写 `codex --surface app`。恢复由 dispatch 记录的会话时，provider、surface 和模型从原会话继承：

```bash
ai-dispatch resume --session-id <id> "只检查这次改动" --json-result
```

显式要求换到另一渠道会报错，不会用 CLI 去接管 App 线程。没有 dispatch 历史的 session 需要明确入口/渠道，避免猜测。

App 需要 macOS、已安装客户端和辅助功能授权。投递绑定目标 App 的 bundle/PID，必要时打开该安装。显式 `AI_DISPATCH_CODEX_APP_PID` 可指定目标；默认排除带 `--user-data-dir` 的自定义 profile 实例，再按进程祖先、唯一 active 或唯一 GUI 窗口选择，仍有歧义时拒绝。URL 使用定向 PID 的 GURL AppleEvent，不能用 `open -a` 在同 bundle 的多个进程间猜测。准备阶段只操作 PID 绑定的精确线程窗口，通过原生 AX 菜单验证模型/effort/速度；自定义强度控件使用定向该 PID 的单步箭头并逐次回读，边界取菜单实际档数。**App 后端模型目录不等于 GUI 当前可选预设**；扫描不切换 GUI，也不保证该组合可以入队。

临时后端只创建线程元数据或向冷队列写消息，退出后由原 GUI 执行；不使用独立 daemon 或 CLI 模型回合做 fallback。实例准备阶段串行，同 session 防重复投递；模型等待期允许其他线程准备。正文首行保持 `[ai-dispatch]`，App 另带唯一 `dispatch-id` 关联本次回执。

只有本次匹配 turn 的完成终态及 `turn_context` 验证通过才能成功。排队成功、部分输出、旧回合完成事件均不能替代。`app_turn_active` 或 `app_delivery_pending` 表示先检查原线程，不能靠再次发送制造进展。

取消只结束 dispatch 等待，**不保证 GUI 拥有的回合停止**；结果保留 session 和警告。App 回合不受 CLI 的 `--timeout` 限制；支持显式 `--activity-timeout`，未指定时保留投递拾取保护。

## Effort、Fast 与结果

`--effort auto|none|minimal|low|medium|high|xhigh|max` 控制推理意图。精确档位由本次选中的 `targets.<provider>.<surface>.efforts` 声明；缺失或不支持时保持 auto，并返回 `effort_fallback_reason`。能力不能跨入口照搬，也不静默降到相邻档。

`fast: true` 只允许显式 `--fast` 生效，不会默认开启。未声明或不支持时标准速度执行并返回 `fast_fallback_reason`。能力回退不触发第二次模型调用。

```bash
ai-dispatch models resolve sol --format json
ai-dispatch models resolve codex --surface app --model sol --format json
ai-dispatch version --format json
```

结果以 JSON 的 `ok`、`status`、`provider_used`、`model_used`、`surface`、`session_id`、`failure_class` 和实际产物为准。默认不贴调用卡片；配置 `user_facing_summary: true` 或单次 `--wrap-up` 可开启。模型回合成功不等于业务验收完成。

## 安装

```bash
npm install -g ai-dispatch
```

Agent skill：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
npx skills add rennzhang/ai-dispatch -g --agent codex
```

skill wrapper 按 `VERSION` 使用或下载经校验的 release binary。v0.11.0 使用 v2 配置；升级已有 v1 配置时先按上文执行 `config migrate`，安装器保留用户配置。检查实际构建用 `version --format json` 的 `version`、`revision`、`modified`。

其他安装方式见[技术篇](docs/technical.md#安装形态)。

## 安全与文档

底层 CLI 可能读写文件、执行命令。OMP 默认自动批准（`omp.approval=always`），Grok 默认 `grok.approval=always`；需要保留工具批准边界时显式传 `--provider-opt omp.approval=default` 或 `grok.approval=default`。Cursor 默认只信任工作区，不自动批准工具操作。

- [技术篇](docs/technical.md)：协议、结果、扫描、运行记录和发布。
- [架构设计](DESIGN.md)：模块边界与单次请求链路。
- [配置参考](skills/ai-dispatch/references/config.md)：v2 字段、迁移与对应关系。
- [偏好参考](skills/ai-dispatch/references/preferences.md)：场景偏好的维护边界。
- [Provider Acceptance](docs/provider-acceptance.md)：真实验收标准。
- [Changelog](CHANGELOG.md)：发布历史与变更。
