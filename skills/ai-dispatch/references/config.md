# 配置、发现与模型对应关系

可执行配置只有 `~/.ai-dispatch/config.json`。v2 把默认选择、本机入口和模型对应关系分开存为对象；不再使用有顺序的候选数组，不自动遍历 fallback。

## 初始化与发现

首次 `send`/`resume` 只确保最小 config 和偏好文件存在，不在生成请求中偷偷扫描。默认选择为 OMP / CLI / Sol（`gpt-6.1-sol`），但初始化不保证该模型已经可解析。先用 `init` 或明确扫描填充本机目录：

```bash
ai-dispatch init
ai-dispatch providers scan --dry-run --format json
ai-dispatch providers scan --format json
```

`--dry-run` 只预览候选配置和各入口发现结果。普通扫描更新同一个 config。`--refresh` 请求 OpenCode 刷新其模型缓存；不是重试生成请求，也不会自动修改 OMP/Magpie 等外部配置。

扫描范围为内置适配器：`omp`、`codex`、`grok`、`claude`、`cursor`、`opencode`、`antigravity`。先找 executable，再尝试该入口自己的模型列举能力：OMP `models --json`，Grok 等 `models`，Codex 对指定 binary 使用 `model/list`。不具备列举接口的入口明确标记 unsupported；不凭印象捏造模型。

- 未安装的入口只出现在扫描报告，不写一堆 `available: false`。
- 已有配置路径失效、模型接口失败或不支持列举，会报告限制，不伪装成“目录为空”。
- 成功列举后替换该入口/渠道的 `origin: scan` target；未再出现的扫描条目可移除。
- 读取失败/不支持时保留已有模型映射；手工 targets、aliases、defaults 始终保留。
- 写入前持锁重新读取并合并，再校验、原子替换。扫描不消耗模型生成额度，不安装 CLI、不替用户登录。

Codex CLI 和 App 使用各自 binary 的目录。App 只读 bundled 后端 `model/list`，不启动或聚焦 GUI，不建线程、不切模型；报告中的 `gui_selection: not_checked` 表示**目录条目尚未证明是 GUI 可选预设**。列出模型也不证明账号权限、剩余额度或单次请求可用。

## v2 完整示例

路径和模型仅演示结构，实际以本机发现和人工确认结果为准：

```json
{
  "version": 2,
  "defaults": {
    "provider": "omp",
    "surface": "cli",
    "model": "gpt-6.1-sol"
  },
  "claude_transport": "print",
  "user_facing_summary": false,
  "providers": {
    "omp": {
      "executable": "/absolute/path/to/omp",
      "surfaces": ["cli"]
    },
    "codex": {
      "executable": "/absolute/path/to/codex",
      "surfaces": ["cli", "app"]
    },
    "grok": {
      "executable": "/absolute/path/to/grok",
      "surfaces": ["cli"]
    }
  },
  "models": {
    "gpt-6.1-sol": {
      "aliases": ["sol"],
      "targets": {
        "omp": {
          "cli": {
            "model": "magpie/codex/gpt-6.1-sol",
            "origin": "manual",
            "efforts": ["low", "medium", "high", "xhigh", "max"]
          }
        },
        "codex": {
          "cli": {
            "model": "gpt-6.1-sol",
            "origin": "manual",
            "efforts": ["low", "medium", "high", "xhigh", "max"]
          },
          "app": {
            "model": "gpt-6.1-sol",
            "origin": "manual",
            "efforts": ["low", "medium", "high"]
          }
        }
      }
    },
    "grok-4.7": {
      "aliases": ["grok4.7"],
      "targets": {
        "grok": {
          "cli": {
            "model": "grok-4.7",
            "origin": "manual"
          }
        }
      }
    }
  }
}
```

`targets` 是多对多支持关系，不是优先级；同一个模型可有多个入口，每个 provider/surface 最多一个原生 ID。相同逻辑模型在同一个入口有多种来源时，保留不同模型 key，不覆盖已有 selector，也不猜测“同名就是同一个模型”。Magpie 仅在具体 OMP selector 中出现。

| 字段 | 含义 |
| --- | --- |
| `version` | 配置 schema 版本，与 CLI 版本无关 |
| `defaults` | 没明确指定入口/模型时使用的选择 |
| `providers.*.executable` | 已发现的绝对可执行路径；App 执行另定位其 bundled CLI |
| `providers.*.surfaces` | 已发现入口渠道，不保证 GUI 当前预设或模型额度 |
| `models.*.aliases` | 可选逻辑别名，大小写不区分；不得占用 provider 名称 |
| `targets.<provider>.<surface>.model` | 该入口接受的完整原生模型 ID |
| `origin` | `scan` 由扫描维护；`manual` 保留用户设置；手填省略时规范为 manual |
| `efforts` | 此入口/渠道可精确应用的档位，非默认档位 |
| `fast` | 只有 `true` 才允许显式 `--fast` 生效，非默认开启 |
| `claude_transport` | `print`、`pty`、`auto`、`disabled` |
| `user_facing_summary` | 开启可直接贴给用户的结果卡；默认 false |

