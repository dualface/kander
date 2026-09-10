# Funnel terminal operations through an internal/terminal Backend interface for herdr and tmux

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-terminal-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:17
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

把散在 8 个包里对 herdr 与 tmux 的直接调用收口到一个新包 `internal/terminal`, 以 `Backend` 接口按 launcher 名分发. 这是把终端后端改为声明式定义文件的地基: 只有当所有调用方都经由同一组抽象操作访问终端时, 一个由 JSON 描述的新终端才可能被 launch, liveness, notify, takeover, focus, menu, probe 同时认出.

当前统计 (develop 1e1d3a6, 排除测试): launcher 名字符串分支约 145 处, 分布在 launch 41, menu 49, notify 17, liveness 15, takeover 10, focus 8, config 3, tui 3. herdr 与 tmux 的命令构造分别在 `internal/launch/herdr.go`, `internal/launch/tmux.go`, `internal/probe/herdr.go`, `internal/probe/tmux.go`, `internal/takeover/ops.go`, `internal/notify` 与 `internal/focus` 中.

本卡是纯搬运: 行为不变, 不引入定义文件, 不改用户可见输出.

## USER_DECISIONS

- 终端定义文件是主要扩展方式; Backend 接口作为其底层承载, 不作为独立的重构目标, 其完成标志是后续卡能把 tmux 与 herdr 各自变成一份嵌入 JSON 加少数钩子.
- 收口 (本卡) 与定义格式 (`20260908-terminal-definition-format-task`) 分为两张卡, 不合并, 以控制审核范围.
- 本轮只建卡, 不启动.

## EXPECTED_OUTCOME

- 新包 `internal/terminal` 定义 `Backend` 接口与按 launcher 名取后端的注册表. 接口的操作集合由执行者从现有调用点归纳并在 `plan.md` 列出; 每个操作给出类型化的输入与输出 (不只是名字), `plan.md` 同时给出 "原调用点 -> 操作" 的映射表, 以及一份纸面验证: tmux 与 herdr 的每个操作都能表达为 "若干条 argv + 输出解析 + 错误分类" 的步骤序列, 需要轮询、回退或多步组合的操作 (等待标记, 元数据缺失回退, 多步聚焦) 明确标出, 供 `20260908-terminal-definition-format-task` 一比一映射. 操作至少覆盖: 创建容器并返回容器与 pane 地址, 等待 pane 就绪, 在 pane 中启动命令, 向 pane 投递字面文本与 Enter, 等待输出中出现标记, 写入与读取 pane 元数据 (会话标记, 项目标记), 读取 pane 事实 (存活, 前台进程, copy-mode 等既有字段), 读取 pane 所属容器与容器内 pane 数, 切换焦点, 关闭容器, 以及判定 "容器已消失" 与 "元数据缺失" 两类错误. 接口另含: 地址契约 (`WINDOW` 的 `<launcher>:<后端不透明地址>` 由后端负责编码与解析, 调用方不再拆分冒号字段) 与能力标志 (是否有容器, 是否支持聚焦, 是否支持元数据, 是否能报告前台进程), 调用方按能力标志而不是按 launcher 名做降级.
- `internal/terminal/tmux` 与 `internal/terminal/herdr` 两个子包各自实现 `Backend`, 吸收原来分散在 launch, probe, takeover, notify, focus 中的 herdr/tmux 专用代码. `tmux` 与 `tmux-session` 共用 tmux 后端, 差异以后端选项表达. `foreground` 与 `console` 归为无容器后端: 容器操作返回明确的 "不支持" 错误, 调用方现有的降级逻辑不变.
- `internal/probe` 保留通用的进程执行, 超时与结果分类; herdr/tmux 专用探测函数移入对应后端子包. `internal/focus/focus.go` 经 `HERDR_SOCKET_PATH` 做 pane 级聚焦的代码归入 herdr 后端的 `focus` 操作, focus 包只调用接口.
- launcher 名的校验不再固定于 `config.Launchers`: `internal/config` 提供 launcher 名注册口 (例如 `config.RegisterLauncherNames`), 并保留内置六个名字作为未注册时的默认集合, 使 `config` 包单独使用与单包测试时行为不变; `internal/terminal` 在 `init` 中注册内置名, 重复注册幂等, 注册在 `config` 校验首次使用前完成 (由完整二进制的 import 顺序保证并有测试); `config` 不 import `terminal`, 后续定义文件卡用同一注册口加入用户定义的名字.
- launch, liveness, notify, takeover, focus, menu, tui 不再直接引用 herdr/tmux 可执行名或构造其命令行, 改为经 `terminal.Backend` 调用.
- 用户可见行为, 输出文案, 错误码, 卡片 `WINDOW` 格式与现有测试期望全部不变.

