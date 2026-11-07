# 技术篇

本文面向要排查、集成或发版 ai-dispatch 的人。普通用户先看 README。

## 安装形态

### npm

有 Node.js 时，推荐通过 npm 安装 CLI：

```bash
npm install -g ai-dispatch
```

也可以不做全局安装：

```bash
npx --yes ai-dispatch doctor
```

npm 包不内置平台二进制。安装时它会下载与 npm 包版本对应的 GitHub Release `.bin` 资产，并按同一 release 的 `SHA256SUMS` 校验后才执行。当前支持 darwin/linux 的 amd64/arm64，要求 Node.js 18+。

通过 npm 安装时，npm 的 `ai-dispatch` 命令直接代理到包内已校验的 release binary；不写入 `~/.ai-dispatch/bin/`。

### Homebrew

Homebrew 用户通过独立 tap 安装：

```bash
brew install rennzhang/tap/ai-dispatch
```

公式下载同一 GitHub Release 的平台 tarball，并使用生成时写入的 SHA-256。公式不是手工维护版本和 hash：`scripts/render-homebrew-formula.sh` 从 release `SHA256SUMS` 生成它。

### 无 Node.js 或同时安装 CLI 与 skill

通过 curl 安装时，默认同时安装 CLI、Claude Code skill 和 Codex skill：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
```

只安装 CLI：

```bash
curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh \
  | AI_DISPATCH_SKILL_TARGET=none bash
```

curl 安装脚本会下载 release tarball，把稳定 CLI 写到 `~/.ai-dispatch/bin/ai-dispatch`，并把 `ai-dispatch` 链接到 `~/.local/bin/`。如果不想创建 PATH 链接，设置 `AI_DISPATCH_LINK_DIR=none`。

### Agent skill

也可以只通过 skill 安装：

```bash
npx skills add rennzhang/ai-dispatch -g --agent claude-code
npx skills add rennzhang/ai-dispatch -g --agent codex
npx skills add rennzhang/ai-dispatch -g --all
```

通过 `npx skills add` 安装时，skill 目录里只有轻量 wrapper。第一次执行 wrapper 时，它会按 `VERSION` 下载对应 GitHub Release tarball，并校验 checksum。

### 检查安装

检查 CLI 状态和当前入口实际运行的构建：

```bash
ai-dispatch doctor
ai-dispatch version --format json
```

要确认 PATH、skill 或 npm 入口实际运行的是哪一份构建，以返回的 `version`、`revision`、`modified` 为准；不要只看安装目录里的 `VERSION` 文件。排查 skill 入口时，用对应的 skill wrapper 执行同一条 `version --format json` 命令。

## 路径

Claude Code 安装入口：

```bash
~/.claude/skills/ai-dispatch/scripts/ai-dispatch
```

Codex 安装入口：

```bash
~/.codex/skills/ai-dispatch/scripts/ai-dispatch
```

稳定 CLI 入口：

```bash
~/.ai-dispatch/bin/ai-dispatch
ai-dispatch  # 如果 ~/.local/bin 在 PATH 中
```

本机运行态：

```text
~/.ai-dispatch/
  config.json
  preferences.md
  runs/
  logs/
  bin/
  desktop-locks/
```

`bin/` 保存稳定 CLI wrapper 和按版本缓存的 release binary。`runs/` 保存每次调用的结构化结果。

## CLI 与单次执行

```bash
# config defaults：OMP / CLI / Sol
ai-dispatch send --prompt-file /tmp/task.md --cwd "$PWD" \
  --json-result --stream-progress --task-name default-task

# 明确入口、渠道、模型
ai-dispatch send codex "review current diff" --surface app --model sol \
  --cwd "$PWD" --json-result --task-name app-review
ai-dispatch send grok "review current diff" --model grok-4.7 \
  --cwd "$PWD" --json-result --task-name grok-review

# 从 dispatch 记录继承 provider/surface/model
ai-dispatch resume --session-id <id> "focus on the delta" \
  --json-result --stream-progress --task-name review-r2
