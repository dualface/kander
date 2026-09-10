# 操作命令必须读取完整配置

## 实际改动

操作类 CLI 与 TUI 选项/启动改为 `config.Load(false)`。缺文件、非法 JSON、schema 校验失败立即非零退出，不再回落 `DefaultConfig` 或 `Effective(nil)` 的 welcome 掩码。`kander doctor` 失败走既有 `Repair`，修完后按合并配置重新探测 Agent。选项面板每次打开先 `Load(false)`，失败停在 `loadErr` 画框；保存仍走 `LoadScope`。按 `s` 配置失败留在启动弹层。

## 最终提交

- 基线：`211c104282f181d5fabe6cc49b03583b0bed269f`（创建任务分支时的 `origin/develop`）
- 合入 `develop` 前因源分支前进，干净 rebase 到 `ef9f640`，无冲突，只重跑验证、未复审。
- 最终交付：`fd24781ac0f6abf001e11422d926b566e56623f8`
  - `fba2e46` feat(config): require a complete config for operational commands
  - `fd24781` fix(config): reload complete config on options reopen and doctor repair

## 验证

`go test ./...` 于 rebase 后的 `fd24781ac0f6abf001e11422d926b566e56623f8` 通过，21 个包 ok。`git diff --check` 干净。

## 审核

计划 `require-complete-config`，批次 `require-complete-config-1` 已关闭。

- PM：required，codex，PASS（`require-complete-config-pm-2`）
- QA：required，codex，PASS（`require-complete-config-qa-2`）
- CSA：N/A（本仓库 AGENTS.md；`review_stages.large.CSA=skip`）
- Hacker：N/A（本仓库 AGENTS.md；`review_stages.large.Hacker=skip`）

首轮 PM-001/QA-001/QA-002/QA-003 已在 `fd24781` 修复并经增量复审关闭。原文见 `reviews/<run_id>/`。

## 偏差

无。welcome 未完成但文件合法时是否额外拒绝启动，按 OUT_OF_SCOPE 未改。

## 未解决问题

无。

## 风险

无新增风险。

## 验收结论

验收条件均已满足。已合入 `origin/develop`（`fd24781ac0f6abf001e11422d926b566e56623f8`）。
