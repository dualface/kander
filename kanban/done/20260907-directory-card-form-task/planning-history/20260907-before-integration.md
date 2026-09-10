# 统一卡片目录形态, 任务规模改由 SIZE 字段承载

- TYPE: Feature
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

把看板卡片统一为目录形态, 让"任务规模"不再由入口形态推断.

今天 `internal/board/scan.go` 完全靠形态推 `Kind`: `*-task.md` 单文件是 small, `*-task/spec.md` 目录是 large. 这个值下游用在四处: `start` 按 `kanban_agents.large|small` 选执行 Agent 与模型档位 (`internal/launch/commands.go`, `session.go`); 启动 prompt 的开头词 (`internal/launch/agent.go`); 进 `todo/` 的门禁对 large 额外要求 `CARD_REVIEW:` (`internal/board/document.go`); `list` 与 TUI 的 SIZE 列 (`list.go`, `payload.go`). 形态同时承担"规模"和"能否容纳附属文件"两个职责, 导致小卡没有目录, 也就无处存放本任务组后续要落盘的审核报告.

改为: 每张卡都是状态目录下的 `<task-id>/` 目录, 正文固定为 `spec.md`; 规模改由元数据字段 `SIZE: small | large` 显式承载; `kander new` 只再产出目录卡; 存量单文件卡由 `kander init` 一次性迁移为目录, ID 不变.

## USER_DECISIONS

以下为用户在本次讨论中明确确认的决定:

1. 不论单卡还是任务组成员卡, 一律采用目录形式.
2. 每次审核的结果依次写入卡片目录 (由本组第二张卡 `20260907-review-report-archive-task` 实现); 任务卡正文只追加审核结果文件的索引.
3. 按提议的三卡拆分建立任务组 `20260907-review-archive-group`, 本卡为第一张; 建卡后不启动, 何时启动由用户决定.

## EXPECTED_OUTCOME

- 看板里不再有 `*-task.md` 形态的活动卡; 每张卡是一个目录, 里面至少有 `spec.md`.
- 大小卡的全部既有行为 (执行 Agent 与模型选择, 启动 prompt 措辞, `CARD_REVIEW:` 门禁, 完成门禁对 `SUMMARY` 或 `report.md` 的要求, SIZE 列) 只由 `SIZE` 字段决定, 与形态无关; 缺字段的旧目录卡按 large 处理, 因为旧方案下只有 large 才会是目录.
- `kander init` 在已有看板上重跑时把全部状态目录下的单文件卡迁移为目录卡, 幂等, 逐卡原子, 中断后再次运行可收敛.
- 迁移前的过渡期内, 只读命令 (`list`, `show`, `check`, TUI, `subscribe`) 仍能读取单文件卡; 改变状态或回写正文的命令对单文件卡失败关闭并提示先运行 `kander init`.
- 描述卡片形态的规则与文档与新行为一致.

## ACCEPTANCE_CRITERIA