```

程序化调用用 `--json-result`，实时进度用 `--stream-progress`。target 是位置参数；省略 target 时用 `--prompt-file` 或 stdin 单独提供正文，避免把一句 prompt 当模型名。

每次解析出唯一 provider/surface/native-model 后只执行一次。不遍历候选，不在 quota、network、runtime 等失败后切入口。显式 provider 名称保留给入口，不允许模型 key/alias 覆盖。已知模型 key/alias 通过所选入口的 targets 转换；未收录原生 ID 可以在显式 `provider --model` 下透传，但不推断能力。

全局默认只补没有明确指定的选择。裸 `send codex` 不带模型时使用 Codex CLI 自己的默认模型。只指定逻辑模型时使用 config 默认 provider，缺少对应 target 就报错，不挑其他 provider。`provider:model` 冒号语法仍拒绝。

`resume` 从已有运行记录继承 provider、surface、模型和已声明能力，显式冲突渠道/入口会报错。没有 dispatch 记录的 session 必须明确足够的目标信息；不能靠当前默认 OMP 接管未知 session。

CLI 默认墙钟上限为 7200 秒，`--timeout 0` 关闭；无活动超时默认关闭，显式 `--activity-timeout <seconds>` 才启用，stdout/stderr 增量会重置计时。App 回合不套 CLI 墙钟限制：未指定活动超时时使用投递拾取保护，落地后等终态；显式活动超时可以限制静默等待，`--activity-timeout 0` 关闭拾取保护。

收到取消时，CLI 结束拥有的进程组并返回取消状态。App 只停止等待和临时后端；它不拥有 GUI 模型回合，必须保留“GUI 仍可能运行”的 warning，不能声称模型已停止。`exit_code=130` 表示 dispatch 调用取消，session 保留用于核对和恢复。

## v2 配置与迁移

只有 `~/.ai-dispatch/config.json` 承载可执行配置：

- `defaults`：默认 OMP / CLI / `gpt-6.1-sol`。
- `providers`：发现的绝对 executable 与 surfaces；不保存 CLI 版本号。
- `models.<logical-key>`：aliases 与 `targets.<provider>.<surface>` 对象。
- 每个 target 记录 `model` 原生 ID、`origin: scan|manual` 及可选 `efforts` / `fast`。

模型可以对应多个 provider；targets 不表达顺序或 fallback。Magpie 只是具体 OMP selector 的来源之一。一个入口/渠道中的多个不同原生来源保留为不同 key，不用名字相似来合并。

顶层 `version: 2` 是 schema 版本。严格校验未知字段、别名冲突、保留 provider 名和不支持的渠道。defaults/model 未发现或未配置时直接报错，不捏造 Sol 原生 ID。

```bash
ai-dispatch config migrate --dry-run --format json
ai-dispatch config migrate --apply --format json
ai-dispatch providers scan --dry-run --format json
```

迁移默认预览，应用时先以独占文件保留原始备份，再原子替换；冲突不写。旧候选顺序取消，同 provider/surface 的不同模型须显式拆分。新 runtime 不在生成路径中读取 v1 数组，旧 runtime 也不应读取 v2 文件；二进制与配置一起验收、切换。备份和写锁不是第二份活跃路由。

配置写命令持锁后重新读取、合并和校验，避免扫描期间覆盖另一写入者的 defaults/manual 条目。详细 schema 与完整 JSON 见[配置参考](../skills/ai-dispatch/references/config.md)。`preferences.md` 只给 Agent 读场景偏好，不参与执行路由。

## Provider 与模型扫描

```bash
ai-dispatch providers scan --dry-run --format json
ai-dispatch providers scan --format json
ai-dispatch providers scan --refresh --format json
```

`--dry-run` 预览同一份候选 config 和完整报告；不带它才原子保存。`--refresh` 请求 OpenCode 刷新自己的模型缓存。扫描不生成内容、不安装/授权 provider、不修改 OMP/Magpie 等外部配置，但原生 CLI 可能维护自己的模型缓存。

扫描遍历支持的七个 provider，统一按显式环境覆盖、已配置 executable、PATH 和已知安装位置找 CLI。显式路径失效不偷偷跳到另一安装。执行和发现使用同一套定位规则，目标模型/能力/executable 来自同一 config 快照。

| 入口 | 模型发现 |
| --- | --- |
| OMP | `models --json --no-extensions`，保留完整 selector |
| Grok / Cursor / Antigravity | 对应 CLI 的 `models` 接口 |
| OpenCode | 对应模型目录接口及可选缓存刷新 |
| Codex CLI | PATH/config 指定 binary 的 `model/list` |
| Codex App | 注册 App bundled binary 的 `model/list` |
| Claude | 无已接入的完整列举接口，明确报告 unsupported，保留手工映射 |

未安装项只在扫描报告中出现。已配置路径失效、模型发现失败、不支持列举和成功空列表是不同状态。成功/确认未安装时清理该入口的扫描 target；失败/不支持时保留旧条目并给 warning。manual targets、aliases 和 defaults 保留。能力仅采用上游声明与 dispatch 档位集合的交集，未证实的 fast 不自动写 true。

App 发现不启动 GUI、不创建线程、不切预设。`gui_selection: not_checked` 明确说明目录尚未证明 GUI 可选。扫描只说明入口与目录事实，不保证认证、额度、地区可用性或模型生成成功。

## Codex Desktop 交付

显式 `AI_DISPATCH_CODEX_APP_PID` 可选择目标。默认先排除带 `--user-data-dir` 的自定义 profile 实例，再按祖先、唯一 active 或唯一 GUI 窗口选择；仍有歧义就拒绝。只保留 flag 分类，不记录或输出其路径值。没有运行实例时可启动注册安装并绑定 PID。每个 URL 通过 PID-addressed GURL AppleEvent 投递，不用 `open -a bundle URL` 猜同 bundle 的目标进程；发送前核验 PID/bundle，之后仍以真实 turn 终态证明执行。设置桥直接用 `AXUIElementCreateApplication(pid)` 创建原生 AX 句柄，窗口和子节点全程保留真实 owner PID，不把 System Events 的窗口/entireContents 结果转成按名字引用；模型和速度直接 AXPress 菜单项；自定义强度控件没有可写 AXValue 或增减动作，使用 CGEventPostToPid 定向发送单个箭头，每步回读实际模型/level，并以菜单报告档数为上限。不使用全局键盘、CtrlShiftM 或固定16项循环。关闭菜单只使用定向该 PID 的 Escape，并核对 AXExpanded=false、恢复可读模型标签且无可见菜单项；不以同名 group 是否仍存在判断开关。所有定向键发送前还要用 CFEqual、标题和 owner PID 确认当前 FocusedWindow 就是已验证线程，不能把键送到同 PID 的其它会话。缺窗口、控件或 owner 不匹配时给出明确错误，不访问空对象。

新线程使用客户端 bundled CLI 的短生命周期 stdio `app-server`：`thread/start` 创建元数据，`thread/name/set` 持久化空线程，进程退出释放所有权。空线程在首次入队前失败仍可 resume，不需要伪造历史 rollout。临时后端不调用 `turn/start` 或 `thread/resume`。

准备阶段持有 App 实例跨进程锁，GUI 选择并验证请求 model/effort/speed；不可用组合在入队前报错并尝试恢复原预设。随后另起冷后端，只执行 `thread/read`、`thread/loaded/list`、`thread/queue/add`，入队前后验证未加载任何线程。后端退出后通过 wake 路由由 GUI 执行，普通线程路由导航。

实例准备锁在等待模型前释放，其他线程可以准备；本 session 持独立锁防重复提交。首行 `[ai-dispatch]` 是来源标记，App 另带唯一 `dispatch-id`，对应 queue 的客户端消息 ID。即使正文相同，也只能由本次带唯一标记的用户消息及其 turn ID 收口。未确认投递保留 UUID-only pending 状态，取消/超时后不能盲目再次入队。

收口必须有同一 turn 的成功终态和匹配 `turn_context` 中 model、effort、cwd、速度；失败终态不能被重放 completed 覆盖。元数据、open/queue 成功、部分文本、旧回合尾部均不是成功。`app_turn_active`、`app_session_busy`、`app_delivery_pending` 要求检查原线程。沿用当前 `CODEX_HOME`，不新建 profile，不退到独立 daemon 或 CLI 模型回合。

rollout continuation 沿选定段的 `history_base` 祖先恢复状态，不拼 sibling 分支；断链/成环/歧义返回错误。本次回执保留物理段快照边界，不把祖先消息当新投递。协议依据保留于[上游队列处理](https://github.com/openai/codex/blob/rust-v0.167.0-alpha.3.2/codex-rs/app-server/src/request_processors/thread_queue_processor.rs)和[命名前持久化](https://github.com/openai/codex/blob/rust-v0.167.0-alpha.3.2/codex-rs/thread-store/src/local/update_thread_metadata.rs)。

辅助功能授权、可用 GUI 预设和目标线程可见性需真实验收。目录读取或单元测试通过不能代替 App 实际交付。模型回合成功也不代表业务产物通过验收。

## Reasoning effort 与 Fast

```bash
ai-dispatch send sol "implement the fix" --effort high --json-result
ai-dispatch send codex "review the diff" --model sol --effort auto --json-result
```

合法 effort：`auto|none|minimal|low|medium|high|xhigh|max`。省略/auto 保留底层默认；精确档位以本次 target 的 `efforts` 为准，缺失/不支持时回 auto 并报告 `effort_fallback_reason`，不降相邻档、不重跑。

`fast: true` 只允许用户显式 `--fast`，不传永远标准速度。缺失/不支持时以标准速度执行同一路径，返回 `fast_fallback_reason`。能力回退不设置路由降级，也不触发额外生成。

查询本次目标及已知能力：

```bash
ai-dispatch models resolve sol --format json
ai-dispatch models resolve codex --model sol --surface app --format json
```

解析包含 provider、surface、native model、executable、`model_listed` 及能力。未收录原生 ID 的 `model_listed=false`；不能据此声称它一定可运行或一定不支持。

## 结果字段

以 `ok`、`status`、`text`、`provider_used`、`model_used`、`surface`、`session_id`、`failure_class`、`warnings` 和实际产物判断执行情况。另有 `requested_effort` / `applied_effort` / `effort_fallback_reason`、`requested_fast` / `applied_fast` / `fast_fallback_reason` 说明能力是否实际应用。

兼容历史读取的 `route_trace`、`route_steps`、`degraded` 字段不意味着新请求会遍历候选。新 runtime 每次只执行一个目标，失败保留原原因。取消时保留 session、已捕获输出及 App 未停止模型的警告。

`user_facing_summary` 默认关闭。config 设置 true 或单次 `--wrap-up` 开启，返回 `agent_hint` 时调用 Agent 原样贴 `user_facing_summary`，同 session 只保留最新一张。Duration 使用 `duration_text`，程序计时用 `duration_ms`；不要重算或猜测模型身份。`--stream-progress` 下 stderr 保持 NDJSON，终态事件附摘要。

## Provider 参数

Grok Build：

```bash
ai-dispatch send grok "Reply exactly: OK" --model grok-4.7 \
  --provider-opt grok.max-turns=1 --provider-opt grok.web-search=off --json-result
