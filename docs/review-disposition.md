# 审核处置与完成门禁

本协议建立在 [审核证据归档](review-evidence.md) 的 run/batch 和 [卡片事务](card-transactions.md) 上。`internal/board` 定义共享模型、解析、受控发布与纯结构校验；`internal/review` 提供命令、Reviewer 提示与 Git 验证。board 不依赖 review。

## 执行周期与计划

活动卡进入 done 前必须具有显式 review plan。即使没有 REVIEWS 索引、所有角色都未运行，或者审核规则关闭，也不能用空索引推断通过。确实不适用时，四角色分别写 `N/A: <原因及规则依据>`，再显式闭批。工具记录调用者的适用性判断，不替代用户授权或项目规则判断。

```text
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review progress <absolute-CWD> <task-id>
```

单批计划示例，所有 SHA 都需换成真实完整提交：

```json
{
  "schema": 1,
  "sealed": true,
  "plan_id": "implementation-cycle",
  "author": "coordinator",
  "basis": "已确认任务及本仓库 AGENTS.md",
  "cwd": "/absolute/group-worktree",
  "report_language": "zh-CN",
  "task_ids": ["20260907-example-task"],
  "batches": [{
    "batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<full-base-sha>",
    "target_commit": "<full-target-sha>",
    "requirements": {
      "PM": "required",
      "QA": "required",
      "CSA": "N/A: 本仓库 AGENTS.md 安全角色例外",
      "Hacker": "N/A: 本仓库 AGENTS.md 安全角色例外"
    }
  }]
}
```

非 Git 项目且四角色全部明确 N/A 时，可将 base 与 target_commit 都写为 `N/A`。此时 close 保存 `git.not_applicable` 依据，不调用 Git、不声称提交或祖先关系已验证。只要任一角色 required，就必须使用真实完整 SHA。

工具生成 revision、recorded_at 和每成员的执行周期绑定；后者绑定任务 ID 与 STARTED_AT。任务计划指针和 tracked-cycles 位于稳定控制目录，卡片保存完整 `reviews/plan.json`。计划不能被新计划覆盖，也不能用换 plan ID 丢弃失败轮。tracked-cycles 必须与计划保存的周期一致，缺失或不一致属于结构错误。

`move <id> working --owner <agent>` 改变 STARTED_AT 后，整个计划返回 `requirements-needed`，progress 同时输出全部变化成员的 `rebind_cycles`。调用 `extend-plan` 显式携带当前 plan_id、expected_revision、author、basis 及这份完整 rebind_cycles，可原子重绑定全部成员的新周期。此操作只能恢复原计划，不能同时改批次或 seal，也不能替换角色要求；旧运行、失败轮、assignment、作者记录、闭批证据和其他成员状态全部保留。旧完整计划与请求按 revision 存入 plan-history；当前 reviews/plan.json 副本同步更新。已 sealed 的计划也可重绑定；同周期请求、漏成员或过时 revision 拒绝。已有失败仍须显式成功替代，已有未解决 finding 仍须处置，不能借重绑定跳过。

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "用户指定新 OWNER，保留整组全部审核义务",
  "rebind_cycles": {"20260907-example-task": "<progress 返回的当前周期摘要>"}
}
```

需要逐批调度时先用 `sealed: false`，列出整个周期的固定成员和首批。前批关闭后通过 extend-plan 追加下一批，不能修改既有批次或成员。示例：

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "前批已经关闭，接收下一批交付",
  "seal": true,
  "batch": {
    "batch_id": "batch-two",
    "previous_batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<previous-closed-target-sha>",
    "target_commit": "<next-target-sha>",
    "requirements": {
      "PM": "required", "QA": "required",
      "CSA": "N/A: 项目规则", "Hacker": "N/A: 项目规则"
    }
  }
}
```

也可以省略 batch，仅 seal。seal 要求所有成员已归入至少一个批次；未 seal、未分批成员或任一批未关闭均不能 done。扩展记录按 revision 留存，旧批结论不因计划追加而改写。多批静态计划也接受，但各后批 base 必须等于前批实际 closed target；如果前批尚可能修复，应使用追加协议，不预填猜测的最终 base。

旧 done/archived 卡没有计划时读取为 `legacy-untracked`，不补造 PASS。已有 tracked-cycles 却丢计划是错误。旧活动卡需要建立计划；启动、复审与收尾仍遵守原有执行授权。该计划不是后续 durable dispatch 的 execution epoch。

## 结构化 finding 与人工映射

每份新 Reviewer 提示要求一个独立围栏块：

````text
```kander-findings
{
  "FINDINGS": [{
    "id": "PM-01",
    "tier": "medium",
    "text": "完整 finding 原文",
    "evidence": "file.go:42；具体触发与影响"
  }],
  "NON_BLOCKING": []
}
```
````