- [ ] `go build ./... && go test ./... && go vet ./...` 全绿, `gofmt -l .` 无输出, `git diff --check HEAD` 无告警.
- [ ] `kander new <type> <slug> <title>` 创建 `backlog/<task-id>/spec.md`, 元数据含 `- SIZE: small`; `kander new --large` 同样创建目录, 元数据为 `- SIZE: large`. 两者都不再创建 `<task-id>.md`. `SIZE` 行位于 `TYPE` 之后, `TASK_GROUP` 之前.
- [ ] `SIZE: small` 的目录卡 `spec.md` 含 `IMPLEMENTATION` 与 `SUMMARY` 两节, 进 `done/` 要求 `SUMMARY` 已填且无占位符; `SIZE: large` 的目录卡 `spec.md` 不含这两节, 进 `done/` 要求非空 `report.md`. 即完成门禁按 `SIZE` 分支, 结果与今天按形态分支一致.
- [ ] `SIZE` 取值只接受 `small` 与 `large`; 其他值在 `kander check` 中报为契约缺陷, 且 `move`/`pick`/`start` 对该卡失败关闭. 目录卡缺 `SIZE` 行时按 `large` 处理, 单文件卡 (过渡期读取) 按 `small` 处理; `check` 把缺 `SIZE` 的目录卡报为问题.
- [ ] `kander start` 对 `SIZE: large` 选 `kanban_agents.large` 及 `large_model`/`large_effort`, 对 `SIZE: small` 选 `kanban_agents.small` 及 `small_model`/`small_effort`; 启动、恢复、通知写入的 prompt 开头词随 `SIZE` 变化. 用现有 launch 测试改造覆盖.
- [ ] 进 `todo/` 的门禁: `SIZE: large` 或 `TASK_GROUP` 非空的卡要求 `CARD_REVIEW:` 行; `SIZE: small` 且无任务组的卡只要求 `SELF_REVIEW:` 行.
- [ ] `kander list` 与 TUI 的 SIZE 列读取 `SIZE` 字段; `board.TaskSummary` 的 JSON `kind` 字段取值仍为 `small`/`large`, 含义不变.
- [ ] `kander init` 在已有看板上: 对 7 个状态目录下每个 `<task-id>.md` 单文件卡, 创建同名目录并把原文件变为其中的 `spec.md`, 补写 `- SIZE: small`; 对缺 `SIZE` 的既有目录卡补写 `- SIZE: large`. 迁移只做重命名和补一行元数据, 不改写其他正文. 完成后输出迁移的卡数与卡 ID, 其中 `working/` 与 `review/` 中被迁移的卡单独列出并提示相关执行 Agent 需重新 `kander show` 定位; 第二次运行输出 0 张且不改动任何文件 (以 mtime 或内容哈希为证).
- [ ] 迁移逐卡原子: 任一中间步骤失败时该卡要么仍是完整的单文件卡, 要么已是完整的目录卡 ("完整"指含 `spec.md` 且 `spec.md` 已带合法 `SIZE` 行), 不出现同 ID 文件与目录并存、目录存在但缺 `spec.md`、或目录卡缺 `SIZE` 的状态; `SIZE` 行的补写发生在临时目录内的 `spec.md` 上、最终重命名之前. 中间产物使用不以点开头、不以 `-task` 结尾的临时名 (如 `<task-id>.migrating`), 使 scan 的 default 分支把它报为无效入口而不是静默忽略; 下次 `init` 能识别并收敛 (完成或回退). 用注入失败的测试覆盖.
- [ ] 迁移遇到同 ID 目录已存在、目标或源为符号链接/reparse point 时对该卡失败关闭并报告, 不影响其他卡的迁移; 全部写入经 `internal/fs`.
- [ ] 过渡期: `list`/`show`/`check`/TUI/`subscribe` 能读取尚未迁移的单文件卡; `check` 把非 `done/archived/trash` 状态下的单文件卡报为问题并提示运行 `kander init`; `move`/`pick`/`start`/`resume`/`notify`/`dismiss` 以及 `start` 的元数据回写对单文件卡失败关闭, 错误信息提示先运行 `kander init`, 且不做任何部分写入.
- [ ] `kander guard-write` 对目录卡内任意路径 (`<state>/<task-id>/...`) 的判定与今天对 `<task-id>/spec.md` 的判定一致; 对已迁移卡的旧路径 `<state>/<task-id>.md` 写入被拒绝, 理由指向卡片现在所在状态.
- [ ] `kander show` 与 `kander move` 打印的路径与今天 large 目录卡的打印方式一致, 规则中"以打印路径为准再写入"的约定继续成立.
- [ ] 卡片目录内的相对链接在 `move` 后仍然有效 (沿用现有不变量), 新增测试覆盖 small 目录卡.
- [ ] 新增的面向用户字符串在 `en.json`、`zh-CN.json`、`ja.json` 成对存在, `internal/i18n` 目录测试通过.
- [ ] 规则与文档同步: `rules/KANDER-KANBAN-RULES.md` 的 "Invariants"、"Small Task Template" (增加 `- SIZE:` 行)、"Large Task Documents"、"Task Scale and Grouping" (形态升级改为修改 `SIZE`, 进 `todo/` 后 `SIZE` 冻结)、Command Contract 中 `new`/`init`/`check`/`guard-write` 的说明, `rules/KANDER-TASK-GROUP-RULES.md` "Task IDs" 一节, 以及 `rules/KANDER-AGENTS.md`、`README.md`、`AGENTS.md`、`docs/` 中提到单文件卡或 `-task.md` 的句子全部改为新形态. 完成后 `grep -rn -- '-task\.md' rules README.md AGENTS.md docs` 只剩描述迁移或过渡期读取的句子.
- [ ] 引用卡片形态的既有测试 (以 `grep -rln -- '-task\.md\|spec\.md' internal --include='*_test.go'` 为准) 全部改为目录卡; 保留少量显式标注为过渡期读取的单文件卡用例.
- [ ] 遵守仓库语言约定: 新增 Go 注释与提交备注全英文; 规则文件保持英文; 用户可见字符串走 i18n.

