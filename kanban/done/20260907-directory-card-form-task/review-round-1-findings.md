# D 第二批首轮派回

任务：20260907-directory-card-form-task。先使用本卡现有受控入口 move working，再逐项独立验证下列 PM/QA finding。主控未替你确认或拒绝。

已知事实：两角色均对 base a7fe54beb6ade00678e14655c0d385115eb950c8、commit 70da00edfff664ec1c0740a49f6ea3ae51b42c8c 完成首轮，工具均 exit=0，语义均不通过；组分支已接收该提交，A 尚未启动。CSA/Hacker N/A。

PM-001 的原子写入内部 kill/restart 恢复问题属于现有验收，可先独立验证并修复。PM-002/QA-001 相对链接问题涉及“正文仅补 SIZE”与“保留链接目标”的冻结约束冲突；请先验证并记录证据，不自行改写冻结契约。主控已向用户提问：允许必要链接地址重写并纳入事务，或保持正文限制、对受影响链接拒绝迁移。当前尚未收到用户答复，选项默认值不是授权。其余独立修复继续；待主控传达实际决定后再完成该部分。

逐角色保留原作者处置，记录修复提交和验证，不因两个角色重复而丢失其中一个引用。全部可处理工作完成后，按组规则 rebase/验证/推送/记录最终 SHA。若仍等待契约决定，保持实际工作状态并在卡记录清晰的待定项，不把未决验收写为通过。不触发审核、更新组分支或集成 develop。

## PM 原文

Role: PM  
Commit: `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`  
Task Context: `/tmp/kander-d-batch2-context/spec.md`，第二批仅 D，11 条验收。  
Reviewed Scope: `a7fe54beb6ade00678e14655c0d385115eb950c8..70da00edfff664ec1c0740a49f6ea3ae51b42c8c`；目录迁移、SIZE 消费链、事务恢复、读取快照、规则及相关测试。未扩展 A/R/N/E，未重复报告 S 已关闭事项。

**结论：暂不通过。2 项 medium。** 原子要求共 22 项：Complete 17、Partial 3、Missing 0、Contradicted 0、Unverifiable 2。

### Requirement Table

Complete 表示静态实现证据完整，不代表本轮实机测试通过。下列实现证据为 Observed；运行时缺陷推断另行标注。

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| 复用 S 互斥 | 迁移全程持看板排他锁 | `internal/board/migration.go:30`、`transaction_lock.go:98` | Complete |
| 维护窗口 | 有迁移且存在 working/review 时默认拒绝；恢复同样检查 | `migration.go:85`、`:285` | Complete |
| new 目录形态 | 默认 small，--large 为 large，SIZE 紧随 TYPE | `move.go:35`、`templates/contract.md.tmpl:3` | Complete |
| 完成规模语义 | small 要 SUMMARY；large 要非空 report.md | `document.go:226` | Complete |
| 旧卡规模回退 | 文件缺 SIZE 为 small；目录缺 SIZE 为 large | `size.go:16` | Complete |
| SIZE 校验 | 非法、重复值拒绝变更 | `size.go:23`、`transaction.go:165`、`:235` | Complete |
| 消费方规模 | list/TUI、Agent、模型、prompt 使用 SIZE | `list.go:141`、`payload.go:58`、`launch/commands.go:86`、`:146`、`launch/prompts.go:96` | Complete |
| todo 审卡门禁 | 全部 SELF_REVIEW；large/组成员另需 CARD_REVIEW | `document.go:202` | Complete |
| SIZE 冻结 | todo 后冻结；退回 backlog 不解冻；显式决定可修订 | `update.go:91`、`:138`、`:186`、`recovery.go:218` | Complete |
| 七状态迁移与正文 | ID 不变，仅补 SIZE，兼容 CRLF/旧 token | `board.go:21`、`migration.go:69`、`:117`、`size.go:51` | Complete |
| 附件与相对链接 | 迁移后仍能访问原目标 | `migration.go:113`、`:200`、`:219`；见 PM-002 | Partial |
| 幂等 | 无候选时返回 0，不重写卡片 | `migration.go:78`、`:82` | Complete |
| 中间态读取 | 锁隔离；中断后全量/定向读取报告待恢复 | `snapshot.go:30`、`:58`、`:74`、`transaction.go:79` | Complete |
| 崩溃恢复 | 各迁移阶段中断后 init 可重做 | `migration.go:168`、`:212`；见 PM-001 | Partial |
| 真实异常保留 | 冲突、未知暂存、reparse 不猜测删除 | `migration.go:152`、`:165`、`:250` | Complete |
| 并发与订阅 | 共用锁；消费者使用已提交正文快照 | `snapshot.go:7`、`:234`、`deps.go:28`、`liveness/subscribe.go:142` | Complete |
| 旧文件过渡 | 保留读取；变更及执行副作用前提示 init | `size.go:40`、`transaction.go:166`、`launch/commands.go:78`、`notify/command.go:39`、`takeover/dismiss.go:46` | Complete |
| check 范围 | 默认继续排除 done/archived；缺 SIZE 目录提示修复 | `board.go:23`、`deps.go:296` | Complete |
| guard-write/update | 识别目录内部与旧拼写；保留外部竞态边界 | `guard.go:41`、`:49`、`update.go:158`、`docs/directory-cards.md:35` | Complete |
| 文档与实现一致 | 同步目录、SIZE、恢复及保留承诺 | `rules/KANDER-KANBAN-RULES.md:486`、`:490`；受 PM-001/002 影响 | Partial |
| 构建与验证 | build/test/vet、格式和差异检查通过 | 执行端声明通过；本轮只读核查，`git diff --check` 通过 | Unverifiable |
| Windows 原生验证 | 实机验证句柄、junction、锁与恢复 | `migration_windows_test.go:12`、`migration_test.go:295`；无原生结果 | Unverifiable |