## ACCEPTANCE_CRITERIA

- [ ] `internal/terminal` 包存在, 含 `Backend` 接口, 注册表, 以及 `tmux`, `herdr`, 无容器三种实现; `plan.md` 列出每个操作的类型化输入与输出, 原调用点映射表, 地址契约与能力标志, 以及 tmux/herdr 两份 "argv + 解析 + 错误分类" 的纸面表达验证, 标出需要轮询/回退/多步的操作; 该 `plan.md` 在本卡进入 review 前完成并作为下一张卡的输入.
- [ ] 排除 `_test.go` 后, `grep -rn '"herdr"\|"tmux"\|"tmux-session"' internal --include='*.go'` 的命中仅出现在: `internal/terminal/**`, `internal/config` 的内置默认名集合与 launcher 校验, i18n 文案键. launch, liveness, notify, takeover, focus, menu, tui 七个包内命中为零 (tui 现有的 `backgroundStartLauncher` 改为按能力标志判断).
- [ ] 排除 `_test.go` 后, launch, liveness, notify, takeover, focus, menu 六个包中不再有 `exec` herdr 或 tmux 二进制的代码; 所有对终端的访问经 `terminal.Backend` 方法.
- [ ] `config.Launchers` 固定列表被注册口取代, 未注册时回落内置默认集合; 单元测试覆盖: 未注册时默认名可用, 注册后新增名可用, 重复注册幂等, 完整二进制中 `terminal` 的注册先于首次配置校验.
- [ ] 新增 import 图测试: `internal/board`, `internal/config`, `internal/fs`, `internal/process`, `internal/i18n` 不 import `internal/terminal`; `internal/terminal` 与其子包不 import launch, liveness, notify, takeover, focus, menu, tui.
- [ ] 现有全部测试保持通过且未删改期望值; 需要移动的测试随代码移动到新包, 用例数量不减少 (以 `go test -list` 计数对比改动前后, 差异在 `plan.md` 逐条说明).
- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `AGENTS.md` 包表新增 `internal/terminal` 及子包一行, 更新 launch, probe, focus, takeover 的职责描述; 新增 `docs/terminal-backend.md` 说明接口操作集合与 "调用方只能经接口访问终端" 的规则.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 已有问题: herdr socket 上报 (`HERDR_SOCKET_PATH`) 与 tmux 前台进程名匹配的既有限制不变, 只搬运; 排除.
- 并发与跨平台加固: 探测超时, 取消, Windows job object 进程树等既有实现只搬运不改; 排除.
- 共享契约与文档: `AGENTS.md` 包表与新增 `docs/terminal-backend.md` 纳入; 发布规则 `rules/*.md` 不改, 因用户可见命令与行为不变; 部分纳入.
- 相邻功能与后续阶段: 声明式定义格式, 解析器, 用户定义搜索路径属 `20260908-terminal-definition-format-task`; herdr 钩子清单属 `20260908-terminal-herdr-hooks-task`; 一致性检查命令属 `20260908-terminal-conformance-test-task`; 排除.

## DISCUSSION

```text
PREREQUISITES: 20260908-agent-definition-group
```