## THREAT_MODEL

本卡包含对用户看板数据的批量重写 (迁移), 按跨数据操作对待.

- 资产: 主 worktree `kanban/` 下全部卡片, 其中可能有 `working/` 中正被执行 Agent 使用的卡; 卡片内容会被 Agent 当作契约读取.
- 可信主体: 本机当前用户及其启动的 Agent.
- 攻击者能力: 能在看板目录内预置符号链接、junction 或其他 reparse point, 诱导迁移把文件写到看板外或覆盖别处文件; 能预置同名目录制造歧义.
- 对策: 全部创建、重命名与 `SIZE` 行的原子替换经 `internal/fs` 的固定父句柄与 no-follow 语义, 逐分量拒绝 reparse point; 同 ID 目录已存在时硬失败; 正文只经重命名加一次原子替换, 不复制, 失败时原文件仍在; 不删除任何用户内容. `init` 单独列出 `working/`、`review/` 中被迁移的卡, 便于编排会话通知执行 Agent 重新定位.
- 非目标: 不防御已能任意写入用户 HOME 的本地攻击者; 不做卡片内容签名.

## OUT_OF_SCOPE

按既有问题 / 加固 / 共享契约与文档 / 相邻功能四类界定:

- 既有问题 — `kander check` 对 `done/archived` 默认不检查 (`deferredCheckStates`): 排除理由: 与本卡无因果; 迁移覆盖全部状态, 但 `check` 的状态范围维持现状.
- 既有问题 — `GuardWrite` 使用 `os.Lstat` 而非 `internal/fs` 句柄读取: 排除理由: 只读判定, 本卡只按新形态修正其路径逻辑, 不改其文件访问方式.
- 加固 — 迁移过程的看板级互斥锁 (防止两个进程同时 `init`): 排除理由: 现有看板操作以"同一文件系统上的重命名"为唯一原子原语, 没有看板级锁; 本卡沿用该模型, 逐卡原子即可, 增加锁属独立设计.
- 加固 — 在任意命令定位看板时自动迁移 (类似 `review/` 目录自动创建的先例): 排除理由: 迁移会重命名用户数据, 在只读命令里做写操作且 Windows 上可能因文件占用失败, 不宜隐式触发; 本卡只在 `kander init` 显式执行. 用户未要求自动迁移.
- 相邻功能 — 由 `kander doctor` 执行或提示迁移: 排除理由: 方案讨论时写的是 `doctor`/`init` 二选一; 核实 `internal/menu` 的 doctor 只处理配置、规则文件与环境, 不以看板目录为工作对象, 因此迁移只放在看板自己的维护命令 `kander init`.
- 共享契约与文档 — 移除过渡期对单文件卡的读取支持: 排除理由: 属后续阶段; 待用户确认所有机器已迁移后另开卡删除, 本卡保留读取以避免升级即失效.
- 共享契约与文档 — `models.kanban.*` / `kanban_agents.*` 配置键的改名: 排除理由: 仓库 `AGENTS.md` 明确要求保持 onevoke schema 不改名; `SIZE` 字段的取值 `small`/`large` 正是为了对齐这些键.
- 相邻功能 — 卡片目录下的 `reviews/` 归档与索引行: 由本组第二张卡 `20260907-review-report-archive-task` 实现; 本卡只保证每张卡有目录可放.
- 相邻功能 — `kander new` 之外的卡片创建入口 (如 TUI 内建卡): 排除理由: 今天不存在, 用户未要求.

## DISCUSSION

```text
PREREQUISITES: N/A
```

**关键技术结论 (建卡前已核实)**

