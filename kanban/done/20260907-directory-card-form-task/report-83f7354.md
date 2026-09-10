# D 新契约首轮修复交付

最终提交：`83f73544ef1f612a97f99d384e9386eeecb78752`；前一修复：`6d447150f78e098067d5d13f102f5407a8bd87f6`。任务分支 `directory-card-form` 本地/远端一致，均已正常推送。最新组基线 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`；fetch/rebase 无变化，随后全量测试通过。工作树 `/home/dualf/works/kander/worktrees/directory-card-form` 干净并保留。

## 原文与作者处置

[本轮 PM/QA 完整原文](review-new-contract-findings.md)；[所有 gate 与非阻断项的逐项作者判断、证据及修复提交](review-new-contract-disposition.md)。逐角色保留共享问题引用；QA 五条无 ID 非阻断项按原顺序记录。PM-106/109 的建议未采用，理由与调用链证据已记录，未冒充已修复。

[上一交付报告 d93f0b6](report-d93f0b6.md)、[原始交付 70da00e](report-initial-70da00e.md)、[早期原文](review-round-1-findings.md) 与 [早期处置](review-round-1.md) 全部保留。旧审核结论不继承为本次通过。

## 本次变化

- 无迁移时普通非卡片散落文件只警告并提示 check；需要迁移时仍拒绝结构问题。缺 spec、真实重复、未知迁移产物、reparse 和冲突数据保持拒绝与现场保留。
- 恢复实际先于结构扫描；有效 prepared 事务可先 committed，随后再报无关结构问题。MigrateCards 与兼容入口 RecoverTransactions 共用同一恢复核心；读命令不修复。
- 不移动且目标不变的历史 Markdown 原文/mtime 保留，不再被无关反斜线路径、无效旧 URL、Wiki 散文或绝对 srcset 阻断。需要改写的不支持语法仍预检拒绝。
- reviews/dispatches 等生产者子树不作普通迁移写入；受控更新与迁移共用受管路径判据，回放也拒绝这些路径。原审核/派发证据保持原文，不实现未来 A 功能。
- Kind 在包内结构扫描留空，公开快照 attachSize 后才表示规模；check 汇总不可读文档错误一次；同状态旧文件的目录内路径提示 init；无 TYPE 的旧卡保留 H1 在前；清除重复赋值/包含校验并整理直接依赖块。

## 11 条自检

1. 通过：S 排他维护锁、默认活动卡拒绝、停写确认与不自动终止 Agent 保留。
2. 通过：new 统一目录、SIZE 默认 small/large 及完成要求保持；无 TYPE 旧卡不补造 TYPE，H1 不再后移。
3. 通过：SIZE 消费链、回退和非法值拒绝保持；结构扫描不再伪装规模。
4. 通过：todo 审卡与 SIZE 冻结保持，未修改用户冻结契约。
5. 通过：七状态、SIZE 与必要链接调整、正文/附件保持、真实目标与幂等回归通过；不相关历史文档不改写，生产者原件排除于普通迁移。
6. 通过：持久映射/同卷暂存/恢复及中间态诊断保持；共用恢复核心，已验证恢复先于无关结构错误。
7. 通过：原 14 个失败与 kill/restart 边界、部分写入及异常产物保留回归通过；未放松重复、冲突与 reparse 边界。
8. 通过：并发、订阅、旧 schema、缺 SIZE/中文 token 回归保持。
9. 通过：旧文件只读与副作用前拒绝保持；check 默认历史状态范围不扩张，聚合诊断恢复。
10. 通过：guard-write 同状态与跨状态分别提示，受控写入口与外部竞态边界保持。
11. 部分：规则/AGENTS/中文说明/三语诊断同步；全量测试、build/vet/格式通过。Windows 只完成交叉编译，原生执行缺环境。

自检 10/11 完整通过；第 11 条不记为完全通过。

## 验证与边界

`go test ./...`、`go build ./...`、`go vet ./...`、`go test -race ./internal/board ./internal/fs ./internal/liveness`、gofmt、git diff --check 均通过；rebase 后全量测试再次通过。Windows/amd64 board 测试程序及完整二进制交叉编译通过，Linux 环境无 Windows/Wine，不视作原生验证。

PM/QA 本轮 exit=0、语义未通过；作者现已修复并自测，待编排端按同契约增量复审，CSA/Hacker N/A。未触发审核、未修改组分支/develop、未清理分支/工作树。真实看板未迁移、二进制未部署；记录使用已有受控 S 入口。

剩余验证缺口 1 项：Windows 原生测试，需在 Windows 执行同套句柄/锁/junction/大小写/恢复测试。PM-106 的零选项兼容包装、PM-109 的 umask 叶模式保留为已处置可选建议，无待修 must-fix；不是审核通过声明。
