# Go 消息目录

使用 [go-i18n v2](https://github.com/nicksnyder/go-i18n) 读取 `locales/en.json` 和
`locales/zh-CN.json`. 两份 JSON 由 `go:embed` 编入二进制, 运行时不依赖外部翻译文件.

业务代码调用 `config.Text("message.id", args...)`; 包内的 `t` 和错误构造函数仅做转发.
语言选择仍由 `config.ResolveLanguage` 管理: CLI > 配置 > 环境变量, 默认中文.
公开配置值和 `--lang` 继续使用 `cn` / `en`; `cn` 在目录层映射为标准语言标签 `zh-CN`.
显式指定语言的场景调用 `i18n.Text(lang, id, args...)`, 不修改全局语言状态.

## 修改与新增

1. 在两份 JSON 中添加相同的语义 ID. ID 按业务命名, 已有共用消息可以直接复用.
   修改文案时保持 ID 不变, 不需要修改 Go 调用点.
2. 插值使用 `{{.V0}}`, `{{.V1}}` 等, 按调用参数顺序编号. 两种语言可以调整顺序,
   但参数集合必须一致. 需要数字或引号格式时使用 `{{printf "%d" .V0}}` /
   `{{printf "%q" .V0}}`. 不要把变量先拼入翻译文本.
3. 用 `config.Text("message.id", value)` 获取文本. 变量中的花括号、百分号和 HTML
   字符保持原样, 不会再次解释为模板或转义为 HTML.
4. 运行 `go test ./...`. 目录测试检查双语键、模板、参数集合以及所有平台源码中的
   静态调用参数; 不断言 TUI 配色或排版.

例如, `launch.prompt.resume_head` 的英文为 `Resume Kanban task {{.V0}}.`,
调用 `config.Text("launch.prompt.resume_head", taskID)` 即可.

空 ID 返回空字符串; 缺失 ID 或模板执行错误返回 ID, 避免错误展示路径 panic.
内嵌目录解析失败属于构建产物缺陷, 初始化时显式失败.

## 边界

- Go 中的中英界面文案、帮助和可展示错误集中在此目录; 测试保留独立的预期文案.
- Agent 启动/继续/接管提示词也在目录中. 会话反查同时识别两种语言, 兼容原有中文会话.
  修改 `launch.prompt.*_head` 时必须考虑已保存会话的识别兼容性.
- `process` 的任务文件短指令和清理附言保持原有固定语言, 避免改变共用任务文件协议;
  文本同样存入目录. doctor 配置修复结果按磁盘配置语言输出, 保持原有行为.
- 卡片字段、Markdown 卡片模板和解析正则属于持久化格式, 不随界面语言翻译.
  审核协议、命令标识、外部工具诊断和代码注释也不属于界面翻译.