```

支持 `grok.max-turns`、`grok.web-search=on|off`、`grok.subagents=on|off`、`grok.approval=always|default`。默认不派生子代理，只有用户明确要求才开启。默认 approval=always 用于非交互执行；需要保留工具批准边界时改 default。推理使用顶层 `--effort`，不用旧 `grok.effort`。

OMP：

```bash
ai-dispatch send omp "Reply exactly: OK" --model magpie/codex/gpt-6.1-sol \
  --provider-opt omp.session=none --json-result
```

`omp.approval=always|default`，默认 always 追加 `--auto-approve`；`omp.session=persist|none`，none 追加 `--no-session`，不能同时 resume。`--effort` 映射 `--thinking`，none→off。子进程保留 `OMP_` / `PI_` 环境前缀；完整 selector 由 OMP 目录确认，Magpie 并非必选后端。

Cursor 使用 `cursor-agent --print --output-format stream-json`；模型 ID 自带推理后缀时保留原 ID，不再叠精确 `--effort`。`cursor.approval=default|always`，默认只信任工作区，always 追加全自动批准。

## Run history

```bash
ai-dispatch runs list --limit 20
ai-dispatch runs show <run-id>
ai-dispatch runs failures --since 24h
```

`runs list` 只输出安全摘要。完整结果看 `runs show`。

## failure_class

| failure_class | 含义 |
| --- | --- |
| `config` | provider 二进制、认证、账号、地区或模型访问权问题 |
| `quota` | 额度不足 |
| `timeout` | 墙钟或活动超时 |
| `network` | 网络问题 |
| `runtime` | provider 进程或输出协议异常 |
| `input` | 调用参数或 prompt 输入不合法 |

## Doctor

`doctor` 是排查命令，不是普通安装后的必跑步骤：

```bash
ai-dispatch doctor --format json
```

它只输出健康摘要，不回显本机路径、完整模型路由或环境变量具体值。需要定位文件时用：

```bash
ai-dispatch config path
ai-dispatch preferences path
```

## 开发验证

单元测试默认关闭真实 provider 执行：

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go vet ./...
scripts/go_active_caller_check.sh
```

