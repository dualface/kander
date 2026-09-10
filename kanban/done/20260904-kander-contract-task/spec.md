# Rule publishing and repository contract

- 类型: Chore
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 676daf55-b4e9-4102-b270-1981bce49ca0
- 窗口: herdr:wG:t1M:wG:p1V
- 开始时间: 2026-09-04 02:25
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-contract-task
- 结果: completed

## 任务目标

把 `~/works/onevoke` 的发布规则、README、LICENSE、docs 迁入本仓库, 并写明本仓库 Go 开发契约. 规则正文只改命令名与安装身份路径, 不改工作流机制.

## 用户决策

用户已确认计划并走看板. 命名以本卡为后续实现的唯一契约:

- 单一命令 `kander` 合并原 `onevoke` 与 `kanban` 全部子命令; `onevoke-review.sh`/`.cmd` 改为 `kander review`; `merge-worktree-memory.py`/`.cmd` 改为 `kander merge-memory`.
- 安装身份改名: `.kander/`, `~/.config/kander/config.json`, `~/.local/share/kander`, `KANDER_CONFIG`/`KANDER_LANG`, 规则入口 `KANDER-AGENTS.md`, git exclude `/.kander/`.
- 看板数据目录仍为 `kanban/`, 覆盖仍为 `KANBAN_DIR`; 配置键 `kanban_agent`/`kanban_agents`/`models.kanban` 不改.
- 协议串双读: 新写 `@kander_session` / `# kander-notify:` / `@kander_project`, 同时识别 onevoke 旧值.
- 本卡只落文档与规则, 不写 Go 实现. 后续卡以本卡为前置, 按 `AGENTS.md` 的包图实现, 互不改同一文件.

## 预期成果

仓库根存在完整发布规则与开发契约, 后续实现卡无需再猜命令名、路径或包边界.

## 验收条件

- [x] `rules/KANDER-AGENTS.md` 为入口; 分册 `BASE-RULES.md` `GIT-RULES.md` `REVIEW-RULES.md` `CODE-RULES.md` `KANBAN-RULES.md` 均在 `rules/`.
- [x] 与 `~/works/onevoke/rules/` 对比, 行为条款不变; 可见差异仅限命令名 (`onevoke`/`kanban`/`onevoke-review.*`/`merge-worktree-memory.*` -> `kander` 对应子命令) 和安装身份路径/环境变量/入口文件名.
- [x] `KANBAN-RULES.md` 命令表全部为 `kander <subcommand>`; 正文中的 `kanban/` 目录与 `KANBAN_DIR` 保持不变.
- [x] `REVIEW-RULES.md` 审核入口统一为 `kander review`, 不再描述 Python/shell 分叉.
- [x] `README.md` 新手四步与常用命令使用 `kander`; `docs/` 图与说明同步; `LICENSE` 为 MIT.
- [x] 根 `AGENTS.md` 写明: Go 模块路径, 包图 (`cmd/kander`, `internal/{cli,config,fs,process,board,launch,probe,liveness,notify,takeover,window,review,memory,web,tui,menu}`), 子命令由 `internal/cli` 一次注册、后续卡只实现对应包、禁止为接线改同一 `main.go` 冲突段, 测试命令, Windows 句柄/reparse/DACL 约束, 以及本仓库 CSA/Hacker N/A、PM/QA 适用.
- [x] `.gitignore` 排除 `.memsearch/`、`/worktrees/` 与构建产物, 不把 `kanban/` 写入项目 gitignore.

## 威胁模型

N/A (只迁文档与规则, 不改运行时代码或安全边界实现.)

## 不在本轮范围

- 既有问题: 排除 onevoke Python 实现的任何缺陷修复; 本卡不回写 onevoke 仓库.
- 并发/跨平台/安全加固: 排除. 规则中的 POSIX/Windows 安全条款原样保留, 本卡不新增实现.
- 共享契约与文档: 纳入本卡; 这就是契约卡. 配置 JSON 字段名保持 onevoke schema (除路径与环境变量前缀).
- 相邻功能: 排除全部 Go 包、安装器、CLI、TUI/Web 实现, 由后续卡承担.

