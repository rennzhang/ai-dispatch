package config

const DefaultPreferencesMarkdown = `# ai-dispatch 偏好

Agent 在真实调用 ai-dispatch 前必须先读这个文件，再选择 target/model 短名。用户明确指定 target 或 model 时，优先按用户明确指定执行。

偏好的用途、更新方式和维护边界见 ai-dispatch skill 的 ` + "`references/preferences.md`" + ` 子规范。
短名到 provider/model 的真实路由见 ` + "`~/.ai-dispatch/config.json`" + ` 的 ` + "`models`" + ` 字段。

## 模型倾向

把你反复使用、值得记住的模型倾向写在这里。

## 场景选择

### Review

**候选池**：

在这里写 review 场景的常用模型组合。

### 前端 UI

**候选池**：

在这里写前端 UI 场景的常用模型组合。

### Bug 查找

**候选池**：

在这里写 Bug 查找场景的常用模型组合。

### 写文档

**候选池**：

在这里写文档场景的常用模型组合。

### 代码实现

**候选池**：

在这里写代码实现场景的常用模型组合。
`
