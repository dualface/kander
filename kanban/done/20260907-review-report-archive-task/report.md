本卡完成收尾。最终交付 `8241a49b1f50cbb99acc68b4c3dc58b5a803253e` 已进入 develop（组 HEAD `251f5d89186730ea372053a401136030710efe84`）；完整验收、适用审核、清理及四项保留事实见 [完成报告](wrapup-report.md)。以下按原样保留之前各轮的历史判断。

增量非阻断处置更新：最新交付 `8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；本轮只改发布规则一处措辞，运行时代码无变化。QA-09 保留、QA-10 文档修正，详见 [增量处置](review-round-2.md)。完整历史记录继续保留。

本报告保留初次交付历史。首轮审核后最新交付为 `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`；15 项作者处置、基线及验证见 [首轮审核处置](review-round-1.md)。PM007 范围外保留，Windows 原生缺口保留，等待编排端复审。

# 审核归档交付报告

## 交付

- 最终交付：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`；本地与远端 `review-report-archive` 一致。
- 来源组基线：`839f72119b22ce8b48811fc9819de9643a4028f8`；fetch 后 rebase 无变更，随后 `go test ./...` 再次通过。
- 工作树：`/home/dualf/works/kander/worktrees/review-report-archive`。
- 新增 --task/run/batch/前驱/requirements/advance 参数；无 --task 路径不定位看板。
- board 负责共享 schema、输入意图、受控暂存、原件、逐卡清单、索引和完整性校验；review 负责 Git 范围、Reviewer 生命周期和原始输出。board 不依赖 review。
- 未更新组分支，未集成 develop，未部署新二进制，未迁移真实看板。

## 验收自检

12/12 项实现自检满足；Windows 原生验证缺口单列，不把交叉编译记为原生通过。

| 项 | 结论与证据 |
| --- | --- |
| 1 参数 | 保留位置参数；选项在 CWD 前，重复任务 ID 去重；run 可随机生成，batch 必须显式保存。 |
| 2 前置卡验证 | 从目标 CWD 定位主看板；验证 working/review 目录卡、成员及语言；启动前拒绝无效卡；独立路径回归通过。 |
| 3 身份与批次 | 同 run 输入冲突拒绝；重试不重跑；成员/base/要求/任务上下文哈希固定；CAS 推进记录完整 Git 范围逐提交归属；前驱显式关联。 |
| 4 原件和 sidecar | 原始输出、日志、上下文、提示和报告分别保存；SHA-256、版本、身份、语言、耗时、执行失败原因齐备；执行 ok 不代表 PASS。 |
| 5 生命周期证据 | 意图先落盘，输出进入受控 staging；回收、worktree 检查和 runtime 清理后才 finalization；超时、非法 UTF-8、残留子进程、清理超限均有回归。 |
| 6 失败与 stdout | 启动前错误无虚假运行；启动失败记 not_started；失败原输出可从 stdout/归档读取；发布错误独立非零。 |
| 7 并发与迁移 | 长运行只持 run 执行锁；发布复用 S，锁内按 ID 重定位；两个角色、同 run 重复发布、move/update 竞争回归通过。 |
| 8 每卡证据和索引 | 每卡完整不可覆盖原件及 manifest；正文只追加机器 JSON 索引；board 提供共用类型和解析接口。 |
| 9 部分发布和崩溃 | 逐卡原子；保留成功卡，重试只补缺项。七个实际子进程 kill/restart 边界通过；S 共用多文件日志恢复回归仍通过。 |
| 10 check | 同时读取意图和索引，零卡成功也报告未完成；校验清单、哈希、语言、成员、前驱；前驱原件篡改不能通过完整性接口。 |
| 11 语言与 fs | 卡 LANGUAGE 优先，缺失回落冻结；后续角色和重试不随配置漂移；binary 原件经 S 日志 base64 无损恢复，写入全部经 internal/fs。 |
| 12 文档和验证 | 四份发布规则、README、AGENTS 文档索引、事务文档和新 schema 文档同步；新输出有 cn/en/ja 资源；全量/race/build/vet/格式检查通过。 |

## 验证

- `go test ./...`：通过；成功 fetch/rebase 后再次通过。
- `go test -race ./internal/board ./internal/review ./internal/fs`：通过；最终 Reviewer 输出修订后单独再次运行 review race，通过。
- `go build ./...`、`go vet ./...`、修改 Go 文件的 `gofmt -l`、`git diff --check`：通过。
- Windows amd64：board/review/fs 测试程序及完整 kander 编译通过；最终 Reviewer 修订后重新编译 review 测试程序与二进制，通过。
- 新增真实 kill/restart 边界：intent、prompt、launching、output、finalized、first-card、published。执行锁在 kill 后可重新取得；未 finalized 的运行恢复为 interrupted，不造最终报告。
- 修复开发期回归：旧文件卡 check 误报及无效正文重复诊断；Codex 非法 UTF-8 原先被记为有效文本，新增失败回归后改为仅保存 raw 证据。
- Git 同步曾遇到一次 `GnuTLS, handshake failed: The TLS connection was non-properly terminated.`；重试 fetch 成功，随后 rebase 无变化并复验。没有遗留同步阻塞。

## 接口与边界说明

- 新 batch 的 requirements JSON 必须列出四角色 required 或带理由的 N/A；调用方按项目/用户规则解析要求。
- --advance-file 对完整 old..new 范围逐提交映射到本批成员；业务归属由调用方真实提供，工具验证范围/成员/CAS，不声称从代码证明业务归属。
- 复审消费者使用 `board.ReviewPublicationComplete` 校验当前及前驱完整原件，但仍须独立判断报告语义和角色结论。finding/disposition/required 角色完成门禁归后续 R。
- 相同 run 恢复会保留未确认运行的原输出，标记 interrupted；不依据旧 PID 擅自回收未知进程或删除未知 runtime，sidecar 明确回收/清理未确认。
- 原 spec 路径移动或正文变化时，可传归档 task-context.md 的绝对路径恢复相同输入；比较的是字节哈希。
- 证据和本卡记录均在本机 kanban，不进 Git。

## 审核与收尾

PM/QA：执行端未触发，等待编排端接收并安排组审核。CSA/Hacker：按仓库规则 N/A。
分支和工作树保留，等待组审核、集成和派回；RESULT 保持空白。

## 未解决事项

1. Windows 原生环境缺失；主机为 Linux，未安装 Wine。LockFileEx、DACL/reparse、归档和进程行为只有交叉编译及跨平台测试代码，尚未在原生 Windows 执行。
2. PM/QA 组审核尚待编排端执行，这是本轮 review 交付的正常后续步骤。

自检未发现其他未修复功能缺陷。Reviewer 接口测试使用隔离假 CLI，不冒称真实 Reviewer 审核通过。
