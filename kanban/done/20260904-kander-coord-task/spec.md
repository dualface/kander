# Liveness probing and subscription

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor f7f522e0-32d8-41b6-a440-d74f33f54f24
- 窗口: herdr:wG:t21:wG:p28
- 开始时间: 2026-09-04 11:42
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-coord-task
- 结果: completed

## 任务目标

实现 `internal/probe`、`internal/liveness` 与 `kander subscribe`, 以及 `kander check` 的只读存活段. 对标 `kanban_probe.py`、`kanban_liveness.py` 和 `subscribe`/`check` 的 liveness 部分.

## 用户决策

四态 `alive`/`stopped`/`drifted`/`unknown` 纯只读, 不影响 `check` 退出码, 永不写卡. herdr 未上报会话身份但 Agent 与状态匹配时为 `alive` 并提示无法直投. Windows 与 foreground/console 恒为 `unknown`. 协议双读 `@kander_session` 与 `@onevoke_session`.

## 预期成果

主控可用 `kander subscribe` 阻塞读取 JSON Lines; `check` 在 working 卡上输出存活段; notify 卡可复用 probe 与反查, 不重写事实层.

## 验收条件

- [ ] probe 封装 herdr/tmux pane 事实采集, 不含投递或存活策略. 消失、选项缺失、探测失败分类对标 `tests/test-kanban-probe.py`.
- [ ] liveness 提供 `check`/`subscribe` 共用分类器与 notify 共用的会话反查 (`herdr_reverse_lookup`/`tmux_reverse_lookup`).
- [ ] `check` 无参和 `--all` 探测全部 `working/` 卡; 定向只探测指定目标中的 `working/`, 跳过 `review/`. 存活错误折算 `unknown`.
- [ ] `subscribe` 校验任务组与成员后输出 `snapshot`/`state-change`/`heartbeat`; 默认 heartbeat 900s, refresh 1s. 有 `working/` 监控任务时 heartbeat 带 `liveness`. `--watch` 展开外部目标, 重复/空/不存在拒绝. JSON 字段与 onevoke 兼容.
- [ ] 测试覆盖 subscribe 的 snapshot/state-change/heartbeat/`--watch`, 以及 check 存活段不影响退出码.

## 威胁模型

只读探测. tmux 用户选项与 herdr `agent_session` 处于同用户权限内, 只用于避免误判, 不是安全边界. 不投递按键或消息.

## 不在本轮范围

- 既有问题: 排除 probe 之上的直投/忙/过期决策 (notify 卡).
- 并发/跨平台/安全加固: 纳入 no-follow 扫描边界; 排除写正文载荷.
- 共享契约与文档: 不改规则语义; JSON 事件字段保持 onevoke 形状 (组 id 字段名仍为规则已写的 `group_id` 等).
- 相邻功能: 排除 `notify`/`dismiss`/`start`. `check` 的结构/依赖部分已由 board 卡实现, 本卡只加存活段, 避免重写校验内核.

## 讨论与决策

```text
前置任务: 20260904-kander-launch-task
```

- 窗口地址格式由 launch 卡写入, 本卡按该格式解析; 兼认 onevoke 旧前缀.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-coord-task`, worktree `worktrees/20260904-kander-coord-task`, 基于当时组集成分支头创建.
- 新增 `internal/probe`: herdr `pane get` 与 tmux display/show-options/container 事实采集; pane_not_found 与 tmux gone 记消失; 选项缺失与探测失败分开; 双读 `@kander_session` / `@onevoke_session`. 不含投递或存活策略.
- 新增 `internal/liveness`: 四态分类器, `HerdrReverseLookup` / `TmuxReverseLookup` (兼容 7 列 onevoke 与 8 列双标记), `kander subscribe` JSON Lines, 以及覆写 `kander check` 在 board 结构校验之后追加只读存活段.
- 提交 `1278f91dba3e4263cb9333e6746a20e26f2562b0` (实现 probe/liveness 与 kander subscribe、check 存活段). 仓库无 `origin`, 已跳过 push; 已 rebase 到组分支最新头并 `git merge --ff-only` 进入 `group/20260904-migrate-to-go-group`.
- 验证: 在任务 worktree 模块根运行 `go test ./internal/probe ./internal/liveness` 与 `go test ./...`, rebase 后再跑一遍, 均通过. 测试用临时看板与假 tmux/herdr, 未改用户看板或 `$HOME` 配置.
- 批次 13 finding (base `47291317d7bf421d39b4f4acb99c2ed0c38f680b`, 当时 HEAD `1278f91dba3e4263cb9333e6746a20e26f2562b0`):
  - PM-1 / QA-2 unused `ReverseLookupStale`: 成立. 全库仅 `lookup.go` 定义, classify/subscribe 直接调用反查并把失败折成 stopped/unknown. 已删除. 同根因一次处理.
  - PM-2 / QA-1 unused `probe.t`: 成立. `internal/probe` 内 `t` 无调用点, `probeError` 已直接走 `config.LanguageText`. 已删除 `t`. 同根因一次处理.
  - PM-3 / QA-3 `--watch` 复制 board 成员解析: 核实成立 (liveness 自有 `groupMembers`/`taskGroupFrom`, board 同逻辑未导出). NON-BLOCKING 且 QA 标 recommend、PM 标 out-of-contract; 本轮不导出 board API, 避免改 board 卡冻结面. 建议后续或 notify 需要时再导出共用读取口.
- 修复提交 `c2036b8028704084c85c7bf10ba2237bc694a41d`; 已 rebase 到组头 `4ad6766` 并 ff 进 `group/20260904-migrate-to-go-group`. 无 origin, 未 push. 验证: `go test ./internal/probe ./internal/liveness` 通过.

## 完成总结

- 交付: `internal/probe` 与 `internal/liveness`; `kander check` 对 `working/` 输出存活段且不影响退出码; `kander subscribe` 输出 `snapshot`/`state-change`/`heartbeat` (heartbeat 可带 `liveness`), `--watch` 展开外部目标. notify 可复用 probe 与反查 API.
- 验收: 5/5; probe 消失/缺失/失败分类与双读覆盖; 反查唯一性与 herdr 反查后未知状态不标 drifted; check 跳过 review、探测失败折 unknown、未上报会话为 alive 且提示无法直投; subscribe 校验拒绝、snapshot/state-change/heartbeat/`--watch` 展开均有测试.
- 验证: `go test ./internal/probe ./internal/liveness` 通过; `go test ./...` 通过 (rebase 到组头后复跑通过).
- 审核: 批次 13 首轮 + 增量; PM/QA 通过. CSA/Hacker N/A. 修复一轮: 删除 `ReverseLookupStale` 与 `probe.t` (`c2036b8028704084c85c7bf10ba2237bc694a41d`).
- 收尾: 最终 commit `c2036b8028704084c85c7bf10ba2237bc694a41d`; 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回本地 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` (无 origin, 未 push). 记忆合并空操作退出 0; 已删本卡 worktree 与本地任务分支; 未删组分支. `kanban check 20260904-kander-coord-task` 通过: 1 个任务.

