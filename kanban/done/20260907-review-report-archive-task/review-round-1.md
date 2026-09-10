# A 第三批首轮审核处置

任务：20260907-review-report-archive-task。作者：原执行端 Codex。本文是作者独立核实与修复记录，不替代 PM/QA 复审结论。

## 审核输入与交付

- 完整派回通知、PM/QA 原报告逐字保存于 [审核原文](review-round-1-original.md)，包含 NON-BLOCKING 项。
- 固定审核 base：`839f72119b22ce8b48811fc9819de9643a4028f8`。
- 首轮 target / 后续 reviewed-commit：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`。
- 最新组基线：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`；fetch 成功，任务分支 rebase 此组 HEAD，无冲突且没有重写提交。
- 最新交付：`e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`；本地 HEAD 与 origin/review-report-archive 一致。
- 本轮提交：
  - `ebb13ee9c180be6d5e4e3ae8e1576d35e7c912a5`：显式看板覆盖优先于工作目录读取。
  - `75bcfb2a2a3d4ec0fc59d54a15f29f5b46bca4b3`：统一 REVIEWS 定位、诊断归属与排序、损坏记录聚合及发布状态文档。
  - `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`：报告重放字节一致、真实 CLI 推进测试、冻结上下文规则与测试入口清理。

## 逐项作者处置

| 原作者编号 | 作者结论 | 独立证据、处理及验证 |
| --- | --- | --- |
| PM001 medium mechanical | 成立，已修复规则 | `PrepareReviewRun` 确实同时冻结 task-context 哈希与 report_language，原规则漏写。`TestArchiveBatchAdvanceRangeAttribution` 现在通过真实命令入口：首次以 spec 路径运行，受控 update 添加执行记录，同批修复传实时 spec 得到 `batch binding conflict` 且不创建新 run；改用首轮归档 task-context.md 绝对路径后，推进、发布、前驱完整性校验均成功。规则补全冻结五项及后续角色/修复/恢复复用原件路径，保持同 batch。 |
| PM002 medium mechanical | 成立，已修复测试 | 原 advance.json 从未被 CLI 读取，rest 仅被丢弃，实际只有内存校验。改用 `--advance-file` 经 `captureRun` 贯穿读取、Git 范围校验、意图和发布；断言外部成员出现含目标 SHA 的 `unattributed or foreign delivery`、exit=2、无意图，纠正成员并复用冻结上下文后成功。检查 JSON 编码错误和首轮退出码，移除无效 rest 计算。 |
| PM003 low | 成立，已修复；与 QA-03 共用处置 | 子进程进入隔离临时目录并删除该 cwd，确认 Getwd 失败；绝对 KANBAN_DIR 仍有效。修复前 BoardRoot 返回找不到看板，修复后优先读取显式覆盖并正确返回。`TestBoardRootOverrideAfterCWDDeletion` 通过，不更改测试主进程 cwd。 |
| PM004 low | 成立，已修复；与 QA-04 共用处置 | Claude 假 CLI 的真实 review/重放链复现首轮多一个换行：无尾换行及已有尾换行两种原报告都不一致。采纳 QA 的输出原字节方案，归档模式首轮写入 report 原字节并 sync；不向归档原件添加换行，也不在重放时改写原件。`TestArchiveJSONReviewerReplayBytes` 断言首轮、归档 report、删除 CLI 后的重放三者字节相同；无任务绑定仍沿用原行为。 |
| PM005 low | 发布状态事实成立；采纳文档修正，拒绝移回 done 的恢复描述 | `TestReviewPublicationRejectsTerminalMove` 经受控 update 完成测试卡 SUMMARY/report，再以 MoveWithOptions(Result=completed) 实际移入 done。此后逐卡发布失败，原件可读取；默认 check 跳过 done，显式卡 ID check 报未完成发布；MoveEntry(done,working) 被拒绝。原报告所说“移回 working/review 后重试”不符合终态不可复用规则。文档限定待发布期间 working/review 互移，终态迁移前先完成发布；提前进入终态须另行确认处理。未增加 done 门禁、未放宽状态、未实现 R。 |
| PM006 suggest | 成立，采纳诊断聚合 | 四类隔离破坏分别为 runs 下非目录条目、非法 JSON、错误 schema、无效 UTF-8 卡文档。修复前均使 CheckReviewEvidence 提前返回并跳过另一张已篡改报告的卡；修复后收集局部问题并继续检查，其余卡的 task/run 诊断保留。根目录枚举、锁、事务恢复等整体失败仍返回错误，未忽略失败。`TestReviewCheckContinuesAfterInvalidRecords` 四项通过。 |
| PM007 suggest out-of-contract | 写入/读取事实成立，保留范围外；不实施清理 | 独立临时探针使用真实 Prepare/Finalize/Publish 与 operationRecords：单卡单 run 留有 5 条操作记录，report.md 有 2 份 committed 日志副本，分别对应控制 originals 与卡片 reviews。调用链为 WithTransaction → pending → operationRecords → readOperationRecords，全量读取并 JSON 解码日志；当前没有裁剪已提交日志的路径。实际耗时增长未做基准，性能量级仍是推断。契约明确排除保留期清理且不重做 S；本轮未改日志实现。后续可另卡评估头部扫描/按需解码与保留期策略，需测量及恢复兼容性设计。探针执行后删除，未把日志保留策略固化为本卡回归。 |
| QA-01 medium | 成立，已修复 | 实测公开 CheckBoard 返回的 stderr 原为仅 `report.md: hash mismatch`，缺任务及 run。CheckReviewEvidence 将任务标识写入 Message；checkRunStructure/verifyCardReview 统一补 run 标识，底层错误保留。`TestReviewCheckPublicDiagnosticsAndOrder` 同时篡改两卡的 report/manifest、各两个 run，断言公开 stderr 每行可定位卡及 run。 |
| QA-02 medium | 成立，已修复 | 不只验证字符串：`TestReviewPublicationAfterControlledHeaderUpdate` 覆盖标题尾空格、LF 卡仅标题变 CRLF、已有 CRLF 卡标题添加空格/tab。每项均首次发布、通过 UpdateDocument 改标题并追加独立正文、发布第二角色、同 run 再发布、验证两份完整证据与 CheckBoard。修复前均在后续重试出现 duplicate REVIEWS section；修复后两条索引、正文后续章节保留、完整性检查通过。读写共用同一标题/区段定位器，保留区段换行风格，提交前复核构造后的索引。测试未绕过受控 update 修改既有 REVIEWS。 |
| QA-03 low | 成立，已修复 | 同 PM003，保留此独立编号；子进程删除 cwd 的实际 BoardRoot 调用回归通过。 |
| QA-04 low | 成立，已修复 | 同 PM004，保留此独立编号；采用原字节 stdout 写入，Claude 两种尾换行真实运行与重放链回归通过。 |
| QA-05 low | 成立，已修复 | CheckReviewEvidence 对输入任务、run、最终问题排序；原件哈希校验按文件名排序，避免多份损坏时首个文件不稳定。排序在公开 evidence API 内完成，因此所有调用方一致，不要求 deps 预排序。两卡两 run 连续 20 次公开 CheckBoard 断言四条诊断精确相同且任务/run 顺序固定。 |
| QA-06 recommend | 成立，采纳文档补全 | AGENTS.md 包图的 board 职责已加入审核 run/batch 身份、原件、逐卡发布索引与完整性校验。仅说明既有依赖方向和职责，没有改变包边界。 |
| QA-07 suggest | 成立，采纳清理 | rg 核对旧 validateContext 仅为测试包装，生产调用 validateContextMode。删除未导出包装，恢复测试显式传 replay=false；推进测试改走 CLI。无外部兼容调用需要保留，代码搜索不再有旧函数调用，全量 review 测试通过。 |
| QA-08 suggest | 成立，已修复 | 同 PM002，保留此独立编号；移除只为丢弃而计算的 rest，测试改用真实 CLI 参数与推进文件。 |

