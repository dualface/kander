# Changes in the options panel model input box cannot be saved

- 类型: Bug
- SIZE: small
- 任务组:
- 创建时间: 2026-09-06 20:51
- 负责人: grok
- 会话: grok e1196897-4d4a-47cd-ad01-10560f9c76a7
- 窗口: herdr:wX:tC:wX:pW
- 开始时间: 2026-09-06 20:52
- 完成时间: 2026-09-06 21:07
- 任务分支: options-model-input
- 结果: completed

## 任务目标

选项面板里「任务执行与模型」和「审核与模型」两个分区的模型 / 推理档位输入框, 部分字段改了值也保存不了: 界面上能看到输入的字符, 但改动既不写回配置会话, 也不落盘, 重开面板还是旧值.

根因在 `internal/tui/options_form.go` 的 `modelInputs`: 它先把新值 append 进 `bind.modelValues` 切片, 再把 `&bind.modelValues[index]` 交给 huh 输入框. 之后每追加一个模型字段都可能触发切片扩容 (cap 1→2→4→8) 并更换底层数组, 先建字段的绑定指针就停留在旧数组上. 用户敲的字写进了没人再读的旧数组, `applyModels` 读的是新数组, 比对后认为「没变化」, 于是不写回会话、不置 dirty, 保存时自然也没有这份改动.

修复目标是让每个模型输入框的绑定地址在整个表单生命周期内稳定, 使全部模型与推理档位字段的改动都能被 `applyModels` 读到.

## 用户决策

- 用户确认本修复走看板执行.
- 修复方案已确认: 把 `formBinding.modelValues` 从 `[]string` 改为 `[]*string`, 每个字段用独立分配的字符串变量, 其地址同时交给 huh 与 `modelValues`; `applyModels` 相应改为解引用比较与写回.
- 用户确认不改动面板的字段顺序与布局.
- 回归测试必须走真实按键路径 (键盘事件进入 huh 表单); 直接改写 `bind.modelValues` 会绕开本 bug, 不算有效验证.

## 预期成果

- 执行分区的大任务、小任务, 以及审核分区 PM / CSA / Hacker / QA 四个角色, 其模型与推理档位输入框中任意一个被修改后, 都能写回配置会话并随保存写入 `config.json`.
- 大任务与小任务选同一个 Agent 时同样成立.
- 面板的字段顺序、缩进和空行规则与修复前一致.

## 验收条件

- [x] `internal/tui/options_form.go` 的模型输入框不再绑定切片元素地址, 表单后续追加字段不会使已建字段的绑定失效
- [x] 新增回归测试走真实按键路径 (构造键盘事件送入面板), 在执行分区修改大任务模型后, `session.Config.Models.Kanban[<agent>]["large_model"]` 为新值, 且提交保存后重新 `config.Load` 读到的也是新值
- [x] 该回归测试覆盖大任务与小任务选同一个 Agent 的情形
- [x] 新增回归测试在审核分区修改 PM 角色的模型后, 同样写回会话并落盘
- [x] 新增回归测试在未修复的代码上失败, 修复后通过 (在实施记录中写明实际验证过这一点)
- [x] `go test ./...` 与 `go vet ./...` 通过, `gofmt -l .` 无输出
- [x] 选项面板的字段顺序、缩进与分组空行规则与修复前一致

## 威胁模型

N/A

## 不在本轮范围

- 既有问题: 不改选项面板其他分区 (界面偏好、规则模块) 的绑定方式. 它们绑定的是结构体字段或 `map[string]*T`, 地址本就稳定, 不受本 bug 影响, 改动只会引入无谓风险.
- 加固: 不为 huh 绑定引入通用的「稳定指针」封装或抽象层. 只有 `modelInputs` 一处有这个需求, 按 KANDER-CODE-RULES「共享抽象须有至少两处稳定需求」不提前抽象.
- 共享契约与文档: 不修改 `KANDER-KANBAN-RULES.md` 中选项面板的行为描述. 规则里写的「大小任务和四个角色各有独立模型与推理档位」本就是期望行为, 本次是实现没做到, 不是契约要改.
- 相邻功能: 不改模型字段的去重规则 (`ModelField.Key()`)、角色缺值时的默认值填充, 以及更换 Reviewer 时重置该角色模型的逻辑. 这些与本 bug 无因果关系, 改动会牵动已确认的面板语义.

