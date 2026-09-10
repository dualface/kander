# 完成报告

## 实际变更

- `config.Load` 返回作用域配置与项目主工作树根 `.kander-config.json` 的对象级深合并结果；`LoadScope` 只读未合并的作用域文件，供 Save / Update / SaveIfUnchanged / doctor / TUI 编辑。覆盖文件禁止 `schema_version`、`welcome_complete` 与未知顶层键；非法 JSON、非常规文件、symlink/reparse 报错并带路径。
- Git 目录在主工作树根定位覆盖文件；非 Git 从当前目录向上查找。合并前对作用域与覆盖两侧的 `review_stages` 做规范化，避免档名与角色同层。
- `kander config --json` 输出合并结果；人类可读输出多打印覆盖文件绝对路径。选项面板加载并保存作用域配置，有覆盖时顶部提示；会话语言回落走 `ResolveScopeLanguage()`，不把覆盖语言写回作用域。
- 规则文档（`rules/KANDER-AGENTS.md`、`KANDER-KANBAN-RULES.md`、`AGENTS.md`、`docs/custom-agents.md`）与 en / zh-CN / ja i18n 已同步覆盖优先级与已接受风险。

## 最终提交与映射

- 任务分支（已删除）：`20260909-project-config-overlay`
- 组分支最终 HEAD：`4832ee9db7912db15d297a32ee8888fff757bf4a`（`group/20260909-config-layering-group`）
- 本卡最终交付：`4832ee9db7912db15d297a32ee8888fff757bf4a`
- 首轮（未重写）：`d275325da177c9724a61e21517ca7961ab4d5e53`、`5631b184af859cd726f07bfa119e5378a5e3ded3`
- 第一修复轮（同步轮 rebase 重写）：`2e70155` → `c1e3851fb4008854d2f58c418411caef79216665`；`fe34100` → `fa8ab21a402b522e5820c631b0e97712f8ceb309`；`f7b31bc` → `5d632af67ee28a19769972298a11984ced5e76e6`
- 第二修复轮：`32ba6ee3bb197acc504d6208fe521cb1d68d0bfc`、`777b1f088985e3d11cec3bd9b4dde9271822119e`
- 第三修复轮：`4832ee9db7912db15d297a32ee8888fff757bf4a`
- 以上提交均已包含在 `develop`（与 `origin/develop` 同为 `4832ee9`；组分支 fast-forward 合入，未改写历史）。

## 验证

- 最终交付 `4832ee9db7912db15d297a32ee8888fff757bf4a`：`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 通过，1269 passed。
- wrap-up：`git fetch origin` 后 `git merge-base --is-ancestor 4832ee9db7912db15d297a32ee8888fff757bf4a origin/develop` 通过；本地与远端 `develop` 均为该 SHA。
- 集成证据：`dispatches/wrapup-c2/integration.json`（`source_commit` `5631b184…` 为计划记录的批次目标；关闭最终目标 `4832ee9` 另经验证为 `origin/develop` 祖先）。

## 审核

- 计划 `config-layering-cycle` 已封存；批次 `batch-one`（base `7466cd6acfd3077bdf311c0bb0ca163566d37ba6` → 最终目标 `4832ee9`）已关闭。
- PM：结论 run `pm-batch-one-r4` 通过于 `4832ee9`（前序失败 `pm-batch-one-r1` 由 `pm-batch-one-r2` 替代）。
- QA：结论 run `qa-batch-one-r4` 通过于 `4832ee9`（前序失败 `qa-batch-one-r2` 由 `qa-batch-one-r3` 替代）。
- CSA / Hacker：N/A（本仓库 AGENTS.md 角色例外）。
- 修复轮 3 轮：`fix-batch-one-r1-c2`、`fix-batch-one-r2-c2`、`fix-batch-one-r3-c2`；另有多轮仅处置的 sync。

## 偏差

无合同内未交付项。覆盖侧 `review_stages` 规范化错误文案仍不带覆盖文件路径（QA-105，suggest，deferred）；doctor 用合并配置判断 Agent 可用性保持不变（PM-05，rejected，out-of-contract）。

## 未解决项

- QA recommend deferred `reviews/qa-batch-one-r4/dispositions/qa-101-deferred-r4.json`
- QA low deferred `reviews/qa-batch-one-r4/dispositions/qa-102-deferred-r4.json`
- QA suggest deferred `reviews/qa-batch-one-r4/dispositions/qa-103-deferred-r4.json`
- QA suggest deferred `reviews/qa-batch-one-r4/dispositions/qa-105-deferred-r4.json`
- PM suggest deferred `reviews/pm-batch-one-r4/dispositions/pm-06-deferred-r4.json`
- PM suggest deferred `reviews/pm-batch-one-r4/dispositions/pm-07-deferred-r4.json`
- PM low deferred `reviews/pm-batch-one-r4/dispositions/pm-11-deferred-r4.json`
- PM suggest rejected `reviews/pm-batch-one-r2/dispositions/pm-05-rejected-r1.json`

## 验收结论

8/8 验收条目已由最终交付上的测试与文档变更覆盖：覆盖定位（Git / 非 Git）、深合并、禁止键与路径报错、写入隔离（含缺 `language` 键分支）、`review_stages` 规范化合并、CLI/TUI 提示、规则与 i18n。批次 `batch-one` 已关闭；组 HEAD `4832ee9` 已 fast-forward 进入 `develop`。本卡可完成。