## 讨论与决策

```text
前置任务: N/A
```

- 本卡是组内契约卡, 无组内前置.
- 源目录固定为 `~/works/onevoke`, 只读拷贝后改名, 不修改源仓库.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 工作区: `/home/dualf/works/kander/worktrees/20260904-kander-contract-task`, 基于组分支 `group/20260904-migrate-to-go-group` 创建任务分支 `20260904-kander-contract-task`.
- 从 `~/works/onevoke` 只读拷贝 `rules/`, `README.md`, `LICENSE`, `docs/` 后改命令名与安装身份; 不修改源仓库, 不写 Go 实现.
- 提交: `e2022e7` 迁入发布规则; `b577704` 同步 README/图示/LICENSE; `16c87fc` 写入 `AGENTS.md` 与 `.gitignore`; `572842f` 审核修复 (协议双读对齐, 去掉 Python curses).
- 最终 commit: `572842f4b613f4614fa65d9e31fa11c868999ca1`.
- 仓库无 `origin`, 任务分支未 push, 仅本地提交; 已在组分支 worktree 对 `group/20260904-migrate-to-go-group` 执行 `git merge --ff-only`.
- 验证: 核对 `rules/` 六份分册齐全; 命令表均为 `kander <subcommand>`; `kanban/` 与 `KANBAN_DIR` 保留; `REVIEW-RULES.md` 入口仅为 `kander review`; README 新手四步与常用命令为 `kander`; LICENSE 首行为 MIT; `.gitignore` 含 `.memsearch/` `/worktrees/` 构建产物且不以路径条目排除 `kanban/`; `AGENTS.md` 含模块路径、包图、cli 一次注册、测试命令、Windows fs 约束、CSA/Hacker N/A.
- 上轮 finding (批次 1, QA medium, commit `16c87fc`):
  - QA-1 成立: 用户决策要求协议双读, 但查找条款只写新标记, 与第 42 行摘要冲突. 未删第 42 行 (删了会违反用户决策); 已把反查过滤、notify 身份匹配、notify 指令前缀匹配、tmux-session 项目标记复用写成 `@kander_*` 或 `@onevoke_*` / `# kander-notify:` 或 `# onevoke-notify:`. 修复 commit `572842f`.
  - QA-2 成立: 本卡 `AGENTS.md` 已规定无 Python 且 TUI 在 `internal/tui`, 发布规则仍要求 Windows Python curses. 已去掉 curses/Python 要求, 保留「Windows TUI 不属于本阶段保证; 无法加载时用 `kander web`」, 未把 Go 包名写入发布分册. 修复 commit `572842f`.
  - PM-NB-1 suggest: 与 QA-1 重叠, 已随 QA-1 处理, 本轮不另改.
  - QA-3 suggest: `Path.home() / ".local/bin/kander"` 只是 onevoke 同形改名, 非本轮必修.

## 完成总结

- 交付: 仓库根具备发布规则 (`rules/KANDER-AGENTS.md` 及五份分册)、MIT LICENSE、kander 版 README/docs, 以及 Go 开发契约 `AGENTS.md`.
- 验收: 7/7 自检通过, 见上验收条件勾选.
- 验证: 见「实施与验证」; 无代码测试; 无 origin 故未远程 push.
- 审核: 批次 1 首轮 + 增量; PM 通过; QA 通过; CSA/Hacker N/A (仓库 AGENTS.md). 修复一轮: QA-1/QA-2 成立并修于 `572842f`. 未处理: QA-3 suggest (`REVIEW-RULES.md` 仍写 `Path.home() / ".local/bin/kander"`).
- 收尾: 最终 commit `572842f4b613f4614fa65d9e31fa11c868999ca1` 已是 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 祖先; 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回 develop. 记忆合并空操作退出 0; 已删本卡 worktree 与本地任务分支; 无 origin 未删远端. 未删组分支.
