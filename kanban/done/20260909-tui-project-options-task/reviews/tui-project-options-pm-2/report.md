评审完成，worktree 未被改动（仅只读检查，HEAD 仍在 `dc478b7`，`git status` 干净）。

**结论摘要**：验收项 6 项 Complete、2 项 Partial、1 项 Contradicted、1 项 Unverifiable。

门禁发现 6 条：
- **PM-1（blocking）** — 在 Project tab 保存时，`Finish()` 的 `!EditingOverlay()` 守卫使 scope 缓冲的 `welcome_complete` 保持 `false`，`SaveAllDirty` 仍会把它写进 `config.json`；`config.Effective()` 在该位为假时丢弃整份配置只留 `Rules`，用户的 launcher/agents/models/reviewers 全部静默回落默认值，而面板报告「已保存」。
- **PM-2（high）** — `overlayExisting` 基线在 `review_stages` 归一化之后才拍摄，与未归一化的读盘内容永不相等；使用扁平 `review_stages` 的项目在 Project tab 每次保存都被误判为并发冲突。
- **PM-3 / PM-4 / PM-5（medium）** — 锁文件裸 `os.Remove` 破坏互斥并可伪报失败；Project tab 清空 `agents.path` 被当成恢复继承导致编辑丢失；关闭确认态 chrome 行数与 `tabHits` 未同步，鼠标可错行并顶掉确认框。
- **PM-6（medium [mechanical]）** — `overlayNotice` 注释与实际生产用途不符。

另有 4 条 NON-BLOCKING（恢复继承后焦点丢失、`single` 覆盖遮蔽 `columns` 恢复行、顶层 `kanban_agent` 未同步、自检记录停留在 `b2f3668` 未覆盖交付 commit）。