任务文件已删除；worktree 保持在 `47d5740` 且干净（`git status --porcelain` 无输出）。

**审查结论汇总**：3 条 gate findings（1 high、2 medium）+ 6 条 NON-BLOCKING。

- **QA-1 (high)** — Project tab 换 Reviewer 后，`ResetReviewRoleModel` 删掉 overlay 键却把新 Reviewer 的默认模型 seed 进合并缓冲，既不重算也不登记覆盖：界面标注“全局：X”，运行期实际用的却是 scope 里为旧 Reviewer 配的模型。
- **QA-2 (medium)** — `switchTab` 没有同步 `p.app`，Global tab 的 Interface 段会显示 Project tab 上未保存的 TUI 值，继续操作还会把它写进 scope `config.json`。
- **QA-3 (medium, mechanical/dead-code)** — 新增的鼠标点击切 tab 与 chrome 命中偏移在运行期不可达（`App.Update` 在 Options 打开时把 `tea.MouseMsg` 全部转给 `Options.Update`），范围内有两个 commit 在修这条走不到的路径。

已在交付 commit 上实测：`go build ./...` 通过，`go test ./... -count=1 -timeout 300s` 全量通过（卡片自检把这些证据记在了更早的 `b2f3668` 上，见 QA-9）。行数门未触发。