真实 smoke：

```bash
scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_CLAUDE=on scripts/go_provider_smoke.sh
AI_DISPATCH_SMOKE_AGY=on scripts/go_agy_stress.sh
AI_DISPATCH_ACCEPTANCE_TARGET=grok \
  AI_DISPATCH_ACCEPTANCE_PROVIDER=grok \
  AI_DISPATCH_ACCEPTANCE_MODEL=grok-4.7 \
  scripts/go_provider_acceptance.sh
scripts/go_grok_stress.sh
```

新增或回归 provider 的完整验收合同见 [Provider Acceptance](provider-acceptance.md)。

## 发版

先更新 `skills/ai-dispatch/VERSION`，再本地构建验证：

```bash
scripts/release.sh vX.Y.Z
```

产物在 `dist/`：

```text
ai-dispatch-<os>-<arch>/
  SKILL.md
  VERSION
  LICENSE
  references/
  agents/
  scripts/ai-dispatch
  scripts/ai-dispatch-go
```

同时生成 `SHA256SUMS`。GitHub Release 上传四个平台的 skill tarball 和 npm 用的 standalone `.bin`：darwin/linux × amd64/arm64。

正式发布由 tag 触发 GitHub Actions：

```bash
git tag vX.Y.Z
git push origin main
git push origin vX.Y.Z
```

