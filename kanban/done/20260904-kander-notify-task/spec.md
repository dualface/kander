# Direct delivery, dismissal and takeover

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 41499555-b087-44b2-957b-eabd73e4c3aa
- 窗口: herdr:wG:t22:wG:p29
- 开始时间: 2026-09-04 11:58
- 完成时间: 2026-09-04 15:08
- 任务分支: 20260904-kander-notify-task
- 结果: completed

## 任务目标

实现 `internal/notify`、`internal/window`、`internal/takeover` 与 `kander notify`/`kander dismiss`, 以及 `resume --agent` 成功后的旧容器清理. 对标 `kanban_notify.py`、`kanban_window.py`、`kanban_takeover.py` 与 dismiss 命令契约.

## 用户决策

`notify` 是主控派回的唯一接口: 忙则在 timeout 内重试且不恢复; 过期则反查, 唯一命中复检后直投; 否则内部恢复原会话. 主控只调一次 notify. 载荷目录创建时 `0700`、文件 `0600`; 终端只收 `# kander-notify:` (兼认 `# onevoke-notify:`) 单行路径. herdr 用 `agent prompt`, 不用 `pane run` 投 TUI. dismiss 只接受 `done`/`archived`, 不改卡片, 不强杀.

## 预期成果

任务组主控可把 finding 和收尾派回原 Agent; 完成后可遣散 pane; resume 接管可清理原容器.

## 验收条件

- [ ] notify 三类探查: 忙/过期/降级, 对标 `tests/kanban_notify_liveness_tests.py`.
- [ ] 直投成功即迁卡; marker 超时只警告不恢复; foreground/console/无通道/发送失败才走恢复; 两路都失败时正文与状态不变并同时报告原因.
- [ ] 窗口回写: 恢复/接管成功后重读卡片再写新地址; 失败恢复调用前原文. 对标 `tests/kanban_window_writeback_tests.py`.
- [ ] dismiss: 身份+单 pane 拓扑门禁, Claude/Codex `/exit`, Grok/Cursor `/quit`; 失败保留现场. 对标 `tests/kanban_dismiss_tests.py`.
- [ ] takeover: 新 Agent 存活后按 dismiss 门禁清理原容器; 失败只报告保留, 不回滚新 Agent. 对标 `tests/kanban_takeover_tests.py`.
- [ ] Windows notify 临时根只用 GetTempPathW 词法路径, 再经 fs 层拒绝 reparse.

## 威胁模型

直投会向已运行 Agent 发送正文. 必须在身份 (Agent+会话标记) 与拓扑 (单 pane 容器) 校验通过后才创建载荷. 会话标记不是对抗同用户伪造的边界. dismiss 失败不得强杀, 以免误关他人 pane.

## 不在本轮范围

- 既有问题: 排除 probe 事实层重构 (coord 卡已提供); 本卡只做策略与投递.
- 并发/跨平台/安全加固: 纳入载荷私有目录与 Windows 临时根; 排除审核 runtime.
- 共享契约与文档: 实现符合 `KANBAN-RULES.md` notify/dismiss 条款, 不改语义.
- 相邻功能: 排除 subscribe 事件循环与 start 的首次领取. launch 卡若留了 takeover hook, 本卡补齐并删除 N/A.

## 讨论与决策

```text
前置任务: 20260904-kander-coord-task
```

- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务 worktree: `/home/dualf/works/kander/worktrees/20260904-kander-notify-task`, 基于组集成分支 `group/20260904-migrate-to-go-group`.
- 实现 `internal/window` 窗口回写与失败恢复原文; `internal/notify` 忙/过期/降级探查、直投载荷 (`0700`/`0600`)、`kander notify`; `internal/takeover` 的 `kander dismiss` 与 `launch.CleanupTakeover` hook.
- Windows 临时根走 `GetTempPathW` 词法路径, 再经 `internal/fs` 创建私有目录. herdr 直投用 `agent prompt`.
- 验证: `go test ./...` (模块根, 2026-09-04) 全部通过; rebase 到组分支头后重跑同样通过.
- 最终任务 commit: `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` (已 ff 进 `group/20260904-migrate-to-go-group`).
- 无 origin, 跳过 push.

### 上轮 finding (组级审核批次 14, batch commit `a36670a…`, batch base `1278f91…`)

主代理核实: QA-1、QA-2 成立并已修; NON-BLOCKING 见下.

| ID | 核实 | 处理 |
| --- | --- | --- |
| QA-1 medium [mechanical] | 成立 | 删除 `internal/notify/notify_test.go` 中 `TestTmuxReverseLookupUniqueMatch` (与 `internal/liveness/liveness_test.go` `TestTmuxReverseLookupUniqueMarker` 同四行 pane / `%4` / 两种 window 渲染). 过期反查仍由 `TestNotifyStaleTmuxRewritesWindow` 覆盖. |
| QA-2 medium [mechanical] | 成立 | 删除 `launch.ParseSession` (`notify_resume.go`); 调用方只用 `ResolvedSession`. 修复后 `rg ParseSession` 无命中. |
| PM-1 suggest | 非阻塞 | 本轮不补 mixin 级 notify-resume / marker-timeout / takeover 空引用测试. |
| PM-2 suggest | 非阻塞, 已顺手 | `herdrDirectNotify` / `tmuxDirectNotify` 在「已投递, 等待回执」后 `flushStdout()`, 对标 Python `flush=True`. |
| QA-3 recommend | 非阻塞 | `window.RenderWindowMetadata` 与 `launch.renderWindowMetadata` 重复, 本轮不改 launch 卡文件. |
| QA-4 suggest | 非阻塞, 已顺手 | 去掉 `RunNotify` 中无用的 `_ = config.LanguageIsChinese()` 及 `config` import. |

- 修复提交 `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`; rebase 组头 `85f388e` 后 `go test ./...` 通过并 ff.

## 完成总结

- 交付: `kander notify` / `kander dismiss` 可用; resume `--agent` 接管后按 dismiss 门禁清理原容器, 失败只报告保留.
- 验收: 6/6 自检通过 (忙不恢复、过期反查直投、降级恢复双原因、窗口回写/回滚、dismiss 拓扑门禁、takeover hook).
- 验证: `go test ./...` 通过. 无 origin, 任务分支未 push. 组集成分支已 ff 到 `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`.
- 审核: 批次 14 三连提交首轮 + 增量; PM/QA 通过. CSA/Hacker N/A (仓库 AGENTS.md). 必修 QA-1/QA-2 已核实修复 (见实施与验证). 修复轮次 1.
- 收尾: 最终 commit `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`; 组分支已 ff 合回本地 `develop` (同 SHA, 无 origin 未 push). `merge-memory` 空操作退出 0; 已删本卡 worktree 与本地任务分支; 未删组分支. 未处理项: PM-1 窗口回写/失败恢复测试缺口; QA-3 launch 仍复制 `RenderWindowMetadata` (NON-BLOCKING).
