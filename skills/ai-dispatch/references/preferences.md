# 用户偏好

`~/.ai-dispatch/preferences.md` 是用户自己的场景偏好，供 Agent 在真实调用前阅读。它不承载可执行路由，不覆盖用户明确指定的入口/模型/渠道，也不是另一份默认配置。

- 默认入口与模型来自 `config.json defaults`：OMP / CLI / Sol。
- 全部已知模型和对应关系来自 `config.json models`。
- 偏好只描述某类任务倾向选哪些模型、如何取舍质量和耗时。

## 调用前

```bash
ai-dispatch preferences path
ai-dispatch config show
```

偏好不存在时可以用 `preferences show` 创建空骨架。不要从场景列表反推模型全集，或因为偏好写了某模型就认为本机已安装对应入口。

用户明确指定 Codex App、Grok Build 或具体模型时，优先按明确要求执行。未指定时使用 config defaults；若 Agent 根据已授权的场景偏好选择其他模型，应在调用参数中明确表达，不能在 CLI 层偷偷改路由。

不确定时先解析：

```bash
ai-dispatch models resolve sol --format json
ai-dispatch models resolve codex --model sol --surface app --format json
```

配置没有收录的原生模型仍可通过显式 `provider --model` 透传。逻辑别名无法解析或没有指定入口的 target 时，先澄清/维护映射，不猜模型 ID、不自动 fallback。

## 更新规则

- 只在用户授权后修改实际偏好文件；不要因扫描顺手重写。
- 用简短的人话记录反复使用的偏好，不写可执行 schema 或候选重试链。
- 模型适用场景与档位建议放在一起；档位建议不是默认 effort。
- 只在实际选中 target 的 `efforts` 声明内挑精确档位；无声明时保持 auto。
- Fast 必须用户明确请求，且选中 target 允许；偏好不能让它隐式开启。
- 失效偏好直接修正或删除，不堆兼容例外。
- 维护规则保留在本参考中，不复制进用户正文。

## 示例

```md
# ai-dispatch 偏好

入口、默认选择和模型对应关系见 ~/.ai-dispatch/config.json。
用户明确指定入口、渠道或模型时，以明确要求为准。

## 模型倾向

在这里记录稳定的模型倾向与档位建议。

## 场景选择

### Review

在这里写常用模型及选择原因，不写失败自动换模型的规则。

### 代码实现

在这里写对质量、速度和工作方式的稳定偏好。
```

实际初始化骨架可能包含更多场景标题；它只是空白提示，不内置具体用户偏好。执行结果仍以返回 JSON 和任务产物为准。
