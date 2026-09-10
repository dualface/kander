# Board queries and state transitions

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 770e7632-d151-4d1a-851d-418df0ace5e9
- 窗口: herdr:wG:t1T:wG:p21
- 开始时间: 2026-09-04 11:07
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-board-task
- 结果: completed

## 任务目标

实现 `internal/board` 及 `kander` 的 `init` `rules` `list` `show` `new` `move` `pick` `check` (不含存活探测). 对标 onevoke `bin/kanban` 中不启动 Agent 的看板生命周期.

## 用户决策

看板目录仍为 `kanban/`, 定位顺序 `KANBAN_DIR` -> 主 worktree `kanban/` -> 向上查找. 七态、卡片模板、任务 ID、任务组字段、`前置任务` 解析与 onevoke 完全一致. 本卡不实现 start/resume/notify.

## 预期成果

可在 Git 或非 Git 目录初始化看板, 创建/列出/展示/迁移任务, `pick` 做完整性校验, `check` 校验入口与依赖图 (不含 working 存活段).

## 验收条件

- [ ] `init` 幂等创建 7 个状态目录; 旧板缺 `review/` 且其余 6 个齐全时任一命令定位即补建; 其他缺失或 `review` 被文件/reparse 占用则全部失败.
- [ ] Git 项目只写本地 `.git/info/exclude`; Windows 新目录 `CREATE_NEW` + 私有 DACL.
- [ ] `rules` 按入口输出当前作用域 `KANBAN-RULES.md`, 项目模式不回落全局.
- [ ] `list` 按状态分组、组内显示时间倒序, 同时缺失时按任务 ID 倒序; 标规模; `--mobile` 竖屏布局.
- [ ] `new` 在 `backlog/` 建小任务; `--large` 建含 `spec.md` 的目录卡. ID `YYYYMMDD-short-slug-task`.
- [ ] `pick`/`move` 只执行状态模型允许的迁移; `review` 只能从 `working` 进入且要求已填 `任务分支`.
- [ ] `check` 默认排除 `done`/`archived` 的无关无效入口; `--all` 纳入; 指定 ID 只检查目标及可达依赖; 依赖未满足不失败; 无环与引用存在. 本卡 `check` 不输出存活段 (留给 coord 卡).
- [ ] `task_dependencies` 返回组内卡、组外卡、任务组三类引用及成员展开.
- [ ] 测试对标 `tests/test-kanban.py` 中生命周期、规模、依赖、`review` 状态迁移; 不含 launcher/notify.

## 威胁模型

看板文件在主 worktree, 经 fs 层 no-follow 迁移. 同主机 Agent 竞争用目录迁移作领取原语, 不加锁服务. 卡片不得写入 token 或凭据 (模板约束, 本卡做结构校验不扫内容密钥).

## 不在本轮范围

- 既有问题: 排除历史卡片缺字段的批量改写; 缺 `会话`/`窗口` 时按规则在 start 时插入, 本卡不 start.
- 并发/跨平台/安全加固: 纳入看板目录与卡片迁移的 fs 边界; 排除 pane 探测与存活.
- 共享契约与文档: 排除改卡片模板与状态机; 若实现与 `KANBAN-RULES.md` 命令契约有出入, 以规则为准修代码.
- 相邻功能: 排除 `start`/`resume`/`notify`/`dismiss`/`subscribe`/`web`/`tui` 及 check 的 liveness 段.

## 讨论与决策

```text
前置任务: 20260904-kander-cli-install-task
```

- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务 worktree: `/home/dualf/works/kander/worktrees/20260904-kander-board-task`, 分支 `20260904-kander-board-task`, 基于当时组分支并在汇入前 rebase 到 `group/20260904-migrate-to-go-group`.
- 实现 `internal/board`: 定位/init/exclude、七态扫描、卡片读写、状态迁移、list/show、依赖解析与 `check`（无存活段）. CLI 通过 `internal/cli/wire_board.go` 覆写 Runner, 未改冻结命令表.
- 提交: `931d2511c5467fd6bf05d268c5745c3e357b302d` (实现看板生命周期命令 internal/board).
- 无 origin, 未 push 任务分支; 已在组 worktree `git merge --ff-only` 进入 `group/20260904-migrate-to-go-group`.
- 验证: `go test ./...` 在 rebase 后于任务 worktree 通过 (`internal/board` 覆盖生命周期、list、pick/review、依赖/check 范围、init/rules/exclude、旧板补 `review/`).

- 组级审核批次 7 (base `0e84bc3` / 原 HEAD `931d251`):
  - QA-1 medium 成立: `BoardRoot` 把 symlink `kanban/` 当未找到并继续向上找, 会写到祖先看板. 已改为候选为 reparse 立即返回与 `ensureLayout` 相同错误, 仅缺失才继续. 测试 `TestBoardRootRejectsSymlinkKanbanWithoutWalkingPast`.
  - QA-2 / PM-1 medium [mechanical] 成立 (同根因一次处理): 删除未引用的 `ReadRulesFile`; 测试仍用 `installPathsFn`.
- 修复提交: `408c866f486c3cdb6f91ace2cd14e544a9ceda98`, rebase 后 `go test ./...` 通过, 已 ff 进 `group/20260904-migrate-to-go-group`. 无 origin, 未 push.

## 完成总结

交付: `kander init|rules|list|show|new|move|pick|check` 可用; `TaskDependenciesOf` 区分组内卡、组外卡、任务组并展开成员.
验收: 9/9 自检通过 (init 七态与缺 `review/` 补建、Git exclude、rules 按安装作用域、list 分组与 `--mobile`、new 小/大卡、pick/move/review 门禁、check 范围与无环、依赖三类引用、Go 测试对标生命周期; Windows CREATE_NEW/DACL 走 `internal/fs`, 本机 POSIX 验证).
验证: `go test ./...` 通过. 未执行 start/resume/notify 与 check 存活段 (本卡排除).
审核: 组级批次 7 首轮 + 增量; PM/QA 通过; CSA/Hacker N/A (仓库 AGENTS.md). 修复一轮: QA-1 与 QA-2/PM-1, 提交 408c866f486c3cdb6f91ace2cd14e544a9ceda98. 未处理项: PM-2 测试对标缺口; PM-3 archive completed 可从非 done 出站 (NON-BLOCKING).
收尾: 最终 commit 408c866f486c3cdb6f91ace2cd14e544a9ceda98 已是 develop bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e 祖先. 组分支已 ff 合回本地 develop; 无 origin. 记忆合并空操作 (源无 .memsearch/memory). 已删本卡 worktree 与本地任务分支; 未删组分支.
