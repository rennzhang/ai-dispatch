# 模型路由

本文件用于选择 dispatch target 和解释路由结果，不是运行时配置。

请求的 target 只是意图，真实执行结果必须看返回 JSON 的字段：`provider_used`、`model_used`、`requested_target`、`route_trace`、`degraded`、`degrade_reason`。

默认 provider 只有 `codex`、`claude`、`opencode`、`antigravity`。Cursor CLI、Augie、Qoder 等其他本地 CLI 要按 `references/provider-onboarding.md` 新增 provider adapter。

## 默认路由

- `gpt5.5` / `codex`：代码实现、bug 修复、仓库编辑、测试和直接工程执行。
- `mimo`：前端 UI、视觉布局、快速视觉候选和低成本 OpenCode/OpenRouter agent 任务。
- `kimi`：前端探索、长上下文 coding、规划和高上限 UI 候选。
- `grok`：快速工程分析、代码理解、brainstorming 和第二个 coding 视角。
- `sonnet`：稳定指令遵循、代码 review、重构、规划和 Claude CLI 可用时的稳态交付。
- `opus`：架构决策、困难 review、高风险规划和复杂判断。
- `gemini-pro` / `gemini-flash`：仅本地 Antigravity/Gemini 可用时使用。

用户明确指定 target 或 model 时，优先尊重用户选择，除非路由失败关闭。

## 稳定 Target

- `gpt5.5` / `gpt`：OpenAI Codex provider。代码执行和工具密集型仓库任务的默认选择。
- `gpt5.4`：上一代 Codex。只有明确需要时作为备用。
- `sonnet4.6` / `sonnet`：Claude provider。稳定规划、实现、review 和文本推理。
- `opus4.7` / `opus`：Claude provider。高成本判断、架构和困难 review。
- `mimo-v2.5-pro` / `mimo`：OpenCode/OpenRouter provider。UI 和视觉工作。
- `kimi-k2.7-code` / `kimi`：OpenCode/OpenRouter provider。UI 探索、长上下文和 coding。
- `grok-build-0.1` / `grok`：OpenCode/OpenRouter provider。快速工程分析和第二视角。

## 显式指定才使用的 Target

这些 target 可用，但不应被默认路由自动选中：

- `glm`：agent coding、读仓库、review、对标 Sonnet。
- `qwen`：长上下文、大仓库、结构化调研和汇总。
- `minimax`：低成本、高吞吐、轻量 agent 任务。
- `deepseek`：通用生成、成本敏感推理和中等复杂度任务。

## 路由检查

不要假设 target 一定存在，先查：

```bash
~/.ai-dispatch/bin/ai-dispatch models
~/.ai-dispatch/bin/ai-dispatch models resolve <target> --format json
```

短 alias 可提升可读性，但必须信任 JSON 里的真实 provider/model。
