# Fill in missing option field tips and add a space before each tip

- 类型: Chore
- SIZE: small
- 任务组:
- 创建时间: 2026-09-05 00:51
- 负责人: cursor
- 会话: cursor 831b764c-9345-412b-adbc-afba99ebd1e9
- 窗口: herdr:wG:t29:wG:p2J
- 开始时间: 2026-09-05 00:47
- 完成时间: 2026-09-05 01:03
- 任务分支: options-field-tips
- 结果: completed

## 任务目标

选项面板里, inline Confirm 的 Title 与 Description 被 Huh 直接拼在同一行, MemSearch 目前显示成 `MemSearch启用会运行上游安装命令`. 要在 tip 前加一个空格, 并给所有能挂 Description 的设置项补上 tip.

## 用户决策

用户确认计划并要求直接建卡启动:

- 加 `tip()` 辅助函数, 文案前加一个空格; inline Confirm / Input 的 Description 都走它, 避免标题和 tip 黏在一起.
- Select 的 Description 在 Huh 里永远另起一行, 不能做成 MemSearch 那种同行浅色字; 已有的「同屏栏目数」「大/小任务 Agent」保持这个形态, 缺的 Select 同样用 Description 补 tip. Select 的 tip 不要再加前导空格 (另起一行, 前导空格只会变成缩进).
- 模型输入框不加 Description, 已有 placeholder「留空表示用 CLI 默认」.
- 根菜单和关闭确认的 Description 是整页说明, 不是字段 tip, 不改.
- 不改行式 welcome / `internal/menu`.

拟加/保留的 tip 文案:

- 配色主题: 跟随终端, 或固定浅色/深色
- 同屏栏目数: 终端放不下时会自动少显示几栏 (已有)
- 自动刷新: 看板自动重载间隔
- 只显示单栏: 忽略栏目数, 只看当前栏
- 显示存档栏目: 把 archived 栏加入看板
- 大/小任务 Agent: 已有
- 启动方式: 领取任务时如何拉起 Agent
- Reviewer: 该角色用哪个 Agent 审核
- 环节: auto 按规则决定, skip 跳过, required 必跑
- MemSearch: 启用会运行上游安装命令 (已有, 改走 `tip()`)
- 默认语言: 界面与规则文案

## 预期成果

MemSearch 显示为 `MemSearch 启用会运行上游安装命令` (标题和 tip 之间有空格). 上表列出的设置项都有浅色 tip; Confirm 的 tip 跟在标题同行, Select 的 tip 在标题下一行.

## 验收条件

- [x] MemSearch 的 Description 经 `tip()` 带前导空格, 渲染上标题与 tip 不相黏.
- [x] 「只显示单栏」「显示存档栏目」有同行 tip.
- [x] 配色主题、自动刷新、启动方式、Reviewer、环节、默认语言有 Description tip; 已有的同屏栏目数和大/小任务 Agent tip 保留.
- [x] 模型输入框、根菜单、关闭确认不新增字段 tip.
- [x] `go test ./internal/tui` 通过; 不写视觉呈现断言.

## 威胁模型

N/A

## 不在本轮范围

- 既有问题: 排除行式 welcome / `internal/menu` 的说明文案. 排除 Huh Select 无法把 Description 放到标题同行的库行为, 不 fork Huh.
- 并发/跨平台/安全加固: 排除 Windows 专项交互录制; 文案与布局逻辑与平台无关. 不改配置、看板或文件安全边界.
- 共享契约与文档: 排除发布规则与 `AGENTS.md` 改写; 不新增对外命令或配置键.
- 相邻功能: 排除选项光标记忆、字段分组、配色、模型档位绑定; 只改 Description / tip 文案与 `tip()` 前导空格.

## 讨论与决策

```text
前置任务: N/A
```

- 光标卡 `20260905-options-root-cursor-task` 已完成并合入 `develop` (`a3a9f75`), 本卡基于最新 `develop`.
- 改动落在 `internal/tui/options_form.go` (及如有必要的 `options_test.go`).
- TUI 界面呈现不写测试: tip 的空格和文案属于视觉细节.

## 实施与验证

- 任务分支 `options-field-tips`, 基于本地 `develop` `a3a9f75` (无 origin, 未同步远端).
- `internal/tui/options_form.go` 增加 `tip()`, Confirm 的 Description 走它; 缺 tip 的 Select 补 Description, 不加前导空格; 模型 Input / 根菜单 / 关闭确认未改.
- 验证: `gofmt`; `go test ./internal/tui` 通过; `go vet ./...` 与 `go test ./...` 通过.
- 提交: `ef2cfb94b8f94ee967f4a1a0f73062fb0712caca` 补齐 tip; `8fd4ef32d707488d42b19e7f4077bb9d3f477c57` 按 QA 修正环节与语言文案, 与 REVIEW-RULES / `SetLanguage` 契约对齐.
- 审核: PM PASS (`ef2cfb9`); QA 首轮 medium QA-001/QA-002 成立并已修, 增量复审闭环 (gate findings none). CSA/Hacker 本仓库 N/A.
- 集成: 本地 `develop` ff 到 `8fd4ef32d707488d42b19e7f4077bb9d3f477c57`; 无 origin, 未 push. 主树用户改动的 `rules/*.md` 已 stash 后恢复, 未进本卡提交.

## 完成总结

选项面板各设置项已有 tip; Confirm 标题与 tip 之间有空格. 环节 tip 写明「审核触发时必跑」, 语言 tip 写明「界面与命令输出」. 最终 commit `8fd4ef32d707488d42b19e7f4077bb9d3f477c57` 已在本地 `develop`. 验收条件全部满足.

