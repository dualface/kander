# Unify directory cards and SIZE with a recoverable migration on the transaction protocol

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tR:wX:p1A
- STARTED_AT: 2026-09-07 15:49
- FINISHED_AT: 2026-09-07 22:16
- TASK_BRANCH: directory-card-form
- RESULT: completed

## GOAL

保留 co-work 已确认的统一目录方案：所有新卡为 <task-id>/spec.md，规模由 SIZE: small|large 决定。将原卡不充分的“两次重命名即逐卡原子”方案改为消费 S 的互斥、事务记录和崩溃恢复，避免迁移瞬态打断订阅或被旧写入复活。

## USER_DECISIONS

2026-09-07，编排端通过通知 /tmp/kander-notify-c9b8429b41ee0c909d44a9f3bc89328e/message.txt 传达实际用户决定。用户原话：“就是 SIZE 要改, 相对链接也要改”。授权将原“正文只补 SIZE”改为“补写 SIZE，并调整保持引用目标所必需的相对链接地址”；保留其余正文、链接文字、附件及既有约束。

保留原决定：所有单卡和组成员卡统一目录；审核结果随卡保存且正文只留索引；原三卡建后不启动。本轮用户明确要求结合可靠性问题更新已有卡和补卡，允许调整本卡冻结契约及依赖。SIZE、init 与具体迁移协议是技术设计。

## EXPECTED_OUTCOME

- new 只建目录卡，small/large 的 Agent、模型、门禁及显示行为保持规模语义。
- init 显式、幂等、可恢复地迁移存量卡；并发读写有协调，不产生无主半迁移或重复入口。
- 过渡期只读兼容明确，活动执行端不得被静默迁移。

## ACCEPTANCE_CRITERIA

- [ ] 依赖 S 的事务接口和锁；移除原卡“不需要迁移互斥”的排除项。迁移前取得排他维护访问权；活动 working 默认拒绝迁移，明确列出停写/暂停执行端的前置条件；review 卡不得有在途通知/归档写入。保持既有卡与数据，不自动终止 Agent。
- [ ] kander new 默认创建 backlog/<id>/spec.md 并写 SIZE: small，--large 写 SIZE: large；SIZE 行在 TYPE 后。small 的 IMPLEMENTATION/SUMMARY、large 的 report.md 完成要求仍按 SIZE 分支。
- [ ] scan/Entry.Kind/TaskSummary.kind、list/TUI、launch/resume/notify 的 Agent/模型与 prompt、todo/done 门禁统一按 SIZE；仅接受 small/large。无 SIZE 的旧目录按 large 读取并由 check 提示修复，旧文件按 small 读取；非法 SIZE 阻止变更命令。
- [ ] large 或 TASK_GROUP 非空进入 todo 仍要求独立 CARD_REVIEW，非组 small 要求 SELF_REVIEW。SIZE 进入 todo 后冻结；形态与大小任务语义不混用。
- [ ] init 覆盖七状态，把旧 <id>.md 变为 <id>/spec.md，ID 不变、正文补写 SIZE 并调整保持引用目标所必需的相对链接地址，既有目录补 large；保留其余正文、链接文字及附属文件，按迁移前后路径映射保持引用目标；二次执行迁移数为 0 且内容/mtime 不变。
- [ ] 文件转目录使用 S 的持久事务记录、同卷暂存和恢复阶段；清楚承认发布前存在内部中间态。正常读者在锁下看到完整结果，进程中断后定向/全量检查能识别受管未完成事务，init 可恢复；不通过点前缀隐藏未完成操作，也不要求 scan 把合法事务产物当无效垃圾。
- [ ] 每个迁移阶段注入失败及 kill/restart，包括源移走、SIZE 补写、目标发布和提交记录；重跑后每 ID 唯一、spec/SIZE 完整，无内容丢失；冲突/无记录的异常产物保留并报错，禁止猜测删除。
- [ ] 迁移期间并发 init、move、update、归档与订阅读取被正确协调；已有 schema 兼容测试覆盖旧文件、缺 SIZE 目录及中英旧 token。维护窗口不以最终打印新路径替代。
- [ ] 过渡期 list/show/check/TUI/subscribe 可读取旧文件；变更命令在 D 发布后对未迁移文件明确提示 init，副作用前失败；读命令不触发批量迁移。check 的状态范围不扩张。
- [ ] guard-write 对目录内部路径和同状态旧 .md 拼写给出准确提示；不声称消除外部工具检查/写入竞态；统一采用 S 的更新入口。
- [ ] 规则/模板/README/AGENTS/docs 与 new/init/SIZE/旧卡过渡同步；配置键保持原名。go build ./...、go test ./...、go vet ./...、格式与差异检查通过；POSIX/Windows 迁移测试覆盖各自句柄约束。

