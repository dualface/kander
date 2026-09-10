**审阅结论**: 三项 Review focus 的核心契约全部落实，未发现 blocking / high 问题。

- **1 条 gate 发现** (QA-001, medium [mechanical])：`version.go:6-7` 与 `AGENTS.md:29` 声称注入的是「语义版本」，但三个构建入口里只有 `release.yml` 如此；`Makefile` / `make-windows.cmd` 注入的是 `git describe` 串（本仓库实测 `v20260908T154432Z-7466cd6acfd3-20-g5172eda`）。属文案与实现不符，改文案即可。
- **3 条 NON-BLOCKING**：`on:` 收窄后仓库已无分支/PR 上的 vet+test（QA-002）；`--always` 在守卫后的分支里永远不生效（QA-003）；本地与发布产物的 `v` 前缀不一致（QA-004）。

全程只读检查，未执行 `make` / `go test` 以免弄脏工作树；工作树与 HEAD 保持干净。任务文件已删除。