# 首轮派回处置

本轮复核对象为首批 S：审核 base `4889d3fb93f8d2d639832b9da5588d668714d9bc`，原交付 `f056af9b75bb981e99d6f071fc04f36988b6cef2`。PM/QA 工具均退出 0、语义未通过。完整原报告保存在 [review-round-1-findings.md](review-round-1-findings.md)，包含需求表、所有 finding 和 NON-BLOCKING: none；没有用摘要替代原报告。

## 独立复核与逐项结论

- PM-001：接受。独立追踪 operationRecords、ListDirectory 和 Windows 原子发布句柄，确认隐藏临时文件过滤晚于打开文件；不同任务的共享看板锁不能保护日志枚举。增加稳定 journal 共享读／独占发布锁，作为看板、排序组、排序任务之后的末端锁，覆盖临时文件创建至读写句柄关闭。修复提交 `db9aac6e821c5952f39f5c9903264f53f9d74414`。
- PM-002：接受。NotifyViaResume 注释无条件恢复的承诺与 managedMutation 的 revision/状态检查不符。明确只在本操作仍持有当前 revision 时恢复，否则保留新记录并报告冲突。修复提交 `a7fe54beb6ade00678e14655c0d385115eb950c8`。
- QA-01：接受。独立追踪 journal 读取句柄不共享 DELETE 与 committed 原子替换路径，确认同一并发缺口可能让正常事务留下 prepared。与 PM-001 共用锁修复，但此项单独保留结论。修复提交 `db9aac6e821c5952f39f5c9903264f53f9d74414`。
- QA-02：接受。本次 S 引入的 ReadDocument revision 冲突会被原 groupMembers 的 continue 吞掉；固定 monitored 集合后永久漏掉成员属于本次新增回归。改为读取错误即返回 nil 和错误，禁止部分成功。测试在 Scan 后真实 update 再展开，要求显式失败；重新扫描后两名成员完整返回。只修消费错误传播，不实现 E 的后续完整订阅重构。修复提交 `76bd83fc5dd6e21fa01e93d08a36286dedf7f37a`。
- QA-03：接受行数事实并按派回要求修复。独立核对 base 为 1030 行、首交付为 1035 行；本轮未在已加载规则文本找到所述 1000 行门禁原句，不将该规则来源声称为已验证。将原回滚测试完整移到 rollback_test.go，launch_test.go 现为 989 行，新文件 54 行，测试覆盖保留。修复提交 `a7fe54beb6ade00678e14655c0d385115eb950c8`。
- QA-04：接受。与 PM-002 同一注释偏差，独立保留此角色处置；静态对照 CAS 失败路径与新版注释一致。修复提交 `a7fe54beb6ade00678e14655c0d385115eb950c8`。

没有拒绝项。Windows 共享模式的故障因果为静态确认，不声称原生实测复现。

## 验证与交付

- 三次提交均已逐次正常推送 origin/card-write-transactions。
- fetch 后 rebase 最新 origin/group/20260907-review-archive-group，结果已是最新，无冲突，无历史重写。
- 最新组基线：`f056af9b75bb981e99d6f071fc04f36988b6cef2`。
- 最新最终交付 SHA：`a7fe54beb6ade00678e14655c0d385115eb950c8`，本地与 origin/card-write-transactions 一致。
- rebase 后 `go test ./...` 全部通过。
- rebase 后 `go test -race ./internal/fs ./internal/board ./internal/window ./internal/launch ./internal/notify ./internal/takeover ./internal/liveness` 全部通过。
- 新增双进程同步回归在 Linux 通过：持 journal 读句柄时 committed 发布等待；持原子发布临时句柄时其他任务读取等待。解除同步后双方成功，日志无遗留 prepared，更新正文/revision 正确。Windows 专项夹具使用 DELETE 权限句柄复现真实共享约束。
- fs、board、window、launch、notify、takeover、liveness 七包 windows/amd64 测试程序及完整二进制交叉编译通过。当前 Linux 主机没有原生 Windows/Wine，新增双进程用例及 LockFileEx/reparse/恢复仍未原生执行；编译通过不能替代原生验证。
- `git diff --check` 通过，任务工作树干净。

本轮只修复、验证和交付任务分支。PM/QA 复审由编排端处理；CSA/Hacker N/A。没有触发审核、更新组分支或集成 develop。卡记录继续遵循当前已安装旧版命令协议：show 重定位、guard-write 成功后打开现存正文/报告；本轮没有部署新二进制。
