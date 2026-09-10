**审查结论**：5 项门禁发现（3 high、2 medium），6 项非阻塞项。

最需要优先处理的三条：
1. **QA-1**：项目覆盖文件里若是旧的扁平 `review_stages` 写法，Project tab 会**永久无法保存**，还报成“配置被他人修改”——基线在归一化之后才克隆（`internal/menu/session_overlay.go:87-89`）。
2. **QA-2**：`withConfigLock` 解锁后 `os.Remove` 掉 `.lock`（`internal/config/loadsave.go:218`），破坏的是**所有**配置写入（`Save`/`Update`/doctor 修复/overlay）的跨进程互斥，Windows 上还会把成功的保存报成失败。
3. **QA-4**：关闭确认框里多画了一行 notice 但 `chromeLines` 仍为 0，双击“保存并关闭”会落到“丢弃并关闭”并立即执行，丢掉本次全部未保存编辑。

另需注意：卡片记录的 `go test ./...` 证据落在 `b2f3668`，不是被审的交付提交 `dc478b7`，而该提交恰好改了保存链、鼠标命中与恢复继承逻辑；本次审查为只读，未执行测试（见 QA-10）。任务文件已删除。