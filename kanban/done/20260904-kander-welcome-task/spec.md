# welcome and doctor

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 4d6a6517-4eb8-4582-ad67-c450110ac756
- 窗口: herdr:wG:t1V:wG:p22
- 开始时间: 2026-09-04 11:08
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-welcome-task
- 结果: completed

## 任务目标

实现 `internal/menu` 与 `kander welcome`/`doctor`/`config`. 对标 `bin/onevoke` 的引导、诊断与配置展示, 不含 review 分发 (review 卡已承担).

## 用户决策

welcome 只在 tty 提问; 无 tty 时诊断后提示重跑. stdin/stderr 均为 tty 时用方向键菜单, 否则编号/文本. 执行 Agent 菜单先大任务再小任务, 写入 `kanban_agents` 并把 `kanban_agent` 设为大任务 Agent. MemSearch 对账规则与 onevoke 相同 (仅 Codex/Claude 支持; 原生 Windows 不启用该集成). 依赖安装必须用户明确选择. 项目入口不回落全局配置或 PATH 同名命令.

## 预期成果

用户可完成首次配置、随时 `--reset`、用 `kander config` 查看、用 `kander doctor` 检查命令与规则接入.

## 验收条件

- [x] welcome 显示配置总览, 只进入用户选择的单项; 总览默认停在保存; q 取消.
- [x] doctor 按 `execution_agents_in_use` 检查每个执行 Agent; 规则接入以当前作用域规则入口为准.
- [x] `kander config` 输出当前生效配置 (含语言、launcher、agents、models、review_stages).
- [x] MemSearch: 菜单确认启用时对账; 执行 Agent 集合变化时保存前对账; 全部不受支持则关闭并告警.
- [x] Windows launcher 菜单只列 `console` 与 `foreground`; POSIX `auto` 最前, herdr 在 tmux-session 之后、foreground 之前 (已安装或当前已选时).
- [x] 测试对标 `tests/test-onevoke.py` 中 welcome/doctor/config 与项目安装上下文, 用临时 HOME 与伪终端.

## 威胁模型

welcome 写配置文件, 必须走 config 卡的 fs 边界. 不在菜单中打印密钥. MemSearch 安装只克隆官方仓库并跑上游脚本, 不扩大权限.

## 不在本轮范围

- 既有问题: 排除自动导入 `~/.config/onevoke`; 若发现旧路径, doctor/welcome 可提示手工拷贝, 不做静默迁移.
- 并发/跨平台/安全加固: 纳入 tty 检测与配置保存; 排除审核 runtime.
- 共享契约与文档: 不改发布规则; 菜单文案中的产品名用 Kander.
- 相邻功能: 排除 `review` 执行、看板命令、安装器本身 (只被安装器在全局安装末尾调用).

## 讨论与决策

```text
前置任务: 20260904-kander-cli-install-task
```

- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-welcome-task`, worktree `worktrees/20260904-kander-welcome-task/`, 基于组分支当时头; 随后 rebase 到含 start/resume 与 web 的组头 `1ed5914`.
- `internal/menu` 实现 `kander welcome`/`doctor`/`config`: tty 检测, 方向键与编号菜单, 执行 Agent 先大后小并写入 `kanban_agents`/`kanban_agent`, MemSearch 对账与官方仓库克隆, Windows launcher 仅 console/foreground, POSIX auto 最前且 herdr 在 tmux-session 与 foreground 之间. 配置保存走 `config.Save` (fs 边界). 产品名与规则入口为 Kander / `KANDER-AGENTS.md`. 无 tty 时 doctor 后提示终端重跑. 发现 `~/.config/onevoke` 只提示手工拷贝.
- `internal/cli` 覆写 welcome/doctor/config Runner; 未实现子命令测试排除这三项.
- 提交: `81506c0214994bea7c4d4a40d406a3c06e5ed2d5`.
- 验证: 任务 worktree `go test ./...` 通过 (`internal/menu` 含临时 HOME、伪终端 welcome/doctor/config 与项目入口不回落全局). Windows 专项路径有 build tag, 本环境未跑.
- 无 `origin`, 跳过 push; 本地 `git merge --ff-only` 已把任务分支头 ff 进 `group/20260904-migrate-to-go-group`.

### 审核回修 (review findings)

主代理核实: QA-1/PM-1、QA-2、QA-3、QA-4/PM-3、QA-5/PM-4、QA-6、PM-2、PM-5 均成立; 同根因项一次修完.

| ID | 结论 | 处理 |
|---|---|---|
| QA-1 / PM-1 | 成立 | SIGINT 在 `wrapInterrupt` 返回 130 前调用 `restoreRawTTY()`, 恢复 cooked termios 与光标; Ctrl-C 测试断言 slave 仍带 ICANON/ECHO |
| QA-2 | 成立 | 不再把未消费的可打印键写入跨菜单 `pendingBytes`; `y`/`n` 确认后排空至换行 |
| QA-3 | 成立 | 拆分 `menu_test.go` 为 harness / welcome / doctor 等文件, 均低于 1000 行 |
| QA-4 / PM-3 | 成立 | 删除空 `TestAskYesNoTextFallback`, 改为管道 stdin 覆盖 yes/默认 |
| QA-5 / PM-4 | 成立 | 删除未使用导出 `RulesIntegration` |
| QA-6 | 成立 | MemSearch 源目录已存在则跳过 clone, 仍执行 `install.sh` |
| PM-2 | 成立 | 项目模式 `projectCommandPath` 在 Windows 先找 `kander.exe` |
| PM-5 | 成立 | 补 onevoke 对标: 未覆盖 Agent 告警、对账、项目 welcome 写项目配置、reset 保留不可用当前值、tmux 安装失败保留 launcher、Cursor 模型无 effort、doctor herdr/tmux-session/auto/console 等 |

- 修复提交: `4ad6766eae9b00348ad9064959c9943d1c8e0110` (rebase 到组头 `37f00ca` 之后).
- 验证: 任务 worktree rebase 后 `go test ./...` 通过.
- 无 `origin`, 跳过 push; 组 worktree `git merge --ff-only` 已将组分支快进到 `4ad6766`.

### 审核回修 (batch 11 增量)

主代理核实: **PM-5 余项成立** (Windows `editLauncher` 无对标测试). QA 未将其列为新闸口, 仍按 Gate 关闭.

| ID | 结论 | 处理 |
|---|---|---|
| PM-5 余项 | 成立 | `TestEditLauncherWindowsMenuOnlyConsoleForeground` 强制 `windowsOS`, 断言菜单仅 console/foreground, 选择后 `config.Save` 写入 `launcher=foreground`. 无 unix build tag, Linux/Windows CI 都会跑 |
| PM-6 | 成立但非闸口 | 本轮不删 `pendingBytes` 管道 |
| QA-7 / QA-8 | 不要求关闭 | 未改 |

- 修复提交: `85f388e8ea39851db6f1a6f96ea9bf7253fceed5` (rebase 到组头 `506d5b6` 之后).
- 验证: rebase 后 `go test ./...` 通过.
- 无 `origin`, 跳过 push; 组分支已 ff 到 `85f388e`.

## 完成总结

- 交付: 用户可用 `kander welcome`/`--reset` 完成配置, `kander config` 查看生效配置 (含语言、launcher、agents、models、review_stages), `kander doctor` 按 `execution_agents_in_use` 检查 Agent 与当前作用域规则入口.
- 验收: 6/6; 上列验收条件均已在 POSIX 测试中自检.
- 验证: `go test ./...` 通过.
- 审核: 组级批次 11, 首轮 + 增量 + 增量2; PM/QA 通过; CSA/Hacker N/A (仓库 AGENTS.md). 闸口修复两轮: `4ad6766` (Ctrl-C tty、输入残留、测试拆分、MemSearch/项目路径等), `85f388e` (Windows launcher 菜单测试). 非闸口未处理: QA-7、QA-8、PM-6.
- 收尾: 最终 commit `85f388e8ea39851db6f1a6f96ea9bf7253fceed5` 已是 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 祖先; 组分支已 ff 合回本地 `develop` (无 origin, 未 push). `merge-memory` 空操作退出 0 (源无 `.memsearch/memory`). 已删本卡 worktree 与本地任务分支; 未删组分支.