不保存 provider CLI 版本号或模型数量摘要。需要排障时读取实际 executable / 构建身份。未知字段、歧义别名和错误结构直接拒绝，不静默忽略配置。

## 选择规则

```bash
# 无 target：defaults，prompt 单独提供
ai-dispatch send --prompt-file /tmp/task.md --json-result

# 模型 key/alias：使用 defaults.provider，不自动另找 provider
ai-dispatch send sol "Review the diff" --json-result

# 显式入口与模型/渠道：不受全局默认限制
ai-dispatch send codex "Review the diff" --model sol --surface app --json-result
ai-dispatch send grok "Review the diff" --model grok-4.7 --json-result
```

- `send codex`、`send grok` 等裸 provider 使用该 CLI 自己的模型默认值；不会把全局默认模型强加给显式入口。
- 显式 `provider --model <native-id>` 可以透传目录未收录的原生 ID，解析返回 `model_listed=false`；目录不是白名单。
- 逻辑别名若没有所选 provider/surface target，明确报错；不把别名假装成原生 ID，也不切换入口。
- 只指定模型且默认入口不支持它时，报错提示明确入口，不挑另一个 target。
- 不支持 `provider:model` / `model:provider` 冒号语法。
- 每次只执行选中的一条路径，quota/network/runtime 等失败直接返回，保留原因。

安全解析不生成内容：

```bash
ai-dispatch models resolve --format json
ai-dispatch models resolve sol --format json
ai-dispatch models resolve codex --model sol --surface app --format json
```

## Effort 与 Fast

精确 effort 只在所选 target 的 `efforts` 声明内应用；未声明/不支持时为 auto，结果保留 `requested_effort`、`applied_effort`、`effort_fallback_reason`。合法档位为 `none|minimal|low|medium|high|xhigh|max`，省略或顶层 `auto` 表示不覆盖底层默认。扫描只保留与 dispatch 支持集合的交集，不能盲目同步上游新档位。

`fast: true` 只允许显式 `--fast`，不传始终标准速度。未声明或 false 时返回 `fast_fallback_reason`；不会因此换 provider 或额外调用。原生 ID 透传但没有已知能力时，不捏造 efforts/fast 支持。

## 会话恢复与 App

恢复 dispatch 已记录的 session 时继承原 provider/surface/model；显式冲突的 provider 或 surface 被拒绝，避免 CLI 接管 App 线程。无可用历史时明确指定入口/渠道，不靠当前 defaults 猜测。

App 通过目标 GUI 执行，模型目录和 GUI 可选预设分别验证。入队前核实模型/effort/速度，收口核实本次匹配 turn 的完成终态与 `turn_context`。取消或投递拾取失败后，保留 session 和待确认投递 UUID；先检查原线程，再恢复，不重复入队。

收到取消时，CLI 路径清理拥有的子进程；App 只停止等待及临时后端，不保证 GUI 回合已停止，并明确保留 warning。`exit_code=130` 只表示 dispatch 调用被取消。

## v1 迁移

```bash
ai-dispatch config migrate --dry-run --format json
ai-dispatch config migrate --apply --format json
ai-dispatch providers scan --dry-run --format json
```

默认仅预览。应用前备份原文件，再原子写入 v2；冲突不写。不同入口映射可以保留，但旧候选顺序取消；同一 provider/surface 有多个不同原生 ID 必须先拆成独立模型。占用 provider 名称的模型 key 会改名并给出警告。旧 provider 诊断没有 executable 路径，需扫描补齐。

备份是恢复材料，不是第二个活跃配置。新 runtime 不在 send/resume 中自动解析旧数组；旧 runtime 不应读取已迁移的文件，切换二进制与配置要一起验收。

## 状态目录

```text
~/.ai-dispatch/
  config.json
  preferences.md
  bin/
  runs/
  logs/
  desktop-locks/
```

`preferences.md` 只给 Agent 提供场景偏好，不是第二套路由配置。`desktop-locks` 只用于 App 跨进程协调及 UUID-only 待确认状态，不存提示正文。

`AI_DISPATCH_GO_PROVIDER_EXECUTION=off` 可关闭真实 provider 执行用于测试；disabled 结果不等于真实执行成功。构建身份使用 `ai-dispatch version --format json` 检查。