## 讨论与决策

- 复现: 打开选项面板「任务执行与模型」, 把焦点移到大任务模型输入框并敲入一个字符, `form.View()` 里能看到 `gpt-5.6-solX` (按键确实到位), 但 `bind.modelValues[0]` 仍是原值, 提交保存后 `config.json` 也是原值.
- 根因: `modelInputs` 中 `bind.modelValues = append(...)` 之后取 `&bind.modelValues[index]`; 切片扩容换数组后, 先建字段的指针指向旧数组.
- 实际影响范围大于用户报告的现象: 执行分区 4 个字段 (大任务模型/档位, 小任务模型/档位) 中大任务的 2 个失效; 审核分区 8 个字段中 PM 与 CSA 的 4 个失效. 与「大小任务选同一个 Agent」无关 — 选不同 Agent 也一样, 只是大任务永远排在最前, 所以永远中招.
- 自审: 通过. 逐条对照后确认 — 目标与成果一致地指向「模型/推理档位改动能保存」这一个用户诉求, 用户报告的大任务场景与排查发现的审核分区场景同属一个根因, 一并纳入本轮而非另开卡; 用户决策栏只记录了用户实际确认的三项 (走看板、指针方案、不改布局) 和一项由用户「必须走真实按键路径」要求推出的测试约束, 没有把排查结论写成用户决定; 范围排除的四项都不是达成目标的必需工作, 与本轮改动无因果依赖; 验收条件逐条可执行可判定 (代码结构一条、测试行为四条、工程门禁两条), 覆盖了目标与全部预期成果, 未引入范围外要求. 无需用户新决策的歧义.

## 实施与验证

- 工作目录: `/home/dualf/works/kander/worktrees/options-model-input`
- 任务分支: `options-model-input`, 从 `origin/develop` (`eee4aad`) 创建
- 改动: `formBinding.modelValues` 改为 `[]*string`; `modelInputs` 为每个字段独立分配字符串变量, 同一地址交给 huh 与 `modelValues`; `applyModels` 解引用比较与写回. 未改字段顺序、缩进、空行.
- 回归测试: `TestExecutionModelInputSavesWhenAgentsMatch` (默认大小任务同为 codex, 键盘 `down` 后送 `X`, 断言会话与 `config.Load` 均为 `gpt-5.6-solX`); `TestReviewModelInputSavesPMRole` (两次 `down` 到 PM 模型框, 同样走按键路径).
- 未修复代码上失败: `go test ./internal/tui -run 'TestExecutionModelInputSavesWhenAgentsMatch|TestReviewModelInputSavesPMRole'` 在改 `options_form.go` 之前失败 — `session large_model="gpt-5.6-sol" want "gpt-5.6-solX"`, `session PM model="gpt-5.6-sol" want "gpt-5.6-solX"`. `form.View()` 已含新值, 证明按键到位、会话未写回.
- 修复后上述两测通过. `gofmt -l .` 无输出, `go vet ./...` 通过, `go test ./...` 通过.
- commit: `b44fe3711f1e5ce1b7f87d96bdde574923b1b100` `fix(tui): 稳定选项面板模型输入框绑定指针`, 已 push `origin/options-model-input`.
- 审核: N/A (`rules.review=false`, 四角色均为 skip; 本仓库 CSA/Hacker 亦为 N/A).
- 合入 `develop`: 用户授权后快进推送 `b44fe37` 到 `origin/develop`, 主树 `git merge --ff-only origin/develop` 成功. `git merge-base --is-ancestor b44fe37 origin/develop` 成立. 已删除任务 worktree、本地与远端 `options-model-input`. 审核 N/A, 未重验 (源分支未前进, 无 rebase).

## 完成总结

选项面板「任务执行与模型」「审核与模型」的模型/推理档位输入框改动能写回会话并随提交落盘. 绑定改为每字段独立 `*string`, 切片扩容不再使先建字段失效. 布局未改. 真实按键回归覆盖大小任务同一 Agent 的大任务模型, 以及审核分区 PM 模型; 未修复时失败、修复后通过. 交付 commit `b44fe37` 已快进进入 `origin/develop` 与本地 `develop`. 无偏差, 无未处理问题. 验收条件全部满足.