- `internal/board/scan.go` 第 51-56 行以入口形态决定 `Kind`; `Kind` 的消费点见 `internal/launch/commands.go:83`、`session.go:329`、`agent.go:183`、`metadata.go:131`, `internal/board/document.go:217,240`、`move.go:58`、`payload.go:59`、`list.go:141`. 本卡把 `Kind` 的来源改为 `SIZE` 字段 (经 `Entry.Kind` 中转即可, 无需改调用方签名), 形态分支只保留在 scan 的过渡期读取里.
- 缺 `SIZE` 兼容规则的推导: 旧方案下 small 必为文件、large 必为目录; 新方案下所有新卡都带 `SIZE`. 因此一张"没有 `SIZE` 的目录卡"只可能是旧 large 卡, 按 large 处理是唯一正确的回落; 若按 small 回落, 存量大卡会在迁移前被换成 small 的 Agent 与模型. `check` 仍把缺字段报为问题, `init` 负责补写.
- `GuardWrite` (`internal/board/guard.go:21-60`) 只在"其他状态"里探测 `<id>` 与 `<id>.md` 两种拼写 (循环里 `if other == state { continue }`); 卡迁移后仍在同一状态时, 对旧路径 `<state>/<id>.md` 的写入落到 `guard_missing_direct_child`, 理由不指向状态. 满足验收需要新增同状态下 `<id>/` 目录的探测并给出"已迁移为目录卡"的理由, 不只是改文案.
- 完成门禁 (`document.go:240`) 按 `Kind == "small"` 检查 `SUMMARY`, 否则检查 `report.md`; 模板 `small_extra.md.tmpl` 只给 small 追加两节. 改由 `SIZE` 分支后行为不变, 因此 `--large` 的语义完全保留.
- 看板目录经 `.git/info/exclude` 本地排除, 迁移不会产生 git 变更.
- 迁移原子性依赖同一文件系统上的重命名; `internal/fs.Rename(root, from, to)` 已是固定根句柄的实现. 建议路径: 先建临时目录 `<task-id>.migrating/`, 把 `<task-id>.md` 重命名为其中的 `spec.md`, 在临时目录内原子替换补写 `- SIZE: small`, 最后把临时目录重命名为 `<task-id>/`. 临时目录名不能以点开头: `scan.go` 第 38-40 行对点开头的项直接跳过, 半迁移状态会被静默忽略; 不以 `-task` 结尾则走 default 分支被报为无效入口, 可见且不被当成卡片. 两次重命名之间该卡对并发读者 (TUI 刷新, `subscribe`) 短暂不可见, 属已知瞬态, `init` 输出中说明即可.
- 定向 `kander check <task-id>` 走 `scan.go` 的 `scanTargetsOnce`, 只探测 `<id>.md` 与 `<id>/`, 看不到 `<id>.migrating/`, 会报任务不存在; 半迁移产物只由无目标的 `check` (全量 scan) 与 `init` 识别, 实现时不必让定向 check 识别它.
- 旧卡可能仍使用中文元数据 token (`legacyToken` 兼容), `SIZE` 是新字段没有旧拼写, 迁移补写英文 `- SIZE:` 行即可, 与中文 token 行并存不影响逐字段匹配.

**与后续卡的接口**

- 本卡交付后, 每张卡都有目录, `20260907-review-report-archive-task` 才能在其中创建 `reviews/` 并向 `spec.md` 追加索引行; 该卡不读取 `SIZE` 的取值.

SELF_REVIEW: 通过. 核对项与修正: (1) 目标与成果对齐用户"一律目录形式"的决定, 审核归档不塞进本卡; (2) USER_DECISIONS 只含用户三条明确确认, `SIZE` 字段、`init` 迁移、过渡期策略均作为设计结论写在 GOAL/DISCUSSION 而非用户决策; (3) 边界: 排除自动迁移、doctor 迁移、移除过渡读取、配置键改名, 均给出事实理由, 达成目标所需的 scan/move/guard/launch/门禁/规则同步全部在范围内; (4) 验收覆盖新建、门禁、Agent 选择、迁移原子性与幂等、过渡期读写、guard、文档 grep, 全部可判定. 首轮卡审后修正: 临时目录改为非点前缀 (scan 会静默跳过点前缀), `SIZE` 补写前移到最终重命名之前并纳入"完整目录卡"定义, GuardWrite 现状描述改准, 补 doctor 排除理由与 working/review 卡单列, 测试范围改以 grep 为准; 第二轮建议项已补: 定向 check 不识别 `.migrating` 产物写入 DISCUSSION.

CARD_REVIEW: PASS — 由不共享建卡会话上下文的独立子 Agent (Claude, general-purpose) 只读卡片、用户原始需求与仓库源码出具, 共两轮. 首轮 FAIL: 迁移原子性描述自相矛盾 (点前缀临时名会被 scan 静默跳过, `SIZE` 补写步骤缺位); 另有 GuardWrite 现状描述不准、并发瞬态未说明、doctor 被静默去掉、测试数量偏大等建议项. 全部修正后第二轮 PASS, 无必须修正项, 一条建议 (定向 check 不识别半迁移产物) 已补入 DISCUSSION.
