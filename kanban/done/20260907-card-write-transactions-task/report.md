# 卡片事务阶段交付报告

## 交付

- 任务：20260907-card-write-transactions-task。
- 任务分支：`card-write-transactions`；本地与 `origin/card-write-transactions` 均为 `f056af9b75bb981e99d6f071fc04f36988b6cef2`。
- 来源组分支：`group/20260907-review-archive-group`，最新基线 `4889d3fb93f8d2d639832b9da5588d668714d9bc`。已 fetch 并 rebase，结果为无需改写；随后重新验证。
- 工作树：`/home/dualf/works/kander/worktrees/card-write-transactions`，提交后干净。
- 本轮只交付任务分支。组分支与 develop 未由执行端更新，等待编排端接收、审核与集成。

## 验收自检

按 spec.md 中的顺序，11 项实现自检完成；Windows 原生执行缺口单独保留，不计为实机通过。

1. 固定看板、排序组、排序任务锁顺序；读共享、写独占、入口创建/迁移/恢复持看板独占锁。私有锁、版本和操作记录不随卡移动，POSIX/Windows 经 internal/fs。
2. 单调 revision 与随机操作 ID；状态/版本比较后只操作锁内定位的现存卡片。旧路径及陈旧恢复记录不会覆盖其他提交。
3. 单二进制注册 update；show --json 返回一致正文、路径、revision 与操作 ID。
4. 支持 spec/plan/report/普通附件和相对子目录；拒绝越界、正文大小写别名、reparse、受管字段和机器产物。任务语言受保护。
5. move 的 owner/result/reason/decision/duplicate-of 入口提供手工认领、完成、终止。contract-decision-file 记录明确决定及原/新冻结字段；退回 backlog 不解除冻结。
6. new/move/pick、launch/resume、notify 的 WINDOW 回写与失败回滚统一复用版本事务；dismiss 的读取复用一致快照。过期回滚保留新记录。
7. WithTransaction 提供多文件及组控制发布接口；prepared/committed schema 1 记录使 init 可明确重做。读者不自动修复，不把已知中间态当普通缺卡。
8. 小文件卡通过逻辑 spec.md 完整填写、认领和完成；其他附件仍要求目录卡。文件转目录迁移留给后续 D 卡。
9. 回归覆盖小卡迁走后 RestoreWindowText、防陈旧全文覆盖、并发 new、update/move、反向批量写入及并发记录追加；七个实际子进程 kill/restart 边界覆盖 prepared、附件目录、首文件、全部文件、rename、revision、committed。目录卡带新附件移走后也能在新位置完成恢复。
10. 发布规则要求受控写卡；guard-write 明确只是辅助检查。真实重复、版本或恢复内容冲突保留现场，不选择或删除副本。
11. AGENTS/README/事务说明/命令规则和 cn/en/ja 消息已同步；全量及相关竞态测试通过。Windows 测试程序与二进制编译通过，原生执行缺口如下。

## 验证

- `go test ./...`：全部通过；最终提交及 rebase 后复验通过。
- `go test -race ./internal/fs ./internal/board ./internal/window ./internal/launch ./internal/notify ./internal/liveness`：全部通过；最终提交及 rebase 后复验通过。
- `GOOS=windows GOARCH=amd64 go test -c`：fs、board、window、launch、notify、liveness 六包全部编译通过。
- `GOOS=windows GOARCH=amd64 go build ./cmd/kander`：完整 Windows 二进制编译通过。
- `git diff --check`：通过。
- 本轮回归已修正测试发现的受控入口兼容、受保护终止历史单节追加及目录迁移后的附件恢复问题。

## 审核与收尾

- PM/QA：等待编排端按任务组规则执行；本报告不代表角色审核通过。
- CSA/Hacker：按仓库规则 N/A。
- 分支已正常推送；未执行组集成、develop 集成或工作树/分支清理，等待编排端派回。
- 任务卡的 RESULT/FINISHED_AT 保持未完成状态。本轮完成实现交付后进入 review，不进入 done。

## 未解决项与限制

- 环境缺口：本机为 Linux，无原生 Windows/Wine。Windows LockFileEx、DACL/reparse 与恢复用例只有编译结果，没有本轮原生执行结果；后续需在 Windows 运行同一测试套件。
- 本层提供 revision/预期状态并发令牌；后续持久执行 epoch 与派回业务状态由 N 卡实现，未在本卡提前发布未来命令。
- 控制记录保留完整变更前后文本；保护遵守协议的本机命令，不防任意进程直接改文件。本轮未部署安装器产物或迁移真实看板形态。
- 当前无已知未修复的实现缺陷；仍待独立 PM/QA 角色审核。


## 首轮派回后交付

最终交付更新为 `a7fe54beb6ade00678e14655c0d385115eb950c8`，组基线更新为 `f056af9b75bb981e99d6f071fc04f36988b6cef2`。六项 finding 分别处置，修复、SHA 和最终验证见 [首轮派回处置](review-round-1.md)，完整原文见 [首轮审核](review-round-1-findings.md)。首轮审核未通过的历史结论保留，等待编排端复审。


## 最终收尾（更新此前阶段状态）

11/11 项自检完成；PM/QA 第一批第二轮增量复审通过，CSA/Hacker N/A，六项 finding 全部关闭。最终交付 `a7fe54beb6ade00678e14655c0d385115eb950c8` 无重写，已随最终组 HEAD `251f5d89186730ea372053a401136030710efe84` 进入本地和远端 develop，执行端独立 fetch/祖先核验通过。任务工作树、本地任务分支、远端任务分支均已删除并核验。完整批次、集成、残留及清理证据见 [收尾记录](wrapup.md)。之前阶段的未集成/待审核/分支保留描述是历史状态，当前由本节更新；原 finding 及作者判断不覆写。未解决项仅 Windows 原生执行环境缺口，未声称原生通过。
