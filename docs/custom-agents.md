# 自定义执行 Agent

`config.json` 的可选 `agents` 对象以 agent 名为键。内置名称为 `codex`、`claude`、`grok`、`cursor`、`kimi`。新增名称以小写字母开头，最多 64 字符，仅含小写字母、数字、`_`、`-`。不配置本节时，原有参数与配置输出保持不变。

## 可执行名与进程名

```json
{
  "agents": {
    "claude": {"process_name": "node"},
    "codex": {"path": "kander-codex"}
  }
}
```

`path` 为 PATH 名或存在且可执行的绝对路径，含分隔符的相对路径不接受。显式指定时必须可以解析；缺省依次回退内置表、agent 名。`process_name` 用于 tmux 前台进程匹配，缺省为最终 path 的 basename。两者显式给出时须非空且无控制字符；恢复默认值请删字段。npm trampoline 可用 `process_name: "node"`，改名包装器可只设 `path`。

启动、恢复、doctor、面板探测、tmux 存活检查、通知反查与接管清理均读取这组设置。保存和读取都会校验显式路径；删除包装器后，须修复配置或恢复可执行文件。doctor 遇到无法验证的 agent 定义时保留原文件并报告，避免丢失模板。

## 方言或 argv 模板

```json
{
  "agents": {
    "my-claude": {"path": "my-claude", "dialect": "claude"},
    "helper": {
      "path": "helper",
      "process_name": "helper",
      "args": {
        "start": ["--model", "{model}", "--effort", "{effort}", "--session-id", "{session}"],
        "resume": ["--resume", "{session}", "--model", "{model}"]
      },
      "session": {"mode": "generated"}
    }
  },
  "kanban_agents": {"large": "my-claude", "small": "helper"}
}
```

自定义 agent 必须有 `dialect` 或 `args`。内置 agent 自动继承自身方言。`dialect` 接受五个内置名称，复用相同 model、effort、越权与会话参数。模型仍位于 `models.kanban.<agent>`，使用 `large_model`、`small_model`、相应 effort 字段；兼容旧共享 `model` 回退。自定义 Cursor 或 Kimi 方言沿用它们无 effort 的模型字段。Kimi 的推理档位由用户自己的 `~/.kimi-code/config.toml` 的 `[thinking] effort` 决定，kander 不介入。

同时声明时 `args` 优先，完整替换方言参数。必须有 `args.start`；支持恢复的模板还必须有 `args.resume`；允许空数组。每个元素独立替换 `{model}`、`{effort}`、`{session}`，替换值不递归解析。任一占位符为空时，丢弃该元素；其紧邻前一个原始元素若为独立 flag（以 `-` 开头、不含占位符或 `=`），同时丢弃该 flag。其他位置参数保留。未知占位符、控制字符与空元素被拒绝。

prompt 不进入模板，始终由 Kander 追加在 argv 最后。分配命令和参数模板均不用 shell 插值；终端启动仍经已有平台参数编码。配置可以执行本机用户声明的程序，不构成新的用户间权限边界。

## 会话策略

- `generated`：生成 UUID，保存到卡片 SESSION，供 `{session}` 与恢复使用。
- `allocated`：先执行 `session.allocate` argv（首元素是程序），最多等待 10 秒；成功输出须为一个 ID，或通过 `session.json_field` 指定顶层 JSON 字符串字段。ID 仅接受 1–128 个字母、数字、`.`、`_`、`:`、`-`。程序失败、无效输出与超时均在领取任务前报告。
- `none`：`resume` 明确拒绝；`notify` 不直投，通过恢复通道运行 start 模板，重新读取卡片上下文。卡片保留仅用于终端标记的 UUID，模板中的 `{session}` 为空，方言参数也省略会话创建/恢复选项，UUID 只供终端身份检查。`dismiss` 仍允许关闭已确认身份的终端。`kander config`、`config --json` 的 stderr、`kander check` 显示降级提示。持久派回仍须满足原有停止事实与回执门禁，不以 `none` 绕过防重复执行检查。

模板自定义 agent 若无方言，必须显式声明 session。有方言时默认继承：Claude/Grok 生成 UUID；Cursor 调用配置后的程序执行 `create-chat`；Codex 保留扫描 CODEX_HOME rollout 的既有发现机制（自定义 Codex 方言同样适用）；Kimi 同为发现式，扫描 `KIMI_CODE_HOME` 下的会话记录，因为 kimi-code 不接受调用方指定的会话 ID。`discovered` 不接受手工配置。无模板的 Codex 与 Kimi 方言不接受 `generated`/`allocated` 覆盖，Cursor 不接受 `generated`，因为对应 start 参数无法兑现这种身份来源；请继承默认值或提供模板。所有方言都允许 `none`，启动时不传会话参数。

分配示例：

```json
"session": {
  "mode": "allocated",
  "allocate": ["helper", "create-session", "--json"],
  "json_field": "id"
}
```

全新模板 agent 推荐通过 tmux / tmux-session 启动，以配置后的前台名与会话标记探测。herdr 仍依赖其自身对 agent 类型的识别；配置 path 不会为 herdr 安装识别器。foreground/console 没有可供 `check` 探测的终端地址，沿用 unknown 分类。

## 面板与审核边界

「任务执行与模型」可以选择自定义 agent。尚未探测成功的内置 agent 也可选择以填写改名程序路径；保存时仍校验显式路径。每个已选 agent 的模型字段后有「可执行名」「pane 进程名」输入；大小任务共用同一 agent 时只显示一次。留空删除覆盖，保存到 `agents`。方言、模板与会话策略只在 JSON 编辑。

reviewer 名单仍只有五个内置 agent，固定使用其只读适配器。review 可执行名优先级为 `*_REVIEW_BIN` 环境变量、review 内置程序名；`agents.*.path` 和 `process_name` 完全不参与 review 选择。因此执行 agent 的包装器不会被自动用于审核。

兼容方言的自定义名称在 `dismiss` 和接管清理时复用该方言的退出命令，仍要求身份及单 pane 容器检查通过。纯模板且未声明兼容方言的程序没有可推断的交互退出命令；`dismiss` 明确拒绝并保留容器。