### Findings

**PM-001 — medium — Inferred，置信度高：SIZE 写入过程中被杀会留下无法自动恢复的迁移**

违反验收第 6、7 条：进程中断后 `init` 应能重做迁移。

- `internal/board/migration.go:212` 在暂存目录中调用 `fs.WriteTextAtomic`。
- `internal/fs/posix.go:399` 创建 `.spec.md.<pid>.<time>.tmp`，`:405` 仅通过 defer 清理，`:427` 才替换正文。进程在创建后、替换前被杀，临时文件会遗留。
- 重启后，`internal/board/migration.go:173` 拒绝暂存目录内所有非 `spec.md` 文件，直接返回冲突。
- 此前源文件已在 `migration.go:200` 移走。卡片入口缺失，prepared 记录持续阻断读取，重复 `init` 无法完成。

这是 D 新增的暂存目录校验与既有原子写入实现之间的缺口。现有 kill 测试只停在外层 checkpoint，未覆盖原子写入内部窗口。

最小修复：让 SIZE 替换临时产物具有日志记录的身份和恢复步骤，恢复时安全处理自身产物，同时继续拒绝未知文件；增加临时文件创建后、替换前的 kill/restart 验证。

**PM-002 — medium — Inferred，置信度高：旧文件迁入子目录后，相对链接解析目标改变**

违反验收第 5 条：迁移应保留相对链接。

`internal/board/migration.go:117` 将 `<id>.md` 迁到 `<id>/`；`:200`、`:219` 最终形成 `<id>/spec.md`。`size.go:51` 仅补 SIZE，没有重定位链接或提供兼容解析。

例如，旧卡：

```text
backlog/20260907-legacy-task.md
```

包含：

```markdown
[参考](../done/20260907-reference-task/spec.md)
```

迁移前指向 `done/20260907-reference-task/spec.md`；迁移后指向 `backlog/done/20260907-reference-task/spec.md`。参考目录本身无需迁移，链接仍会失效。本轮纯路径计算确认该变化；未执行迁移实测。

最小修复：迁移预检识别解析目标会改变的链接，未处理前拒绝迁移该卡。当前“正文仅补 SIZE”与“保持链接目标”对此类卡存在冲突；需明确契约决定后，将必要链接重定位纳入事务，并验证迁移前后目标一致。仅保留链接字符串不足以验收。

NON-BLOCKING: none

### Verification

