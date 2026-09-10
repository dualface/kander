# start, resume and launcher

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor c7ba382c-1e4f-4ba0-91e9-5f1fdefa85a3
- 窗口: herdr:wG:t1Y:wG:p25
- 开始时间: 2026-09-04 11:25
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-launch-task
- 结果: completed

## 任务目标

实现 `internal/launch` 与 `kander start`/`kander resume`: 六种 launcher, 领取回滚, 会话/窗口字段, Codex/Claude/Grok/Cursor 启动与恢复参数. 对标 onevoke `command_start`/`command_resume` 及 `kanban_takeover.py` 中与 `--agent` 接管相关的新会话元数据 (接管后的旧容器清理由 notify/dismiss 卡完成, 本卡调用其接口或先留 hook).

## 用户决策

launcher 语义与 onevoke 完全一致: `auto` 当场解析为 `herdr` 或 `tmux`, 不回落 `tmux-session`/`foreground`; Windows 默认 `console`, 拒绝 `auto`/`herdr`. `start` 只接受 `todo` 卡; `resume` 只接受 `review`/`working`, 必须且只能给 `--message` 或 `--message-file`. 显式 `--agent` 表示全新会话接管. tmux pane 标记写 `@kander_session`, 读时兼认 `@onevoke_session`.

## 预期成果

`kander start <task-id>` 原子迁入 `working/`, 写入负责人/开始时间/会话/窗口并启动 Agent; 失败回滚文档与状态. `resume` 按卡片会话唤醒或按 `--agent` 接管.

## 验收条件

- [ ] `start` 按规模用 `kanban_agent_for`, `--agent` 优先; 任务组卡使用组 prompt, 单卡使用单卡 prompt; 成功输出含规模与实际 Agent 及解析后的 launcher.
- [ ] 会话: Claude/Grok 预分配 UUID; Cursor `create-chat` 失败不领取; Codex 只记 Agent 名. 窗口: herdr `herdr:<tab>:<pane>`; tmux 系 `<launcher>:<sid>:<wid>:<pid>`; foreground/console 为 launcher 名.
- [ ] tmux/tmux-session: 先占位 window, 持久化地址后 `respawn-pane`, 再写 pane 会话标记; 任一步失败关 window 并回滚.
- [ ] `herdr`: `HERDR_ENV=1`, 新建 `--no-focus` tab, 等根 pane 首帧后再 `pane run`; 失败关 tab 并回滚. pane run 成功且 reference 非空时 best-effort `report_agent_session`, 失败只告警不回滚.
- [ ] `resume` 四种 Agent 恢复参数, Codex rollout 检索, `review` 先迁回 `working`, 启动失败 `rollback_launch` (先恢复文档再迁回). `--timeout` > 60, 默认 120. 存活校验对标 onevoke `validate_resumed_agent`.
- [ ] 显式 `--agent` 接管: 覆写负责人/会话/窗口, 不改开始时间; 失败整文回滚. 旧容器清理若 dismiss 卡尚未提供, 本卡输出 N/A 并在实施记录写明 hook; 不得在本卡复制一套 dismiss 实现.
- [ ] 测试对标 `tests/test-kanban.py` 的 start/resume/launcher/console/回滚; `tests/test-windows-console.py` 在非 Windows skip.

## 威胁模型

启动会在用户终端创建 pane/tab 并执行 Agent. 会话标记只防同用户误投, 不是对抗恶意伪造的边界. 回滚必须在 Agent 未成功启动时恢复卡片, 避免双领.

## 不在本轮范围

- 既有问题: 排除 notify 直投、忙重试、过期反查; 排除 dismiss 关容器.
- 并发/跨平台/安全加固: 纳入领取回滚与 launcher 前置检查; 排除 notify 载荷目录的 `0700` 私有文件 (notify 卡).
- 共享契约与文档: 实现必须符合 `KANBAN-RULES.md` start/resume 条款, 不改条款语义.
- 相邻功能: 排除 `notify`/`dismiss`/`subscribe`/probe 事实层. 接管后的优雅退出调用 dismiss 卡的包 API, 若该卡未合并则标记 N/A.

## 讨论与决策

```text
前置任务: 20260904-kander-board-task, 20260904-kander-process-task
```

