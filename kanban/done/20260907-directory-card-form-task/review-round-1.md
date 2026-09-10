# 第二批首轮派回：执行作者处置

以下前半部分保留首次派回处置历史；最新判断以末尾“实际授权后的修复交付”为准。

作者：本卡执行 Agent codex。以下为执行作者独立验证结论，不替代 Reviewer 复审。

## 原始 finding

完整原文见 [PM/QA 首轮派回原文](review-round-1-findings.md)。审核 base：`a7fe54beb6ade00678e14655c0d385115eb950c8`，审核 commit：`70da00edfff664ec1c0740a49f6ea3ae51b42c8c`。PM/QA 均不通过，CSA/Hacker N/A。

## PM-001 — medium

判断：有效，已复现。旧发布路径在 migration-source 后已移走源，若 fs.WriteTextAtomic 留下 `.spec.md.<pid>.<time>.tmp`，两次 init 均因未登记临时文件失败，原卡入口仍缺失；临时文件与原文均保留。原 kill 用例只覆盖外层 checkpoint，不能证明内部写入窗口恢复。

修复：已改为日志绑定的 `rewrite` 与 `original` 名称。临时替换文件按 After 的已写前缀恢复追加、同步；原文先改名为受管备份，完整替换发布后才删除匹配备份；读写继续使用既有排他锁及 internal/fs。只恢复本操作登记产物，未知文件和非前缀内容保留报错。增加创建临时文件、同步完成、原文备份、替换发布和备份删除各边界的失败与 kill/restart。

执行作者处置：已修复并自测，待编排端安排 Reviewer 复核；不替代审核关闭。修复提交：`29ada01a241bb7a7734972a913e22f3dad52756c`，已正常推送。新增 5 个写入内部边界，合计 12 个失败及真实子进程 kill/restart 边界全部通过；另覆盖有效部分内容续写、非前缀内容保留、旧 schema 确定性名称恢复、未知临时产物保留。

验证：`go test ./...`、`go build ./...`、`go vet ./...`、board/fs 两包 race、gofmt 与 diff 检查通过。Windows/amd64 board 测试程序及完整二进制交叉编译通过；原生 Windows 未执行。fetch 后组基线为 `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`，rebase 无变化，其后全量测试再次通过。任务分支本地/远端 SHA 一致，工作树干净。

## PM-002 — medium

判断：有效，实际迁移复现。旧 `backlog/<id>.md` 中 `[参考](../done/20260907-reference-task/spec.md)` 原本指向 `done/20260907-reference-task/spec.md`；迁移后相同地址指向不存在的 `backlog/done/20260907-reference-task/spec.md`。测试建立真实参考文件，验证目标变化及失效。

处置：待用户契约决定。原验收同时要求“正文只补 SIZE”及“相对链接保留”，不能自行选取链接重写或拒绝迁移策略。编排端已提问但尚未传达实际答复；不把默认选项视为授权。本轮仅验证并记录，未改写冻结字段或链接。

## QA-001 — medium

判断：有效，与 PM-002 同一根因，独立保留本引用。旧 `backlog/<id>.md` 中 `[说明](../../README.md)` 迁移前指向临时项目 README.md，迁移后改指不存在的 kanban/README.md。已用实际迁移确认，原测试只比链接字符串没有验证目标。

处置：与 PM-002 一起等待实际用户决定；未声明已修复或验收通过。

## 当前状态

卡片 working；PM-001 独立修复已交付，PM-002/QA-001 等待编排端传达实际用户决定后继续。整体验收自检 9/11：第 5 条链接目标未通过，第 11 条 Windows 原生验证未完成；不继承原报告的整体通过结论。Windows 原生验证缺环境仍保留。组分支/develop 未修改，任务工作树与分支保留。不触发审核。

定向 `kander check 20260907-directory-card-form-task` 通过，存活 alive。原文保留于 review-round-1-findings.md，先前 report.md 为原交付快照，以上处置纠正其链接保留及无已知缺陷声明。

## 实际授权后的修复交付

用户原话：“就是 SIZE 要改, 相对链接也要改”。来源：编排端通知 `/tmp/kander-notify-c9b8429b41ee0c909d44a9f3bc89328e/message.txt`，2026-09-07。受控 update + contract-decision-file 已将原/新冻结契约及授权持久记录在 spec.md 的 CONTRACT_DECISIONS。此次授权解除此前等待；旧原文和判断不删除。

- PM-001 — medium：作者确认有效，修复 `29ada01a241bb7a7734972a913e22f3dad52756c` 保留在最终交付中。12 个原失败/kill 边界回归通过；整批链接迁移进一步覆盖 14 个边界。未知产物、非前缀内容仍保留报错，部分写入及旧 schema 确定性临时名称可恢复。作者结论：已修复并自测，待 Reviewer 重新审核，不替代审核关闭。
- PM-002 — medium：作者确认有效，修复 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`。按引用源与目标的旧/新映射重定位；真实目录目标、双方旧卡同时迁移、既有目录 spec 和 report 反向引用、图片、查询/片段均验证目标正确。整批在同一日志持久登记，恢复不依赖半迁移现场重建映射。作者结论：已修复并自测，待新契约审核。
- QA-001 — medium：与 PM-002 同根因，保留独立引用。真实临时项目 README 测试验证 `../../README.md` 经迁移调整后仍指向同一项目文件；14 个失败/kill/restart 边界与第二张卡发布后恢复均验证真实目标及内容/mtime 幂等。作者结论：已修复并自测，待新契约审核。

验证：go test ./...、go build ./...、go vet ./...、board/fs/liveness race、格式及差异检查通过。Windows board 测试程序和完整二进制交叉编译通过，新增路径大小写覆盖，但未在 Windows 原生执行。最终提交 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef` 已正常推送，最新组基线 `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`；rebase 无变化，其后全量测试再次通过。

当前阻塞链接策略已解除。整体验收自检 10/11，第 11 条仅 Windows 原生执行仍为环境缺口；不把该项记为全部通过。PM/QA 将由编排端按新完整契约重启首轮，CSA/Hacker N/A；执行端未触发审核、修改组分支或集成 develop。