## THREAT_MODEL

迁移处理全部用户看板。依赖 S 的互斥、事务与 internal/fs no-follow/固定句柄；防并发丢失、崩溃半成品及 reparse 越界。直接绕过协议的旧 Agent 写入通过明确维护窗口避免，不宣称工具锁能约束任意写进程。

## OUT_OF_SCOPE

- 既有问题：迁移竞态、半成品误报、旧路径写入与规模耦合在范围；check 原有默认状态范围不改。
- 加固：迁移互斥和恢复是必要范围；自动迁移、doctor 迁移和删除未知遗留物排除。
- 共享契约与文档：SIZE/形态/过渡读取归本卡；通用事务归 S，reviews 归 A，禁止重做另一套锁。
- 相邻功能：不做审核结论、派回或 TUI 新功能，不移除旧卡只读兼容，不改 models/kanban_agents schema。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task
```

统一方案通过 kander show 20260907-card-write-transactions-task 定位 plan.md。原技术建议存在两次 rename 之间不可见却声称全程二选一完整状态的矛盾，本次用协调读写+恢复记录修正。原审卡结论仅适用于旧版本，原文保存在 planning-history/20260907-before-integration.md，本版重新独立审卡。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## IMPLEMENTATION

- 本次非阻断处置最终交付 `839f72119b22ce8b48811fc9819de9643a4028f8`；组基线 `83f73544ef1f612a97f99d384e9386eeecb78752`。当前 SUMMARY/report.md 优先于下方历史轮次记录。

- 新契约首轮修复最终交付 `83f73544ef1f612a97f99d384e9386eeecb78752`；组基线 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`。本条及当前 SUMMARY/report.md 优先于下方旧轮次记录；历史完整保留。