- 需要 board 迁卡与 process 构建 Agent 命令.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-launch-task`, worktree `worktrees/20260904-kander-launch-task/`. 仓库无 `origin`, 跳过 push.
- 新增 `internal/launch` 与 `cmd/kander/launch.go` 空白导入接线; 未改冻结的 `internal/cli` 命令表.
- `kander start` / `kander resume`: 六种 launcher (`auto` 仅解析 herdr/tmux; Windows 拒绝 `auto`/`tmux`/`tmux-session`/`herdr`, `console` 仅 Windows). 领取前完成 launcher 检查; 失败回滚先恢复文档再迁回状态; 文档写入走 `internal/fs`.
- 会话: Claude/Grok 预分配 UUID, Cursor `create-chat` 失败不领取, Codex 只记 Agent 名, resume 时按 rollout 检索. 窗口 herdr/tmux/foreground/console 格式对标 onevoke. tmux 先占位 window、写窗口地址、再 `respawn-pane`、再写 `@kander_session` (读兼认 `@onevoke_session`); 项目 session 写 `@kander_project` (读兼认 `@onevoke_project`).
- herdr: `HERDR_ENV=1`, `tab create --no-focus`, 等根 pane 后再 `pane run`; `report_agent_session` best-effort, 失败只告警. request id 形如 `kander:{agent}:{seq}`.
- 接管: 显式 `--agent` 开新会话并覆写负责人/会话/窗口, 不改开始时间. 旧容器清理 hook `launch.CleanupTakeover`; dismiss 卡未提供时输出 `已清理原容器: N/A`, 未复制 dismiss 实现.
- 最终 commit: `37f00ca7a2015809747782d736b13b2b6491acac` (已 rebase 到组头 `4b8407c` 后本地 `git merge --ff-only` 进 `group/20260904-migrate-to-go-group`).
- 验证: 任务 worktree `go test ./...` 通过 (`internal/launch` 含 start/resume/herdr/tmux/回滚与对标补测; `TestWindowsConsoleLauncher` 在非 Windows skip).

### 上轮 finding (组级审核批次 10, base 18e363c, reviewed 1ed5914)

- PM-1 medium 成立: 验收要求对标 onevoke `test-kanban.py` / `test-windows-console.py` 的 start/resume/launcher/console/回滚子集, 首轮测试未覆盖 `auto` 解析、`tmux-session` 建/复用/避让/回滚、herdr 上报失败只告警、tab/pane 失败关容器回滚、resume `--message-file`、resume 启动/存活失败迁回 `review`、working 卡不迁、缺 `会话` 拒绝、Cursor resume argv、herdr 存活身份策略、Windows 拒绝 auto/herdr、console 创建失败回滚. 已写入 `launch_contract_test.go` (notify/dismiss/probe 未纳入). 未处理: herdr socket 成功读回与慢分片预算 (非 PM 点名项).
- PM-2 medium [mechanical] 成立: `isLaunchError`、`herdrSocketTimeout`、`monotonic` 无引用. 已删除三符号; 存活/上报截止改用已引用的 `nowFn`.
- QA-1 medium [mechanical] 成立, 与 PM-2 同根因一次处理, 不另修.

修复提交 `37f00ca7a2015809747782d736b13b2b6491acac`. 无 origin, 跳过 push, 本地 ff 进组分支.

## 完成总结

- 交付: `kander start`/`kander resume` 与 `internal/launch`; 六种 launcher, 领取回滚, 四 Agent 启动/恢复参数, 接管元数据, 旧容器清理 hook.
- 验收: 7/7; 规模选 Agent、会话窗口字段、tmux 顺序与回滚、herdr tab/pane、resume 存活校验与 Codex rollout、接管 N/A hook、Windows console skip 及批次 10 对标补测均已自检.
- 验证: `go test ./...` 在 `37f00ca7a2015809747782d736b13b2b6491acac` 通过.
- 审核: 组级批次 10 首轮 + 增量; PM/QA 通过 (onevoke review); CSA/Hacker N/A (仓库 AGENTS.md). 修复一轮: PM-1 补对标测试, PM-2/QA-1 删死代码, 提交 `37f00ca7a2015809747782d736b13b2b6491acac`.
- 收尾: 最终 commit `37f00ca7a2015809747782d736b13b2b6491acac` 已是本地 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 祖先; 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回 `develop` (无 origin, 未 push). `merge-memory` 空操作退出 0; 已删本卡 worktree 与本地任务分支; 组分支保留; `kander check` 由主控在全组 `done/` 后执行.
