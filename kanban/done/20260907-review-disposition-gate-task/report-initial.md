# 审核处置与完成门禁交付报告

## 交付

- 最终提交：`d325bb9755cba0ac704a6841cd1d2d786eed42ee`；任务分支 `review-disposition-gate`，本地与 origin 一致，已正常推送。
- 组基线：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；再次 fetch 后 rebase 无变化，无冲突、无提交重写；随后全量复验通过。
- 工作树：`/home/dualf/works/kander/worktrees/review-disposition-gate`。
- 复用 S 的按 ID 事务及 A 的 run/batch/原件；board 提供纯结构类型与校验，review 提供受控命令、自动增量上下文及最终 Git 验证。
- 新增 plan/extend-plan/assign/disposition/map-legacy/aggregate/advance/close/progress，均归入现有 review 单入口；没有第二二进制入口。
- 未部署新二进制，未迁移真实看板，未更新组分支或集成 develop。

## 验收自检

13/13 项实现自检满足；这不是 PM/QA 审核结论。Windows 原生验证缺口单独保留。

| 项 | 实现与验证证据 |
| --- | --- |
| 1 作者与处置绑定 | 原作者记录绑定 run/finding/batch/task/author/time/report hash/原文/修复 SHA；working OWNER 与 assignment、revision 校验；追加链保留旧记录；独立 opinions 不替代作者。 |
| 2 全批聚合 | 按完整 assignment 校验所有归属和作者原件，生成每卡完整 disposition；共享 finding 缺任一作者即拒绝；无 finding 成员无需抄写或派回。 |
| 3 结构与 lineage | 固定 kander-findings JSON、FINDINGS/NON_BLOCKING、唯一稳定 ID；重复 ID 与重复 JSON key 均拒绝；仅解析真实条目；增量复用 ID 必须显式 lineage。 |
| 4 旧报告 | findings_schema=0 仅通过显式人工映射、完整依据及逐字原文行范围参与；空映射、缺定位不能通过；新 schema 无法使用旧映射绕开格式要求。 |
| 5 状态与例外 | confirmed/unverifiable 不闭批，fixed 绑定后续修复和验证，rejected 保留依据；非阻断 fixed/deferred/rejected；CSA/Hacker 适用 waiver 保留非 PASS 状态，PM/QA 不可使用。 |
| 6 机械与非机械 | 独立 advance CAS 接收机械修复，无需空跑 Reviewer；机械分类与验证保留；非机械项要求后续 run 覆盖；无机械项不能任意抬高 PASSED_AT。 |
| 7 显式 plan | 四角色要求与 N/A 原因、成员、执行周期和目标固定；无索引、缺角色、全失败不能 done；非 Git 且全 N/A 时记录真实不适用，不伪造 Git。 |
| 8 活动及旧卡 | 旧完成卡 legacy-untracked；活动卡缺计划显示 requirements-needed，不能完成；有效待处置是 pending，结构错误仍失败；tracked 周期丢计划不回落旧卡。 |
| 9 关闭与批次链 | plan 支持受控追加与 seal；所有必需角色、作者、运行副本和未解决项满足才 closed；修复 batch ID 不变；target CAS；后批 base 绑定前批真实闭批目标。 |
| 10 Git 分层 | review 验证最终干净 HEAD、commit 对象、base/PASSED_AT/fix/target 关系；board 仅复验结构、视图哈希及证据绑定，不 import review、不冒称集成。 |
| 11 自动增量 | --task/previous-run-id 自动带入前驱报告原文、作者记录及工具视图；意图落盘前再校验快照；错误身份与损坏副本启动前拒绝；前轮 confirmed 等失败语义可用于复审。 |
| 12 共用门禁与回归 | check/done 共用校验；覆盖缺角色、全失败及显式替代、部分原件/闭批发布、空及重复结论、错归属、旧 ID、跨批假接续、机械、无 finding 成员、N/A、旧完成卡及并发 CAS。 |
| 13 规则及验证 | 更新 review/task-group/kanban 规则、中文 schema 文档与索引、三语帮助；全量、race、build/vet、格式检查通过；CSA/Hacker 本仓库保持 N/A。 |

## 验证

- `go test ./...`：通过；rebase 后再次通过。原始输出见 [验证记录](validation.txt)。
- `go test -race ./internal/board ./internal/review ./internal/fs`：通过；包含同卡作者记录并发 CAS 及继承的归档/迁移/恢复回归。
- `go vet ./...`、`go build ./...`、`git diff --check`、全部 internal Go 文件 `gofmt -l`：通过。
- 本次修改的全部 Go 文件不超过 1000 行。
- Windows amd64：board/review/fs 测试程序及完整 kander 二进制交叉编译通过。主机为 Linux，未原生执行 Windows 测试；不把编译当作 Windows LockFileEx/DACL/reparse 实机通过。
- CLI 回归使用隔离假 Reviewer，验证真实 review 命令、结构化原件、作者记录、自动增量和 Git 门禁；未冒称实际 Reviewer 审核通过。
- 开发期发现并修复：旧 lifecycle/dismiss 测试缺新 N/A 计划、增量旧夹具无结构、重复 JSON key 可吞 finding、N/A 非 Git 兼容、批次 CWD 绑定及重复 ID lineage。最终无测试失败。

## 接口与适用边界

详细 schema 与命令见仓库 `docs/review-disposition.md`。ReviewPlan、ReviewAssignment、ReviewDisposition、ReviewBatchView、ReviewClosure 和 ReviewGitEvidence 均由 board 暴露；已有 run/batch 模型仅追加 findings_schema、plan_id 与 previous_batch_id。

作者、适用性、业务归属和用户决定引用由遵守协议的调用者如实提供；哈希与受控事务防误写、漏项、错轮次及不一致，不提供同用户恶意篡改的数字签名保证。原作者结论与编排意见分别保存。最终集成授权及实际 develop Git 核验继续走既有协议。

## 审核与收尾

PM/QA：本执行端未触发，等待编排端接收本交付并安排组审核。CSA/Hacker：按仓库 AGENTS.md 标记 N/A。

分支、worktree 与交互会话保留。RESULT 为空，返回 review 后等待编排端派回，不自行集成或清理。

## 未解决事项

1. Windows 原生环境缺口：只有交叉编译，没有 Windows 实机执行。
2. PM/QA 组审核、组分支接收、develop 集成及后续收尾待编排端执行。

未发现其余未修复功能缺陷；自检不替代独立审核。