两个数组必须存在。FINDINGS 仅接受 blocking/high/medium；NON_BLOCKING 仅接受 low/recommend/suggest。ID 在两数组间唯一；报告正文随意提到的 ID 不构成条目。缺 ID、重复 ID、重复 JSON key、未知字段、缺数组、重复围栏及不合法结构都不能参与有效闭批。新运行 sidecar 的 findings_schema 为 1；finalize 校验结构，非法报告以 execution_status=failed、非零退出及解析原因归档，原始 report/raw 均保留。execution_status=ok 仍不代表语义 PASS。

机械 finding 可附 `mechanical: documentation|dead-code|redundant-test`。增量中延续的 finding 用 `lineage: {"run_id":"上一轮 ID","finding_id":"上一轮条目 ID"}` 指向立即前驱，不能引用其他批次或正文提及的 ID。新 finding 不带 lineage。工具验证身份关系，不声称理解自然语言以自动识别重述。

旧 findings_schema=0 无结构报告必须通过 `review map-legacy <CWD> <JSON-file>` 显式映射，保存原件不改写。映射含 run_id、report_hash、author、basis、complete=true、findings 两数组，以及每项 finding_id/start_line/end_line/quote；行号从 1 开始，quote 必须与该原文范围逐字一致。空映射不得自动放行；新格式报告不能借 legacy 映射修补缺失结构，应使用同 batch 的新 run ID 重新进行完整审核，不把非法报告作为 --previous-run-id，并在 close 的 resolved_failures 中把原失败 ID 显式指向成功替代 ID。同 ID 重试只恢复原失败证据，不重跑 Reviewer。

## 归属与原作者记录

```text
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-record.json> <expected-card-revision>
kander review aggregate <absolute-CWD> <batch-id>
```

assignment 的 `run_id/batch_id/author/basis/items` 必填。items 的每个 key 为报告实际 finding ID，值为明确的 task ID 数组。跨卡 finding 列出全部归属；工具冻结这些卡当时的 OWNER。完整报告没有 finding 时，items 明确写 `{}`，没有作者记录要求。

```json
{
  "run_id": "pm-first", "batch_id": "batch-one",
  "author": "coordinator", "basis": "按任务修改范围归属",
  "items": {"PM-01": ["20260907-example-task"]}
}
```

作者记录示例：

```json
{
  "record_id": "pm01-author-first",
  "run_id": "pm-first", "finding_id": "PM-01", "batch_id": "batch-one",
  "task_id": "20260907-example-task", "author": "codex",
  "report_hash": "<report.md 的 SHA-256>",
  "original": "与条目 text 完全相同的原文",
  "status": "fixed", "basis": "返回目标源码与真实触发路径核实",
  "fix_commit": "<full-fix-sha>",
  "verification": "实际验证命令及结果"
}
```

记录由 working 卡的当前 OWNER 提交，只允许自身明确归属。assignment 保留最初 OWNER；合法接管后新 OWNER 可追加自己的结论，不能覆盖旧作者记录，旧 OWNER 不能再提交。命令验证当前 OWNER 与预期 revision，并生成 submitted_revision 和记录时间；旧记录没有 submitted_revision 时仍按原 assignment 作者绑定验证。后续更新新建 record_id，并以 previous_record_id 指向同 run/finding/task 上一记录；旧 JSON 不覆盖。受控 update 不能改这些附件。这里的作者声明是遵守协议的本机 Agent 身份记录，不是数字签名，不能阻止任意同用户进程冒名输入。

must-fix 状态为 confirmed/fixed/rejected/unverifiable/waived。confirmed 与 unverifiable 阻止闭批；rejected 必须写事实依据并进入未解决清单。fixed 必须有修复 SHA 和验证记录；该提交应严格晚于 finding 的目标提交。NON_BLOCKING 只接受 fixed/deferred/rejected，也必须记录依据。

waived 仅对 CSA/Hacker 的 must-fix 有效。waiver 包含 policy=accepted-risk 和明确 decision，或 policy=timed-out、完整通知依据 decision、sent_at 和 timeout_at；两时刻至少相差 15 分钟且 timeout_at 不在未来。它不等于 PASS。PM/QA、未验证项及普通接受确认不能使用该例外。实际通知、用户决定和适用规则仍由 Agent 如实执行并引用，不因字段存在而获得授权。

每条作者记录只发布到自己的 `reviews/<run_id>/dispositions/<record_id>.json`；工具控制 ledger 绑定原文并保留全链。编排者验证意见通过闭批 request 的 opinions 另外标 author/basis，可附 finding 引用，不能改写或替作者提交处置。

aggregate 读取整个批次所有去重 run、显式 assignment 与各作者记录，校验完整性后输出 JSON，并经同一可恢复事务发布到所有成员的 `reviews/batches/<batch_id>/disposition.json`。无 finding 的成员无须仅为抄写结论而 notify。多卡汇总发布未完成时，事务恢复门禁及副本核验阻止完成。

## 推进、增量与关闭

机械修复也要先推进批次目标，不需额外启动 Reviewer：