Observed：COMMIT TREE 的 277 个文件均与工作树 Git blob hash 一致；`git diff --check` 通过。

本轮遵守只读限制，未重跑 build/test/vet、race 或迁移测试。执行端通过声明与本轮结果分开记录。Windows 原生验证仍为 Unverifiable，不将交叉编译视为实机通过。

已尝试删除任务文件；只读文件系统拒绝，文件遗留不影响审核结果。

## QA 原文

Role: QA  
Commit: `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`  
Task Context: `/tmp/kander-d-batch2-context/spec.md`，D 的全部 11 条验收。  
Reviewed Scope: `a7fe54b..70da00e` 及相关事务、读写、启动、通知、订阅、TUI 消费链；不重复 S 已关闭事项。CSA/Hacker N/A。

### Behavior/Quality

| 范围 | 结论与证据 |
|---|---|
| 架构与维护访问 | Inferred：迁移归 `internal/board`，复用 S 排他锁、revision、日志及 `internal/fs`；未发现依赖方向漂移。 |
| 新卡、SIZE、附件 | Inferred：`new` 统一目录；规模独立解析；普通附件按形态判断。见 `move.go`、`size.go`、`transaction.go`。 |
| todo/done 与冻结 | Inferred：门禁消费 SIZE；large/任务组要求 CARD_REVIEW；冻结及明确决定沿用受控更新入口。 |
| Agent、模型、显示、prompt | Inferred：调用链消费 `Entry.Kind` 或 SIZE；list/TUI 经 `BoardPayload` 获取规模。 |
| 七状态、正文、CRLF、幂等 | Observed：存在字节、迁移计数及 mtime 断言；相对链接语义未保持，见 QA-001。 |
| 崩溃与异常保留 | Inferred：七个 checkpoint 接入真实发布函数；测试包含子进程 kill/restart、重复入口及未知产物保留。 |
| 并发、快照、订阅 | Inferred：维护锁串行化迁移/写入；`Board.Document` 保存已提交正文，相关显示、依赖、订阅读取已接入。 |
| 旧文件只读、check、guard-write | Inferred：变更入口拒绝旧文件；默认历史状态排除保留；旧路径提示已更新。 |
| 格式、资源、文件大小 | Observed：提交树与 evidence 一致；`git diff --check`、变更 Go 文件 gofmt 检查通过；三语资源键一致；未触发 1000 行门禁。 |
| 执行验证 | Unverifiable：本轮只读沙箱未运行 build/test/vet/race；交付记录中的通过声明不算 Reviewer 实测。Windows 原生验证缺口保留。 |

### Gate Findings

**QA-001 — medium — Inferred，置信度高：旧文件迁移使有效相对链接失效。**

- **违反要求：** 验收第 5 条要求保留相对链接。
- **代码证据：** [migration.go:79](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/migration.go:79) 仅调用 `addSize`；[migration.go:117](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/migration.go:117) 将文件迁为目录；[migration.go:244](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/migration.go:244) 进一步限定正文只能补 SIZE。没有链接重定位。
- **可达场景：** `kanban/backlog/<id>.md` 含 `[说明](../../README.md)`。迁移至 `<id>/spec.md` 后，同一链接由项目 `README.md` 改指 `kanban/README.md`。本轮只读路径计算确认两者不同。
- **影响：** init 成功、日志 committed，但原本可用的说明、图片或附件引用失效；重复 init 不会修复。
- **测试缺口：** [migration_test.go:89](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/migration_test.go:89) 为旧文件写入链接，却未建立对应文件或验证解析目标；实际附件断言仅覆盖既有目录。
- **最小可靠修复：** 将链接重定位纳入持久迁移计划与恢复校验，保持迁移前后的解析目标。此方案需要明确允许修改链接地址，解决“正文只补 SIZE”的约束冲突；此前应在预检阶段拒绝会破坏链接的迁移。增加临时项目文件测试，验证正常迁移和恢复后的链接目标及内容。

未发现其他 gate findings。

### NON-BLOCKING

NON-BLOCKING: none

任务文件已尝试删除；只读文件系统拒绝，文件遗留不影响审核结果。