- 本次修订最终交付 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`，组基线 `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`；以下旧提交记录为历史，当前结果以 SUMMARY/report.md 为准。

- 本次已获实际链接调整授权，解除策略阻塞。计划见 [链接修订计划](link-migration-plan.md)；旧首轮处置与原交付报告作为历史保留。

- 工作树：`/home/dualf/works/kander/worktrees/directory-card-form`；任务分支 `directory-card-form`。
- 来源与已验证前置交付：`origin/group/20260907-review-archive-group`，`a7fe54beb6ade00678e14655c0d385115eb950c8`；S 已处于 review，最新交付包含于本地及远端组分支。
- 方案：Entry.Kind 专用于规模，存储形态独立判断；扩展现有持久事务记录完成目录迁移。init 取得排他维护锁；涉及 working/review 时要求显式确认全部执行端及外部写入已暂停，不自动停止 Agent。
- 本轮仅交付任务分支并进入 review；组审核、集成与收尾由编排端负责。

- 实现与自检已完成，逐项结论、验证、环境缺口见 [交付报告](report.md)。
- 最终交付：`70da00edfff664ec1c0740a49f6ea3ae51b42c8c`；本地与远端 `directory-card-form` 一致，已正常推送。
- 最新组基线：`a7fe54beb6ade00678e14655c0d385115eb950c8`；fetch/rebase 无变化，随后全量测试再次通过。
- 全量测试、build、vet、七包 race、格式与差异检查通过；Windows 七包测试程序与完整二进制交叉编译通过，原生执行缺口保留。
- 记录使用已接收 S 基线构建的受控 show/update/move 入口；未部署二进制或迁移真实看板。

## REVIEW_ROUND_1

首轮 PM-001、PM-002、QA-001 均已独立复现。原文见 [首轮 finding](review-round-1-findings.md)，作者逐项判断与处置见 [首轮处置](review-round-1.md)。PM-001 已修复；PM-002/QA-001 已获用户实际授权，已按完整路径映射修复并自测。三项处置与验证见首轮处置末节，待新契约审核。先前 report.md 是原交付快照，其“未发现其他缺陷”结论由本轮复现纠正。

本轮修复最终提交：`29ada01a241bb7a7734972a913e22f3dad52756c`；组基线 `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`，fetch/rebase 后全量测试通过，本地与远端任务分支一致、工作树干净。执行端未启动复审或修改组分支。

## SUMMARY

本卡最终 `839f72119b22ce8b48811fc9819de9643a4028f8` 已无重写地随组 `251f5d89186730ea372053a401136030710efe84` 进入 origin/develop 与本地 develop；执行端独立 fetch/祖先验证通过。自身工作树、本地及远端 task 分支已清理，主与组工作区保留。审核第二批旧契约首轮失效保留，新契约 Claude 首轮 d93f0b6 后经 83f7354 增量无 gate，最终非阻断提交承接闭批结论；不冒称 Reviewer 审过最后提交。CSA/Hacker N/A。QA low、PM-106/109 保留判断及被拒绝的 Reviewer 断言反证全部保留；Windows 原生未执行，自检 10/11 完整。详见 [最终报告](report.md)、[收尾证据](wrapup.md)。按明确授权完成本卡收尾，交互 CLI/终端保持，未部署或迁移真实看板。

## CONTRACT_DECISIONS

```json
{
  "at": "2026-09-07 16:57",
  "decision": "2026-09-07，编排端通过通知 /tmp/kander-notify-c9b8429b41ee0c909d44a9f3bc89328e/message.txt 传达实际用户决定。用户原话：“就是 SIZE 要改, 相对链接也要改”。授权将原“正文只补 SIZE”改为“补写 SIZE，并调整保持引用目标所必需的相对链接地址”；保留其余正文、链接文字、附件及既有约束。",
  "Before": {
    "ACCEPTANCE_CRITERIA": "- [ ] 依赖 S 的事务接口和锁；移除原卡“不需要迁移互斥”的排除项。迁移前取得排他维护访问权；活动 working 默认拒绝迁移，明确列出停写/暂停执行端的前置条件；review 卡不得有在途通知/归档写入。保持既有卡与数据，不自动终止 Agent。\n- [ ] kander new 默认创建 backlog/\u003cid\u003e/spec.md 并写 SIZE: small，--large 写 SIZE: large；SIZE 行在 TYPE 后。small 的 IMPLEMENTATION/SUMMARY、large 的 report.md 完成要求仍按 SIZE 分支。\n- [ ] scan/Entry.Kind/TaskSummary.kind、list/TUI、launch/resume/notify 的 Agent/模型与 prompt、todo/done 门禁统一按 SIZE；仅接受 small/large。无 SIZE 的旧目录按 large 读取并由 check 提示修复，旧文件按 small 读取；非法 SIZE 阻止变更命令。\n- [ ] large 或 TASK_GROUP 非空进入 todo 仍要求独立 CARD_REVIEW，非组 small 要求 SELF_REVIEW。SIZE 进入 todo 后冻结；形态与大小任务语义不混用。\n- [ ] init 覆盖七状态，把旧 \u003cid\u003e.md 变为 \u003cid\u003e/spec.md，ID 不变、正文只补 SIZE，既有目录补 large；附属文件及相对链接保留；二次执行迁移数为 0 且内容/mtime 不变。\n- [ ] 文件转目录使用 S 的持久事务记录、同卷暂存和恢复阶段；清楚承认发布前存在内部中间态。正常读者在锁下看到完整结果，进程中断后定向/全量检查能识别受管未完成事务，init 可恢复；不通过点前缀隐藏未完成操作，也不要求 scan 把合法事务产物当无效垃圾。\n- [ ] 每个迁移阶段注入失败及 kill/restart，包括源移走、SIZE 补写、目标发布和提交记录；重跑后每 ID 唯一、spec/SIZE 完整，无内容丢失；冲突/无记录的异常产物保留并报错，禁止猜测删除。\n- [ ] 迁移期间并发 init、move、update、归档与订阅读取被正确协调；已有 schema 兼容测试覆盖旧文件、缺 SIZE 目录及中英旧 token。维护窗口不以最终打印新路径替代。\n- [ ] 过渡期 list/show/check/TUI/subscribe 可读取旧文件；变更命令在 D 发布后对未迁移文件明确提示 init，副作用前失败；读命令不触发批量迁移。check 的状态范围不扩张。\n- [ ] guard-write 对目录内部路径和同状态旧 .md 拼写给出准确提示；不声称消除外部工具检查/写入竞态；统一采用 S 的更新入口。\n- [ ] 规则/模板/README/AGENTS/docs 与 new/init/SIZE/旧卡过渡同步；配置键保持原名。go build ./...、go test ./...、go vet ./...、格式与差异检查通过；POSIX/Windows 迁移测试覆盖各自句柄约束。",
    "EXPECTED_OUTCOME": "- new 只建目录卡，small/large 的 Agent、模型、门禁及显示行为保持规模语义。\n- init 显式、幂等、可恢复地迁移存量卡；并发读写有协调，不产生无主半迁移或重复入口。\n- 过渡期只读兼容明确，活动执行端不得被静默迁移。",
    "GOAL": "保留 co-work 已确认的统一目录方案：所有新卡为 \u003ctask-id\u003e/spec.md，规模由 SIZE: small|large 决定。将原卡不充分的“两次重命名即逐卡原子”方案改为消费 S 的互斥、事务记录和崩溃恢复，避免迁移瞬态打断订阅或被旧写入复活。",
    "OUT_OF_SCOPE": "- 既有问题：迁移竞态、半成品误报、旧路径写入与规模耦合在范围；check 原有默认状态范围不改。\n- 加固：迁移互斥和恢复是必要范围；自动迁移、doctor 迁移和删除未知遗留物排除。\n- 共享契约与文档：SIZE/形态/过渡读取归本卡；通用事务归 S，reviews 归 A，禁止重做另一套锁。\n- 相邻功能：不做审核结论、派回或 TUI 新功能，不移除旧卡只读兼容，不改 models/kanban_agents schema。",
    "PREREQUISITES": "PREREQUISITES: 20260907-card-write-transactions-task",
    "SIZE": "",
    "TASK_GROUP": "20260907-review-archive-group",
    "USER_DECISIONS": "保留原决定：所有单卡和组成员卡统一目录；审核结果随卡保存且正文只留索引；原三卡建后不启动。本轮用户明确要求结合可靠性问题更新已有卡和补卡，允许调整本卡冻结契约及依赖。SIZE、init 与具体迁移协议是技术设计。"
  },
  "After": {
    "ACCEPTANCE_CRITERIA": "- [ ] 依赖 S 的事务接口和锁；移除原卡“不需要迁移互斥”的排除项。迁移前取得排他维护访问权；活动 working 默认拒绝迁移，明确列出停写/暂停执行端的前置条件；review 卡不得有在途通知/归档写入。保持既有卡与数据，不自动终止 Agent。\n- [ ] kander new 默认创建 backlog/\u003cid\u003e/spec.md 并写 SIZE: small，--large 写 SIZE: large；SIZE 行在 TYPE 后。small 的 IMPLEMENTATION/SUMMARY、large 的 report.md 完成要求仍按 SIZE 分支。\n- [ ] scan/Entry.Kind/TaskSummary.kind、list/TUI、launch/resume/notify 的 Agent/模型与 prompt、todo/done 门禁统一按 SIZE；仅接受 small/large。无 SIZE 的旧目录按 large 读取并由 check 提示修复，旧文件按 small 读取；非法 SIZE 阻止变更命令。\n- [ ] large 或 TASK_GROUP 非空进入 todo 仍要求独立 CARD_REVIEW，非组 small 要求 SELF_REVIEW。SIZE 进入 todo 后冻结；形态与大小任务语义不混用。\n- [ ] init 覆盖七状态，把旧 \u003cid\u003e.md 变为 \u003cid\u003e/spec.md，ID 不变、正文补写 SIZE 并调整保持引用目标所必需的相对链接地址，既有目录补 large；保留其余正文、链接文字及附属文件，按迁移前后路径映射保持引用目标；二次执行迁移数为 0 且内容/mtime 不变。\n- [ ] 文件转目录使用 S 的持久事务记录、同卷暂存和恢复阶段；清楚承认发布前存在内部中间态。正常读者在锁下看到完整结果，进程中断后定向/全量检查能识别受管未完成事务，init 可恢复；不通过点前缀隐藏未完成操作，也不要求 scan 把合法事务产物当无效垃圾。\n- [ ] 每个迁移阶段注入失败及 kill/restart，包括源移走、SIZE 补写、目标发布和提交记录；重跑后每 ID 唯一、spec/SIZE 完整，无内容丢失；冲突/无记录的异常产物保留并报错，禁止猜测删除。\n- [ ] 迁移期间并发 init、move、update、归档与订阅读取被正确协调；已有 schema 兼容测试覆盖旧文件、缺 SIZE 目录及中英旧 token。维护窗口不以最终打印新路径替代。\n- [ ] 过渡期 list/show/check/TUI/subscribe 可读取旧文件；变更命令在 D 发布后对未迁移文件明确提示 init，副作用前失败；读命令不触发批量迁移。check 的状态范围不扩张。\n- [ ] guard-write 对目录内部路径和同状态旧 .md 拼写给出准确提示；不声称消除外部工具检查/写入竞态；统一采用 S 的更新入口。\n- [ ] 规则/模板/README/AGENTS/docs 与 new/init/SIZE/旧卡过渡同步；配置键保持原名。go build ./...、go test ./...、go vet ./...、格式与差异检查通过；POSIX/Windows 迁移测试覆盖各自句柄约束。",
    "EXPECTED_OUTCOME": "- new 只建目录卡，small/large 的 Agent、模型、门禁及显示行为保持规模语义。\n- init 显式、幂等、可恢复地迁移存量卡；并发读写有协调，不产生无主半迁移或重复入口。\n- 过渡期只读兼容明确，活动执行端不得被静默迁移。",
    "GOAL": "保留 co-work 已确认的统一目录方案：所有新卡为 \u003ctask-id\u003e/spec.md，规模由 SIZE: small|large 决定。将原卡不充分的“两次重命名即逐卡原子”方案改为消费 S 的互斥、事务记录和崩溃恢复，避免迁移瞬态打断订阅或被旧写入复活。",
    "OUT_OF_SCOPE": "- 既有问题：迁移竞态、半成品误报、旧路径写入与规模耦合在范围；check 原有默认状态范围不改。\n- 加固：迁移互斥和恢复是必要范围；自动迁移、doctor 迁移和删除未知遗留物排除。\n- 共享契约与文档：SIZE/形态/过渡读取归本卡；通用事务归 S，reviews 归 A，禁止重做另一套锁。\n- 相邻功能：不做审核结论、派回或 TUI 新功能，不移除旧卡只读兼容，不改 models/kanban_agents schema。",
    "PREREQUISITES": "PREREQUISITES: 20260907-card-write-transactions-task",
    "SIZE": "",
    "TASK_GROUP": "20260907-review-archive-group",
    "USER_DECISIONS": "2026-09-07，编排端通过通知 /tmp/kander-notify-c9b8429b41ee0c909d44a9f3bc89328e/message.txt 传达实际用户决定。用户原话：“就是 SIZE 要改, 相对链接也要改”。授权将原“正文只补 SIZE”改为“补写 SIZE，并调整保持引用目标所必需的相对链接地址”；保留其余正文、链接文字、附件及既有约束。\n\n保留原决定：所有单卡和组成员卡统一目录；审核结果随卡保存且正文只留索引；原三卡建后不启动。本轮用户明确要求结合可靠性问题更新已有卡和补卡，允许调整本卡冻结契约及依赖。SIZE、init 与具体迁移协议是技术设计。"
  }
}
```
