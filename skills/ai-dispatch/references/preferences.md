# 用户偏好

`~/.ai-dispatch/preferences.md` 是用户自己的场景偏好。Agent 选择 target/model 前先读它，但它不承载可执行路由；短名到 provider/model 的真实映射在 `config.json` 的 `models` 字段里。

## 用途

- 在真实调用前帮助 Agent 选择默认 target/model 短名。
- 记录用户反复表达过的模型倾向和场景选择，例如 review、前端 UI、Bug 查找、写文档、代码实现。
- 保持公共 skill 干净：公开安装包不内置具体用户倾向。

## 使用规则

- 真实调用前先读 `~/.ai-dispatch/preferences.md`。
- 文件不存在时运行 `ai-dispatch preferences show` 创建默认文件。
- 用户明确指定 target/model 时，用户指定优先。
- 偏好只是默认选择，不覆盖返回 JSON 里的真实结果。
- 遇到多个候选短名时，优先按场景下的候选池自由组合；场景内建议组合只是默认建议，不是唯一合法路径。
- 偏好里写的短名必须能被 `ai-dispatch models resolve <target>` 解析；不能解析时，先更新 `config.json models` 或换用内置 target。

## 更新规则

- 用 `ai-dispatch preferences open` 打开文件。
- 用人话写短规则，不写 YAML、schema 或复杂表格。
- 只记录会反复使用的偏好。
- 偏好失效时直接删掉，不叠例外。
- 维护规则写在本子规范里，不写进 `preferences.md` 正文。

## 建议格式

下面是格式示例。`ai-dispatch preferences show` 只生成空骨架，不内置具体用户倾向。

```md
# ai-dispatch 偏好

Agent 在真实调用 ai-dispatch 前必须先读这个文件，再选择 target/model。用户明确指定 target 或 model 时，优先按用户明确指定执行。

偏好的用途、更新方式和维护边界见 ai-dispatch skill 的 `references/preferences.md` 子规范。

## 模型倾向

- model-a：写这个模型长期适合的任务。
- model-b：写这个模型长期适合的任务。

## 场景选择

### Review

**候选池**：`model-a`、`model-b`

可以按任务自由组合。

- 默认 review：用 model-a。
- 交叉 review：用 model-a + model-b。
```
