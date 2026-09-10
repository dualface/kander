Role: QA  
Commit: `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`  
Task Context: `20260907-probe-batch-budget-task`；审核范围 `7e6fe20..021ae63f`。  
Reviewed Scope: 已完整读取任务指令与规格，核对批量入口、分类器、反查、进程回收、check、subscribe、notify 边界及测试文档。330 个提交树文件哈希均匹配。环境只读，未重跑测试；采纳调用方记录的最终提交全量、race、build/vet、格式与 Windows 交叉验证结果。

| 行为／质量 | 结论与证据 |
|---|---|
| 总预算与并发 | Observed：`internal/liveness/batch.go:35` 创建共享 deadline；固定 worker，输入同序输出，返回前 `Wait`。默认 10 秒、并发 4，较早父期限优先。 |
| 取消与回收 | Inferred：批量沿用单卡 context，前向、反查及复查不重置预算；`internal/probe/run.go:48` 等待进程、管道读取及取消回调退出。耗尽后查询入口拒绝继续执行。 |
| 身份与新鲜度 | Observed：`internal/liveness/classify.go:231` 记录结束时间、请求身份及有效性；`observation.go:28` 拒绝失败、未采集和身份变化结果。年龄由消费者判断。 |
| 运行状态 | Observed：herdr 四种运行状态独立记录；tmux 保持 `unknown`；反查保留 `NewWindow`。未将存活升级为 ready 或业务进展。 |
| check 与消费者 | Observed：`internal/liveness/check.go:63` 接入批量，保留排序及结构退出码；subscribe 仍显式投影原事件字段，notify 保持独立就绪检查。 |
| 测试与维护性 | Observed：新增测试覆盖混合快慢任务、并发、取消回收、父期限、身份变化、三语和 check 接线；夹具使用临时目录。6 个改动 Go 文件均低于 1000 行，未发现新增冗余测试或架构偏移。 |
| 文档与平台边界 | Observed：README、期限文档、索引、英文协议及三语消息与实现一致。Unverifiable：原生 Windows、真实 tmux/herdr/Agent；已披露，不单独构成门禁。 |

FINDINGS: none。未发现本范围引入、恶化或掩盖的门禁缺陷。

NON-BLOCKING: none

任务文件未删除：当前环境仅允许只读操作。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```