---
name: ai-dispatch
description: 通过 ai-dispatch 把任务派发给本机已安装的 AI coding CLI。用于调用 Codex、Claude、OpenCode/OpenRouter、OMP/oh-my-pi、Antigravity/Gemini、Grok 做实现、review、调研、模型对照、provider 路由、session resume，或排查 ai-dispatch run。
---

# ai-dispatch

使用当前 skill 的 `scripts/ai-dispatch`，不要把源码路径当安装入口。OMP、Codex、Grok、Claude、OpenCode、Cursor、Antigravity/Gemini 是同级 provider；默认选择为 **OMP / CLI / Sol（`gpt-6.1-sol`）**。

## 调用前

真实调用前读用户场景偏好和当前配置：

```bash
scripts/ai-dispatch preferences path
scripts/ai-dispatch config show
```

读取返回路径的 `preferences.md`；不存在时可用 `preferences show` 创建空骨架。偏好只给 Agent 参考，执行默认值来自 `config.json defaults`，用户明确要求优先。

v2 配置只有一个 `config.json`：`providers` 是已发现入口，`models` 是逻辑模型及 `targets.<provider>.<surface>` 对应关系。它不是有顺序的候选池，也不是调用白名单。需要发现本机入口和模型时：

```bash
scripts/ai-dispatch providers scan --dry-run --format json
```

预览不写配置；用户要求同步扫描时再去掉 `--dry-run`。扫描不发生成、不安装/登录 CLI，不修改外部模型配置。失败/不支持列举要如实说明，不能报告成空目录。App catalog 不证明 GUI 预设可选或额度可用。

已有 v1 配置先 `config migrate --dry-run --format json`，确认警告和冲突后用 `--apply`；不在 send 时兼容旧数组，不用 `init --force` 覆盖用户已有映射。

## 选择入口与模型

- 无指定：使用 config defaults。inline prompt 与 target 有位置歧义时使用 `--prompt-file`。
- 只说逻辑模型：作为 target，例如 `send sol`；使用默认 provider，不自动挑其他入口。
- 明确 provider + 模型：provider 作 target、模型作 `--model`，例如 `send grok --model grok-4.7`。
- 明确 Codex App：`send codex --surface app --model <model>`。不要因为当前会话在 App 中就自行改渠道。
- 裸 provider 不带模型时使用该 CLI 自己的模型默认值。
- provider 名称保留给执行入口，不能被模型别名覆盖。`gemini` 是 `antigravity` 的入口别名。
- 显式 provider 下允许未收录原生 ID 透传；已知逻辑别名没有对应 target 时明确报错，不猜 ID。
- 不使用 `provider:model` / `model:provider`；不在失败后偷偷换入口、模型或渠道。

调用前可只解析，不发模型请求：

```bash
scripts/ai-dispatch models resolve --format json
scripts/ai-dispatch models resolve sol --format json
scripts/ai-dispatch models resolve codex --model sol --surface app --format json
scripts/ai-dispatch models resolve grok --model grok-4.7 --format json
```

## 派发

```bash
# 默认 OMP / Sol，具体 native ID 来自 config
scripts/ai-dispatch send --prompt-file <path> \
  --cwd "$PWD" --json-result --stream-progress --task-name <name>

# 明确选择模型
scripts/ai-dispatch send sol "<task>" \
  --cwd "$PWD" --json-result --stream-progress --task-name <name>
```