```text
kander review advance <absolute-CWD> <absolute-advance-request.json>
```

request 为 `{batch_id, expected_revision, advance}`，advance 沿用归档协议的 previous_target/target/reason/deliveries。advance、extend-plan 都要求调用 CWD 与计划 CWD 完全一致。review 验证干净 HEAD 和完整 Git 提交归属范围，board 在锁内 CAS；任何进行中、未完整发布的 run 或已闭批状态都拒绝推进。原 `--advance-file` 仍可与启动增量审核合并使用。

带 `--task --previous-run-id` 的增量审核自动加载前驱原报告、不可覆盖作者记录及工具生成的批次视图。可省略手工 review-context/reviewed-commit；显式 reviewed-commit 必须匹配。手工 review-context 作为独立补充逐字保留，不替代原件。工具使用明确字节长度分隔自动原件与补充文本；意图提交前只对自动来源重新校验，整体输入仍按原始字节冻结。重试传入不同补充文本会明确拒绝。未达到语义 PASS 的前轮可以复审；副本缺失、作者记录缺失、错误批次/成员/提交/语言则在启动前拒绝。意图提交时再校验上下文快照；同 run 重试用冻结输入，避免把之后的状态混进旧调用。

```text
kander review aggregate <CWD> <batch-id> > /tmp/batch-view.json
kander review close <CWD> <absolute-close-request.json>
```

close request 示例：

```json
{
  "batch_id": "batch-one",
  "expected_revision": 2,
  "view_hash": "<aggregate 输出原始字节的 SHA-256>",
  "author": "coordinator",
  "roles": {
    "PM": {"run_id":"pm-fixed", "passed_at":"<full-sha>", "basis":"逐项核实及跨角色影响检查"},
    "QA": {"run_id":"qa-first", "passed_at":"<full-sha>", "basis":"已验证结构与功能结论"}
  },
  "resolved_failures": {},
  "opinions": [{"author":"coordinator", "basis":"独立交付核验，不替代作者处置"}]
}
```

每个 required 角色必须选择合法完整的成功运行，并覆盖该角色的全部显式前驱链。不能选旧角色通过结果跳过后轮，不能以一个角色替代其他角色。失败尝试通过 resolved_failures 显式指向同角色成功替代运行，并验证提交关系；全部失败不能闭批。

非机械 must-fix 修复要求后继审核运行覆盖修复提交；仅机械 must-fix 时允许该角色 passed_at 前移至验证后的提交，并保留机械分类、实际修复及验证证据。机械 disposition 标签本身不构成豁免。close request 还须有独立 `mechanical` 数组，每项包含 record_id、finding（run_id/finding_id）、task_id、author（与 close author 相同）、category（与作者处置一致）、reported_category（与 Reviewer 原条目一致，缺标签时明确空字符串）、report_hash、fix_commit、basis、facts、paths、diff_hash。facts 要记录逐句比较、引用检索或保留测试覆盖等实际复核。paths 是排序去重的仓库相对文件路径；工具验证每个路径确实发生变化，并验证原被审提交至修复 SHA 的所列范围差异摘要，将相同绑定保存在 git.mechanical。主 Agent 可以按定义补认 Reviewer 漏标的机械项；不能把标签或哈希当语义证明。

计算 diff_hash 使用实际 Git 差异的 UTF-8 原始输出字节 SHA-256，参数为 `git diff --no-ext-diff --no-textconv --no-renames --binary --full-index --no-color <run-commit> <fix-commit> -- ':(literal)<path>' ...`。补充事实必须说明这些路径为什么完整覆盖本 finding 的机械修复；工具不执行自然语言语义 lint，也不以路径后缀推断代码无逻辑变化。没有独立判断、实际验证或匹配的 Git 证据，就应运行后继审核。

没有机械项，不得任意抬高 passed_at。所有通过提交、修复提交都需位于最终 target 的祖先链；修复不能指向原发现提交或无关分支。

close 在 review 层验证当前干净 HEAD、所有需要的 Git 对象/祖先关系，然后将 CWD、HEAD、时间及精确关系集合与当前批次 revision/view_hash 一并交给 board。board 再聚合真实原件，CAS 并发布 closed.json 和 disposition.json。closed 绑定最终 target，之后不能添加 run、修改作者记录或推进目标。后批必须引用前批 closed 的最终 target，不能引用某次旧角色 PASS，也不按时间戳推断。

`check` 与 `move done` 共用结构校验：合法待处置显示 pending/requirements-needed；已存在但损坏的原件、结构、归属和副本是错误。`review progress` 返回机器状态。done 还要求计划 sealed、全部批次 closed、所有副本及 Git 证据绑定一致。纯结构校验不再次运行 Git，不代表代码已集成；最终集成授权、实际交付、rebase 规则与 Git 核验继续按既有工作流执行。

所有证据和控制记录只保存在本机 kanban，不进 Git。原生 Windows 行为需在 Windows 验证；交叉编译不等于原生测试。
