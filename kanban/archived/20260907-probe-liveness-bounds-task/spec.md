# Liveness classification fixes, probe budget and process cancellation

- TYPE: Bug
- SIZE: large
- TASK_GROUP: 20260907-orchestration-reliability-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 14:18
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT: cancelled

## GOAL

修复反查错误被归为 stopped，提供有整体预算和取消能力的存活事实采集。区分会话存在、运行状态和观测新鲜度，为派回恢复及订阅心跳提供可靠输入。

## USER_DECISIONS

本轮用户要求结合已讨论问题制定完整方案并建卡；本卡为技术拆分，未授权启动或切换任何现有 Agent。

## EXPECTED_OUTCOME

- 确认无匹配、唯一匹配、歧义和探测失败有明确结果；unknown 保留诊断，drifted 带新地址。
- 探测/反查接受 context 与预算，取消能结束后代进程和输出管道等待。
- alive 不再被文档解释为工作正在推进，调用方可识别 idle/blocked/done 和过期观测。

## ACCEPTANCE_CRITERIA

- [ ] herdr/tmux reverse lookup 使用类型化结果或错误，staleReport 仅在确认无匹配时据此 stopped；超时、命令失败、非法输出及多匹配返回 unknown，保留阶段和原因；现有 Codex 空引用不反查语义保留。
- [ ] Report/采集事实保留 NewWindow、observed_at、运行状态和观测有效性；alive 只表明匹配会话可探测，idle/blocked/done 不伪装成进展证据。直接投递就绪性仍由 notify 单独判断，不把 alive 等同 ready。
- [ ] context 贯穿 probe Capture、pane 探测、session 反查及 liveness 调用；一个任务的前向探测与反查共用剩余预算，整轮批量探测有总预算和并发上限，不按任务数无限累计默认超时。
- [ ] 保留现有便捷 API 的合理默认值，为 N/E 提供明确 context 入口；窗口/会话/agent 不匹配的既有分类测试保持语义，unknown 不写卡、不改变 check 的结构检查退出约定。
- [ ] 外部进程取消覆盖子进程及继承输出管道，设置有界输出等待/回收策略；不能只 cancel 父进程却永久等 pipe，也不能只丢弃 goroutine。POSIX 与 Windows 分别测试回收路径，说明不可中断系统 I/O 的实际边界。
- [ ] 将已复现的 list-panes 失败误报 stopped、后代输出拖过期限反转为回归；再覆盖歧义、慢反查、预算用尽、多任务并发上限、取消后无残留进程/goroutine。
- [ ] 更新 check/notify/subscribe 相关存活术语文档及三语错误，不修改用户接管授权。go test ./...、probe/liveness -race、build/vet/格式检查通过；真实 tmux/herdr 冒烟与假命令测试分开记录。

## THREAT_MODEL

防本机探测失败、挂起及错误恢复判断；进程输出视为不可信数据。不扩展到安全角色审核，也不自动终止用户现有终端以完成测试。

## OUT_OF_SCOPE

- 既有问题：反查错误吞掉和取消不完整在范围；新 Agent 后端或进程活动识别算法不做。
- 加固：预算、子进程/管道回收和新鲜度为故障边界所需；系统级进程监管服务排除。
- 共享契约与文档：probe/liveness 的结果与 context API 归本卡，notify 的业务状态归 N，调度/输出队列归 E。
- 相邻功能：不改任务卡状态，不自动接管或修复 WINDOW，不实现业务进展超时的消费策略。

## DISCUSSION

```text
PREREQUISITES: 20260907-review-archive-group
```

统一方案见 20260907-card-write-transactions-task 的 plan.md。跨组依赖要求前组 done 且交付实际进入 develop；不能只见前组 review 就启动。P/N/E/O 按依赖串行，避免 launch/notify/规则交叉修改被误承诺并行。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## RESPLIT_DECISION

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

本卡的大范围执行方式被替代，原目标由以下卡承接：P1 `20260907-probe-result-classification-task`, P2 `20260907-probe-cancel-deadline-task`, P3 `20260907-probe-batch-budget-task`.

完整验收映射与新的跨卡依赖见 20260907-probe-result-classification-task 的 resplit-plan.md；原验收、USER_DECISIONS及CARD_REVIEW留作历史，新卡不继承PASS。新卡全部backlog等待独立审卡；不要再start本卡或创建旧第二组分支。

## LIFECYCLE_DECISION

{"at":"2026-09-07 19:52","decision_reference":"用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。","duplicate_of":"","reason":"原大卡执行方式按用户要求拆分替代；需求完整保留，详见 20260907-probe-result-classification-task 的 resplit-plan.md"}