- 项目任务传 `--cwd "$PWD"`；review 带 diff/文件/日志等真源锚点。
- 长 prompt 用 `--prompt-file`；长任务和 review 带具体 `--task-name`。
- 默认补充“不要派发子代理，直接在当前会话完成”。只有用户明确要求被派发模型使用子代理才去掉；Grok 另传 `--provider-opt grok.subagents=on`。
- `--effort auto|none|minimal|low|medium|high|xhigh|max`：精确档位必须在所选 target 的 `efforts` 声明内。未声明/不支持时保持 auto 并报告 `effort_fallback_reason`，不静默降相邻档，也不因此重跑。
- `--fast` 必须显式请求且所选 target 允许 `fast: true` 才生效。不传永远标准速度；未声明/不支持时报告 `fast_fallback_reason`。
- 扫描仅同步可验证能力与 dispatch 支持集合的交集。手动改映射时不捏造 effort/fast；用户未授权的配置不顺手写入。
- 返回模型提供的是输入，最终裁决和任务验收由当前 Agent 负责。
- 正文首行固定为 `[ai-dispatch]`。App 还带每次独特的 `dispatch-id` 用于本次回执匹配；它们是来源/关联标记，不是任务指令。

## Codex App

```bash
scripts/ai-dispatch send codex "<task>" --surface app --model sol \
  --cwd "$PWD" --json-result --task-name <name>
```

App 使用目标安装的 bundled CLI 和原 GUI。只读模型发现仅查询临时后端 `model/list`，不启动或切换 GUI。投递需要 macOS 和辅助功能授权，会打开绑定 PID/bundle 的具体线程并通过原生 AX 操作设置菜单。显式 `AI_DISPATCH_CODEX_APP_PID` 可指定目标；默认排除带 `--user-data-dir` 的自定义 profile，再按祖先、active 或唯一 GUI 窗口选择，仍有歧义时拒绝。URL 通过 PID-addressed GURL 投递，不用 bundle 名猜进程。

临时后端只创建/命名空线程，或确认自己未加载线程后写队列，再退出交由 GUI wake 执行；不启动独立 daemon 或 CLI 模型回合当 fallback。模型/effort/速度须为 GUI 可选择且可验证的组合，目录列出不代表预设一定存在。

准备阶段按 App 实例串行；同 session 防重复提交。等待期间允许其他线程准备。只有唯一匹配的本次 turn 完成且 `turn_context` 参数核对通过才成功；导航、入队、部分输出或旧回合完成均不算。

遇到 `app_turn_active`、`app_session_busy`、`app_delivery_pending`，先检查原线程，不再发一条冒充等待。取消仅停止 dispatch 等待，**GUI 拥有的回合仍可能运行**；保留 session 和 warning，不能声称模型已停止。沿用当前 `CODEX_HOME`，不自动创建隔离 profile。

## 继续会话

只使用真实返回的 `session_id`：

```bash
scripts/ai-dispatch resume --session-id <id> "<delta>" \
  --json-result --stream-progress --task-name <name>-r2
```

已记录的 session 继承原 provider/surface/model，不需要每次重写 `--surface app`。明确指定冲突的入口/渠道会报错；没有 dispatch 历史时需明确足够的目标信息，不能猜。不要把历史全文塞进新 prompt。

## 结果与排障

以返回 JSON 为准，别根据请求 target 猜真实执行结果。当前请求只有一条执行路径，失败直接返回，不在调用方添加 fallback。检查 `ok`、`status`、`provider_used`、`model_used`、`surface`、`session_id`、`failure_class` 和实际产物；能力回退另看 effort/fast 字段。旧运行记录可能含 `degraded`，不代表新 runtime 会遍历候选。

默认不贴结果卡。只有 JSON 有 `agent_hint` 时，原样贴 `user_facing_summary`，不节选、不重算耗时，不输出 `agent_hint` 本身。同一 session 最终只留最新一张。要开启用 config `user_facing_summary: true` 或单次 `--wrap-up`。流式 stderr 保持 NDJSON。

```bash
scripts/ai-dispatch version --format json
scripts/ai-dispatch runs failures --since 24h
```

构建身份比较 `version`、`revision`、`modified`；不要只看 skill 的 VERSION 文件。`+dirty` 是未提交开发构建，不是发布完成或真实链路验收证明。

## 按需参考

- `references/config.md`：v2 对象、扫描、迁移和入口/模型对应关系。
- `references/preferences.md`：场景偏好的用途和更新边界。
