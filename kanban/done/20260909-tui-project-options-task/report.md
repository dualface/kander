# Project Options Delivery Report

## 交付

Options 根据可执行文件安装模式显示 Global / Project 两个 tab 或仅 Project。Project 的保存目标始终为实际项目根目录的 `.kander-config.json`；界面显示基础配置、项目和覆盖文件的实际路径。字段按原始键是否存在显示继承来源，支持稀疏覆盖、恢复继承、未保存 tab 缓冲及保存失败后继续编辑。

最终交付提交：`ead0a736302a273b5ad2489fab6e1d22265b5d24`。集成基础：`98a0a77a61d8ad561566cb344d918f1db0d48637`。任务分支：`tui-project-options`；目标分支：`develop`。

## 验收映射（10/10）

1. 安装模式决定入口，工作目录不代替二进制安装模式。`TestOptionsTargetsByInstallMode`、`TestOptionsTabsFollowInstallMode` 通过。
2. 两种安装的 Project 都只写覆盖文件，Global 保持 scope 写入。`TestProjectInstallSaveDoesNotWriteScopeFile`、`TestProjectTabSaveWritesOverlayOnly` 通过。
3. 实际路径覆盖 Git 子目录、关联 worktree 和首次创建。`TestResolveOverlayLocationGitWorktreeAndNonGit`、稀疏创建测试及 PTY 路径测试通过。
4. 来源按键存在性判断，保留 false、空值及等于基础的显式覆盖。`TestOverlayHasTreatsFalseEmptyAndEqualAsOverride`、两种安装前缀测试通过。
5. 字段编辑及恢复不影响兄弟字段，继承跟随最新 Global。规则绑定、Global 全部 TUI 字段同步、无效草稿恢复及最新基础重建回归通过。另验证语言派生不误建覆盖、Global Reviewer 重置同步 Project。
6. tab 保留独立未保存编辑，浏览及进入模型设置不产生覆盖。输入对象复用保持连续输入光标，失败模型草稿在重建后保持可见并可修正；tab、取消、无保存及恢复后预览基线回归通过。
7. 稀疏保存复用有效配置合并校验及 `internal/fs`；失败保留输入并明确报错。保存冲突、无效合并、隔离及失败草稿测试通过。
8. 非 Git 向上查找及无覆盖目标明确；`KANDER_CONFIG` 显示实际基础来源，不改变覆盖写入目标。对应路径和保存测试通过。
9. 保持 Bubble Tea / Huh 与单向 menu.Session 复用，文案已维护中英日。更新配置文档及 released rules 的旧 scope-only 描述；README 未修改。
10. 全部测试使用临时配置、看板和目录。真实 PTY 覆盖路径、来源前缀、Global/Project 切换及 48 列缩放。未将用户 HOME 配置或真实看板作为测试数据。

## 验证与自检

- 最终生产代码执行 `go test ./... -json -count=1 -timeout 240s`：1413 项通过、1 项跳过，21 个包通过。唯一跳过为非 Windows 环境下的 `internal/launch TestWindowsConsoleLauncher`；本任务的配置安全路径沿用现有后端，无 Windows 专属实现改动。
- 集成初版 `cc4349813a01b125c867195317f72db94e1257f3` 执行配置/菜单/TUI 复测：427 项通过，3 个包通过。随后新增 3 项回归并修复，最终文件树全部纳入前述 1413 项整仓验证。
- `git diff --check` 无输出；无新增超限文件，核对死代码、注释及测试重复。上游异步重载与任务稀疏写入按功能点协调，补充 Project 语言和恢复/tab 预览基线的集成回归。

## 审核闭环

原批次 `tui-project-options-batch` 在 `be375dc63da93461c624da420dbf8be127c64015` 正式关闭。Codex PM-9、QA-8 的 FINDINGS 与 NON_BLOCKING 均为空。此前四类未关闭问题及后续 PM-008/QA-008、PM-009 均已独立复现、修复并增量确认，作者处置链和原始报告完整保留。最初六次输出围栏失败由后续成功运行消解，未冒充通过。CSA、Hacker 按本仓库例外始终 N/A。

因 rebase 解决实质代码冲突，在新基础上另行执行一次集成补审。首轮 PM-1/PM-2 与 QA-1/QA-2/QA-3 归并为三项 medium，均已独立复现并修复于最终提交。Codex PM、QA 增量复核均确认所有项 closed，FINDINGS 与 NON_BLOCKING 均为空。报告见 [PM 首轮](integration-review/pm-1.md)、[QA 首轮](integration-review/qa-1.md)、[处置](integration-review/dispositions-1.md)、[PM 最终](integration-review/pm-2.md)、[QA 最终](integration-review/qa-2.md)。补审不修改已关闭批次的历史原件。

审核角色在只读环境中未重跑测试；测试与 PTY 由执行方实际运行，证据见 [验证记录](integration-review/verification.md)。这一区分已在原始审核报告中保留。未关闭项：0。

## 收尾

已正常推送最终提交到 `origin/develop`，未改写远端 develop 历史。主工作树最初因 5 个本地未推送提交而无法快进，随后仅处理本地同步：保留原始完整 tip `5a5abc0cef715802f10f777ae31bbf5aa5d01c0a` 于 `refs/kander/preserved-develop/tui-project-options`，将这 5 个提交重放到已交付代码之上。range-diff 逐项为 `=`，补丁完全一致，详见 [保留证据](integration-review/local-commit-preservation.txt)。这些用户提交仍未推送。本地 develop 为 `78cf1c9118078b35fb2abb1adb1feefe21ca1a84`，远端 develop 为最终交付提交；两者均通过最终交付 ancestry 校验，主工作树干净。

任务 worktree、远端及本地 `tui-project-options` 分支已清理。原任务审核计划 progress 为 closed，completion 前 `kander check` 退出 0：20 tasks。卡片已通过受控命令进入 done，RESULT=completed；随后全局 `kander check` 退出 0：19 tasks，目标卡检查退出 0：1 task，见 [完成检查](integration-review/completion-checks.txt)。任务专属临时审核文件在持久报告归档后清理；原始提交保护 ref 有意保留，其他工作树未改动。

验收结论：10/10 完成，无未关闭缺陷或后续任务。代码最终位于 develop。
