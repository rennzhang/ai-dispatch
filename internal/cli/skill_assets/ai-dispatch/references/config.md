# 配置

## 初始化

安装后执行一次：

```bash
~/.ai-dispatch/bin/ai-dispatch init --claude-transport print
```

## 配置文件

路径 `~/.ai-dispatch/config.json`。快速查看：

```bash
~/.ai-dispatch/bin/ai-dispatch config path
~/.ai-dispatch/bin/ai-dispatch config show
```

最小字段：

```json
{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "registry_path": ""
  },
  "hooks": {
    "notify_command": ""
  }
}
```

`claude_transport` 可选值：

- `print`：默认，使用 `claude -p`。
- `pty`：通过 PTY driver 调 Claude，适合订阅或本地交互态。
- `auto`：检测到 Anthropic API 环境变量时走 `print`，否则 `pty`。
- `disabled`：Claude target 直接失败关闭。

## 模型 registry

默认使用 ai-dispatch 内置 registry。需要本地覆盖时设置 `models.registry_path` 或环境变量 `AI_DISPATCH_MODEL_REGISTRY`。

## 真实 provider 执行

`AI_DISPATCH_GO_PROVIDER_EXECUTION=on` 显式打开真实 provider CLI 执行，用于开发、测试和 smoke 场景。通过已安装 skill 调用时不需要手动设置。

## Skill 安装

内置 skill 安装到用户级 skill 根目录，不要复制到每个项目：

```bash
~/.ai-dispatch/bin/ai-dispatch skill install --target all
```