- 设计结论: 接口操作集合从现有调用点归纳而非预先设计, 避免接口与实际用法脱节; 但接口一旦定下, 下一张卡的定义格式会一比一映射到这些操作, 因此操作应以 "终端能做什么" 命名, 不以 "kander 哪个命令在调用" 命名.
- 设计结论: `foreground`/`console` 保留在 launcher 名空间内, 以无容器后端实现, 而不是在调用方用 if 判断, 否则分支不会真正消失.
- 本卡与 agent 定义组修改同一批文件 (`internal/config/config.go`, `internal/menu/options.go`, 以及 launch, notify, liveness, takeover 四个包), 资源无法隔离, 按并行规则记录为依赖并串行: 等 `20260908-agent-definition-group` 全部到 `done/` 且交付已进 `develop` 后再启动. 这与用户决策 "Agent 侧先做, 终端侧后做" 一致.
- 本卡工作量大而机械, 审核重点是 "行为不变" 而非设计; 执行者应优先保证测试覆盖不减少.

- SELF_REVIEW: 通过. 纯搬运卡, 验收以 "行为不变" 为核心 (grep 命中范围, 用例数不减少, 期望值不删改, import 图测试); 接口操作集合由执行者归纳并在 plan.md 列出, 已在 DISCUSSION 说明命名原则; foreground/console 归无容器后端的决策已写明. SIZE large 合理.
- CARD_REVIEW (第一轮, 建卡时): 通过, 附两处提醒已采纳 (独立 agent). (1) import 图测试要求 config 不 import terminal, 与后续卡开放 launcher 名校验冲突, 已在本卡预留 launcher 名注册口; (2) focus 包经 HERDR_SOCKET_PATH 的 pane 级聚焦应归入 focus 操作, 已写入 EXPECTED_OUTCOME. 145 处分支分布核实一致.
- 规则复核 (2026-09-08, 建卡会话再次对照任务组规则): 原 PREREQUISITES 为 N/A 且标注可与 agent 组并行, 但两组改同一批文件, 已改为依赖 `20260908-agent-definition-group` 并删除并行表述.
- 任务组说明 (本卡是 `20260908-terminal-definition-group` 依赖序第一张): 组内 4 张卡超过建议的 3 张, 保留理由是四卡共享同一集成契约 (`terminal.Backend` 接口与定义文件格式) 且依赖链接近线性: 本卡 (收口) -> `20260908-terminal-definition-format-task` (格式与 tmux 迁移) -> `20260908-terminal-herdr-hooks-task` (herdr 迁移) -> `20260908-terminal-conformance-test-task` (对两份最终定义做一致性检查, 依赖 format 与 herdr-hooks). 跨组依赖: 本卡依赖 `20260908-agent-definition-group` 整组进入 develop (共改 config/menu/launch/notify/liveness/takeover), format 卡另依赖 `20260908-agent-review-template-task` 交付的 `internal/process` 输出解析结构 (对整组而言该依赖已被组依赖覆盖, 保留为显式记录).
- 接口草案 (从现有调用点归纳, 供执行者在 plan.md 细化; 括号内为输入 -> 输出): CreateContainer(cwd, label, options -> container, pane 地址); WaitReady(pane, timeout -> ok); Run(pane, argv -> ok); DeliverText(pane, text, enter -> ok); WaitMarker(pane, marker, timeout -> matched/timeout); SetMeta(pane, key, value); GetMeta(pane, keys -> 值或 "缺失"); PaneFacts(pane -> 存活, 前台进程, copy-mode, agent 状态, 会话引用等既有字段); Topology(pane -> 所属容器, 容器内 pane 数); Focus(pane -> 成功/降级并附说明/失败); Close(container -> ok); ParseAddress/FormatAddress(WINDOW 字符串 <-> 地址); Capabilities() -> 能力标志. 错误分类: gone (容器/pane 已消失), meta_missing (元数据缺失), other (普通失败).
- 计划: 执行者启动时先写 `plan.md`, 建议阶段: (1) 接口与注册表 (含上述纸面验证); (2) tmux 后端搬运; (3) herdr 后端搬运 (含 socket 聚焦); (4) 七个调用方切换; (5) import 图与用例数对比; (6) 文档.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) 接口只有操作名, 缺输入输出、能力差异与组合语义, 后置格式卡无法一比一映射, 已要求 plan.md 给出类型化签名、地址契约、能力标志与 tmux/herdr 纸面表达验证, 并在 DISCUSSION 附接口草案; (2) launcher 名注册口缺初始化契约, 已明确默认集合、幂等注册与注册时点测试; (3) 四成员组未记录保留理由与集中依赖图, 已补. 三项已修正.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
