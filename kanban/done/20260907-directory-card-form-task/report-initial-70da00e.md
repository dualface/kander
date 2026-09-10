# 目录卡与 SIZE 交付记录

## 交付

- 任务：20260907-directory-card-form-task。
- 分支：`directory-card-form`；工作树：`/home/dualf/works/kander/worktrees/directory-card-form`。
- 最终提交：`70da00edfff664ec1c0740a49f6ea3ae51b42c8c`，本地与远端任务分支一致，已正常推送。
- 最新组基线：`a7fe54beb6ade00678e14655c0d385115eb950c8`；fetch 后 rebase 无变化，随后再次执行全量测试通过。

## 实现与逐项自检

1. 迁移复用 S 的看板排他锁、revision、操作 ID 与 prepared/committed 日志，没有另建锁协议。有待迁移项且看板存在 working/review 时默认拒绝；`init --maintenance` 表示操作方已暂停全部执行端、外部编辑器、在途通知及归档写入。工具不证明停写，不自动终止 Agent。
2. new 始终创建 `<task-id>/spec.md`，SIZE 紧随 TYPE，默认为 small，--large 为 large。small 保留 IMPLEMENTATION/SUMMARY，large 的 done 仍要求非空 report.md；两种规模均可写普通附件。
3. Entry.Kind 与 TaskSummary.kind 表示规模，Entry.IsDirectory() 表示物理形态；list/TUI、Agent/模型参数和启动/恢复/通知 prompt 使用 SIZE。无 SIZE 文件按 small、目录按 large 读取；check 对缺 SIZE 目录提示 init，非法/重复 SIZE 阻止变更。
4. todo 的 SELF_REVIEW/CARD_REVIEW 门禁按 SIZE 与任务组关系判断；SIZE 复用 S 的冻结契约机制，退回 backlog 不解冻，明确契约决定使用原受控入口。
5. init 覆盖七状态，文件迁为同 ID 目录，既有目录只补 large；保留正文其他字节、附件及相对链接。CRLF 与旧中文 token 有回归。再次执行计数 0，内容与 mtime 不变。
6. 文件迁移顺序为持久记录、同卷暂存目录、源移走、SIZE 补写、目录发布、revision、committed；明确存在内部中间态。正常读者由锁或已提交正文快照隔离，进程中断后全量/定向读取报告受管待恢复事务，init 显式重做。
7. 七个边界均通过实际发布路径注入错误，并由子进程 kill/restart 验证恢复；源、暂存与目标的冲突或无记录产物保留报错，不猜测删除。旧文件快照在形态迁移后不能回滚复活原路径。
8. 并发测试覆盖 init/init、快照读取、update 与归档/move 对同一维护锁的等待，释放后串行成功或 revision 冲突；订阅读取旧文件并在迁移后继续运行。为消除 Scan 与后续 ReadDocument 之间的迁移竞态，增加 Board.Document 已提交正文快照，list/TUI/依赖及订阅使用该快照；写端仍校验当前 revision。
9. 旧文件保留 list/show/check/TUI/subscribe 读取，mutation 在副作用前提示 init；读取不批量迁移。新增历史 done/archived 无效 UTF-8 范围回归，确保扫描正文不扩大 check 默认范围。
10. guard-write 对目录内部路径和同状态旧 .md 拼写分别提示；保留其外部检查/写入竞态边界。卡片及附件更新统一使用 S 的 update 入口。
11. 同步英文发布规则、模板、中英日界面资源，以及中文 README、AGENTS、事务/护栏/目录迁移文档。POSIX 测试通过；Windows 测试包含 junction 与同套恢复/锁用例，已交叉编译，原生执行仍缺环境，详见下节。

## 验证

- `go test ./...`：全部通过；fetch/rebase 后再次通过。
- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./internal/fs ./internal/board ./internal/window ./internal/launch ./internal/notify ./internal/liveness ./internal/takeover`：全部通过。
- `gofmt -l`：无未格式化 Go 文件；`git diff --check`：通过。
- Windows/amd64：上述七包的测试程序与 `cmd/kander` 完整二进制均交叉编译通过。
- 当前主机为 Linux，无 Windows/Wine；原生 LockFileEx、固定句柄/DACL、junction/reparse 与 kill/restart 未在 Windows 执行，交叉编译不等于实机通过。

## 审核、边界与后续

PM/QA 由编排端在组分支安排，本执行端未触发审核；CSA/Hacker 按仓库规则 N/A。未集成组分支/develop，未清理任务分支或工作树，等待编排端派回。

已安装版本尚无 S 的 update。执行记录使用从已接收 S 基线编译的 `/tmp/kander-directory-card-record` 的 show --json/update/move 入口写入；未部署二进制，未迁移用户真实看板。所有形态迁移测试均在临时目录。

未解决项：1 项验证环境缺口——Windows 原生执行，需在 Windows 环境运行同套测试。未发现其他已知实现缺陷。保留任务分支与工作树供 PM/QA 审核及后续处理。
