审查完成，任务文件已删除。

**结论摘要**：核心持久化语义（sparse 覆盖写入、scope 隔离、KANDER_CONFIG 处理、非 Git 向上查找、三语文案、文档更新、行数门）均通过。发现 7 项 gate findings：

- **high** — QA-1 跨 tab 未保存编辑无法落盘，「保存并关闭」只保存当前 tab 却报告成功（编辑静默丢失）
- **high** — QA-2 恢复 `tui.*` 继承后，面板把刚删掉的覆盖值当作「全局：值」展示，界面也不回退
- **medium** — QA-3 扁平 `review_stages` 覆盖下「恢复继承」静默失效
- **medium** — QA-4 项目覆盖保存在用户仓库根留下永久 `.kander-config.json.lock`
- **medium [mechanical]** — QA-5 死代码 `RestoreInheritLabel` + `tui.restore_inherit`；QA-6 `overlayNotice` 注释与实现不符 + 遗弃的 `tui.overlay_notice`；QA-7 断言不了自身命名行为的冗余测试

另有 4 项 NON-BLOCKING（焦点恢复、Global tab 路径展示、confirming 态鼠标切 tab、测试冗余嵌套）。工作树保持干净，未执行任何写操作，`go test` 未重跑（采纳卡片记录的交付自检结果）。