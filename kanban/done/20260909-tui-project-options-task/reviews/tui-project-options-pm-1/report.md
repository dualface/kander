审查完成：worktree 保持干净，HEAD 仍在 `b2f3668`，任务文件已删除。

**结论**：核心目标已实现（tabs 按安装模式、继承前缀、稀疏覆盖写入、恢复继承、文档与三语文案同步），11 条原子需求中 Complete 5 / Partial 6。7 条 gate findings：1 条 high（Options 鼠标点击定位因新增 3–5 行作用域抬头整体错位，必现且影响根菜单与各 section）、3 条 medium 逻辑缺陷（Global tab 清空 agent 路径会删项目覆盖键、SyncTUI 短路导致 scope 保存假冲突、跨 tab dirty 与单 tab 保存不匹配）、3 条 medium [mechanical]（恒真断言、孤儿 i18n 键、失真注释）。NON-BLOCKING 4 项。