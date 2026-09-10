Role: QA  
Commit: `c2e711896ad71946ab09b50ed05eb9ce01491113`  
Task Context: 事务日志分区、旧布局迁移、已提交记录保留。  
Reviewed Scope: 已完整读取任务说明与规格，核查指定范围及相关恢复、文件系统、TUI 消费路径。环境只读，未重跑测试；采信交付提交记录的全量测试、race 与 Windows 交叉编译结果。

| 行为／质量 | 结论与证据 |
|---|---|
| 分区发布、恢复顺序 | Observed：prepared 写 pending；版本完成后改写 phase、移入 committed；恢复跳过 committed 镜像。 |
| 读盘与结构校验 | Observed：正常读取跳过 committed 内容；仍检查名称、类型、重复 ID、reparse。测试覆盖无效 JSON、大文件及旧 prepared。 |
| 分拣与保留 | Observed：逐条改名保留字节；保留最近 100 条及迁移暂存对应记录；清理失败不改变提交结果。 |
| 并发与中断 | Observed：沿用终端 journal 锁；测试覆盖分拣、归档前、清理期间 kill/restart 与进程读者互斥。 |
| 架构与可维护性 | Observed：日志职责留在 board，文件操作复用 internal/fs；变更文件均未超过 1000 行。告警输出存在下述边界回归。 |
| Windows | Observed：安全句柄路径及 junction 测试存在；Unverifiable：Windows 原生运行结果。 |

**FINDINGS**

**QA-001 — medium — Inferred，高置信度：日志告警直写 stderr，绕过 TUI 结果通道。**

- **证据：** `internal/board/journal_layout.go:82` 每次发现旧记录直接打印；`internal/board/journal_retention.go:18` 直接打印清理失败。TUI 定时刷新经 `internal/tui/program.go:124`、`internal/board/payload.go:100` 到达读取路径；后台启动经 `internal/tui/start.go:38`、`:45` 到达读取及提交路径。
- **触发与影响：** 升级后尚未 init 的旧看板，每次 TUI 刷新都会向终端直接写提示；后台启动遇到清理失败也会直接输出。输出绕过 Bubble Tea 渲染，可能扰乱画面，且告警未进入启动结果展示。这违反 `AGENTS.md:70` 的明确约束及 `internal/launch/start.go:18` 的无打印接口契约。
- **最小修复：** board 返回结构化告警或接受调用级告警接收器；CLI 负责 stderr，TUI 经消息／`pendingWork` 展示。增加集成断言：旧布局读取、清理失败时，TUI 调用不写 stdout/stderr，同时保留可见告警与成功提交结果。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件遗留不影响审核结果。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred，高置信度：新增日志提示与清理告警在 board 内直接写 stderr。旧布局看板的 TUI 定时刷新反复触发提示；后台启动遇到清理失败也直接输出，绕过 Bubble Tea 与启动结果告警通道，可能扰乱终端画面。违反后台启动告警不得直接写 stdout/stderr 的既有契约。最小修复：通过结构化结果或调用级接收器传递告警，由 CLI 输出 stderr、TUI 经消息或 pendingWork 展示；补充无直接输出且告警可见、提交成功的集成断言。",
      "evidence": "internal/board/journal_layout.go:82、internal/board/journal_retention.go:18 直接调用 os.Stderr.WriteString；internal/tui/program.go:124、internal/tui/payload.go:8、internal/board/payload.go:100 构成定时刷新至 Scan 的消费链；internal/tui/start.go:38、:45 调用 board 读取与提交；internal/tui/start.go:118 消费 StartResult.Warnings 展示告警；AGENTS.md:70 明确要求后台启动及警告通过 pendingWork 回传、不直接写 stdout/stderr；internal/launch/start.go:18 声明 Start 不打印且展示由调用方负责。"
    }
  ],
  "NON_BLOCKING": []
}
```