`scripts/release.sh` 会拒绝 npm 包版本与 `skills/ai-dispatch/VERSION` 不一致的构建，并生成 `dist/ai-dispatch.rb`。

GitHub Release 完成后，同一条 Release workflow 会用 npm trusted publishing 自动发布同版本 npm 包，不再需要本机 OTP。确认：

```bash
gh release view vX.Y.Z
npm view ai-dispatch version --registry https://registry.npmjs.org
```

Homebrew tap 仍需人工更新：把 `dist/ai-dispatch.rb` 复制到 `rennzhang/homebrew-tap` 的 `Formula/ai-dispatch.rb` 后提交推送。

release 构建默认拒绝任何 tracked、staged 或 untracked 改动，避免生成带正式版本名的 dirty 包。只有本地验收时可以显式使用 `AI_DISPATCH_ALLOW_DIRTY_LOCAL=1 scripts/release.sh`；这类产物会保留 dirty identity，不能发布。

公开用户可见变更记录维护在 [Changelog](../CHANGELOG.md)。发版前先补对应版本条目，再更新 `skills/ai-dispatch/VERSION`。

## Provider 扩展

安装后的 skill 只负责调用已有 provider；新增 CLI provider 需要 fork 源码、实现 adapter、补测试并重新发布。接入说明见 [Provider Onboarding](provider-onboarding.md)。