## 验证及实际限制

- 修复前新回归真实失败：删除 cwd、三种受控标题更新后的连续发布、缺归属公开诊断、四类损坏记录中止、两种 JSON 报告尾换行差异。退出 1，作为复现而非通过记录。
- 终态边界测试最初因 fixture 的 SUMMARY 未完成而被既有 done 条件拒绝；补齐隔离 fixture 后才完成终态行为验证，没有据此前置失败宣称产品缺陷。
- `go test ./...` 通过，包含实际子进程崩溃/事务恢复测试。
- `go test -race ./internal/board ./internal/review ./internal/fs` 通过。
- `go build ./...`、`go vet ./...`、`git diff --check` 通过；改动 Go 文件 gofmt 无输出。
- Windows amd64：board/review/fs 测试程序与完整 cmd/kander 二进制交叉编译通过；Linux 主机未执行原生 Windows 测试，缺口继续保留。review 新增命令行/假 CLI 用例及删除 cwd 用例为 Unix 平台测试。
- 增强推进测试的错误原因断言后，专项 `go test ./internal/review -run TestArchiveBatchAdvanceRangeAttribution -count=1` 再次通过。
- 最新组 HEAD rebase 后全量复验结果与原始测试输出见 [测试记录](review-round-1-tests.txt)。
- 本轮 Reviewer 接口测试使用隔离假 CLI；未执行新的 PM/QA 审核。首轮 PM/QA 未运行测试的事实保留，以上验证由执行端实际运行。
- 安装作用域 `kander check 20260907-review-report-archive-task` 通过。卡片记录继续通过 S 基线构建的 show/update 受控入口写入；未部署构建产物、未迁移真实看板。

## 本轮结论与后续

本轮已修复所有核实成立的阻断项，low/recommend/suggest 均逐项提供作者结论。没有已知遗留的本轮代码阻断；这不是 PM/QA 语义 PASS 声明。PM007 的范围外日志成本与 Windows 原生验证缺口保留。

请编排端接收最新任务分支交付，沿用同一固定 base/spec，reviewed-commit 使用首轮 target。执行端不自行审核、不修改组分支、不集成 develop、不清理 worktree。工作区、任务分支及交互会话保留，卡片进入 review 等待复审；RESULT 仍为空，完成收尾须等待组通知。
