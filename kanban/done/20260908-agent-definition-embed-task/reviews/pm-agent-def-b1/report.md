审核完成。任务文件已删除;被审 worktree 的 HEAD 未移动、工作区仍为干净状态。

**结论**:39/43 条原子需求 Complete,3 条 Partial,1 条 Unverifiable(`plan.md` 属看板附件,不在 COMMIT TREE 内),无 Missing/Contradicted。三条交付的集成接缝(审核模板消费 `internal/process` 原语、launch/config/menu 消费嵌入 JSON、空 argv 占位符省略)均已闭环,内置四 agent 的 start/resume/review argv、模型默认、安装器写入内容与改动前一致,`go build` / `go vet` / `go test` / `GOOS=windows go build` 实跑全绿。

四条门禁发现均为 medium,集中在:pane 投递失败在 durable 路径绕过统一回滚(PM-1)、非内置名规则入口目标退化为空(PM-2)、嵌入定义 `process_name` 字段从不被读取(PM-3)、两条 i18n 键变成死条目(PM-4)。