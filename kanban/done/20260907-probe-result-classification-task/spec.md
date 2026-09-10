# Reverse lookup failures report unknown explicitly

- TYPE: Bug
- SIZE: small
- TASK_GROUP: 20260907-runtime-observation-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 19:49
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tW:wX:p1E
- STARTED_AT: 2026-09-07 20:29
- FINISHED_AT: 2026-09-08 01:30
- TASK_BRANCH: probe-result-classification
- RESULT: completed

## GOAL

让探测失败不再被报告为会话已停止，避免消费端据此误恢复。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。


后续实际决定：用户说“拆好就开整吧”，并对本侧独立审卡及启动执行答复“我授权”。该授权取代上方建卡阶段不启动限制；按resplit-plan.md的组交付、审核、develop集成和收尾计划执行，既有审核归档组由原主线程负责。

## EXPECTED_OUTCOME

本卡交付：让探测失败不再被报告为会话已停止，避免消费端据此误恢复。

## ACCEPTANCE_CRITERIA

- [ ] herdr/tmux 反查明确区分零匹配、唯一匹配、歧义和错误；仅确认零匹配才据此 stopped，错误/非法输出/超时/多匹配为 unknown，保留原因与阶段。
- [ ] 唯一匹配返回 drifted/NewWindow；保留窗口、会话、Agent 不匹配及 Codex 空引用不反查的现有语义。unknown 不写卡、不改变 check 的结构退出约定。
- [ ] 反转 TestAuditReverseLookupErrorBecomesStopped，并覆盖零/一/多匹配；文档明确 alive 不是 ready 或进展，不改 notify 的独立就绪判断。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

internal/probe 与 internal/liveness 的反查结果及消费者；不加预算调度、不改通知恢复或WINDOW。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: N/A
```

依赖必要性与不可拆分理由：N/A；这项分类修复不需要审核归档、目录迁移或派回协议。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：internal/probe 与 internal/liveness 的反查结果及消费者；不加预算调度、不改通知恢复或WINDOW。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。

历史来源：20260907-probe-liveness-bounds-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。建卡时独立审卡待执行；现已获得下方真实CARD_REVIEW，通过后由主控pick/start。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已核对完整契约、粒度、必要依赖、覆盖及无环；本卡无阻断。原文记录：/home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/card-review-round1.md。仅契约审查，不代表实现或PM/QA通过。

执行接线：本侧主控负责20260907-runtime-observation-group，组工作区worktrees/20260907-runtime-observation-group，创建锚点4889d3fb93f8d2d639832b9da5588d668714d9bc。只从该组分支准备任务工作区。需要受控写卡时可使用已交付S/D/A构建的/tmp/kander-side-resplit-20260907/kander show --json / update --expect-revision；不要部署或init真实看板。安装命令当前仍按目录识别large，因此为旧门禁兼容保留report.md；SIZE: small及本卡单行为边界不变。

## IMPLEMENTATION

### 2026-09-08 首轮审核派回

最新交付 SHA / 组基线：96b59578954da4cc208684333f293d47bb4b2a0e。任务分支 probe-result-classification 已从 387d236 同步到当前组 HEAD 并正常推送，无新增提交，工作区干净。

QA-S2（suggest）作者结论 Rejected：复查 pane 消失、Agent/session 不匹配时仍 stopped、无新地址且缺反查阶段的现象已在五场景临时测试中核实；相关分支在审核 base 与当前 commit 逐字相同，冻结契约第 2 项要求保留身份不匹配语义，本轮不改。保留未采纳建议供用户复核；完整依据、命令、输出和临时测试源码见 review-round1-verification.md。QA-R1/S1 由 P2 处理，本卡不代改。

最终 HEAD 上 go test ./...、四包 race（liveness/probe/notify/takeover）、go build ./...、go vet ./...、git diff --check 和 gofmt 检查全部通过。主控提供的 PM（Codex）、QA（Grok）首轮无 gate；CSA/Hacker N/A。原始报告保存为 review-round1-qa-original.md、review-round1-pm-original.md，无新审核机器索引。原生 Windows 和真实 tmux/herdr/Agent 未验证。

### 首次交付历史记录

工作区：/home/dualf/works/kander/worktrees/probe-result-classification。
任务分支：probe-result-classification。
源分支：group/20260907-runtime-observation-group。
创建锚点：4889d3fb93f8d2d639832b9da5588d668714d9bc。
最终组基线：dadad01b66fc12cc34ebaceb81ccc3acb96424ac。
最终交付 SHA：387d2362ee91662ef3cc998116c68debf5e08854，已推送 origin/probe-result-classification，本地与远端跟踪引用一致，工作区干净。

反查以匹配数错误区分明确零匹配和歧义，采集/解析/超时失败保留为错误；staleReport 仅将明确零匹配据此报告 stopped，其余为 unknown 并保留原地址原因与反查阶段、原因。唯一匹配保留 drifted/NewWindow；函数签名及 notify/takeover 非 nil error 分支不变。未修改 notify 就绪判断、恢复或 WINDOW 写入。中文、英文、日文消息和 README/英文发布协议一并交付。

原审计测试未在基线源码中；按原审计记录重建 TestAuditReverseLookupErrorBecomesUnknown，在基线临时副本上 herdr/tmux 均实际误报 stopped 并使新断言失败，修复后通过。另覆盖零/一/多匹配、非法/不完整列表、超时、只读卡片、check 退出码、身份不匹配、Codex 空引用不反查及 busy/blocked 仍 alive。

最终 rebase 无冲突；其后 go test ./...、go test -race ./internal/liveness ./internal/probe ./internal/notify ./internal/takeover、go build ./...、go vet ./...、git diff --check 均通过，四个变更 Go 文件 gofmt -l 无输出。首次数字 ID 限制导致 notify 既有测试失败，已改为复用原格式并复测通过；一次 fetch TLS 失败，重试成功后完成最终 rebase。

PM/QA：待主控按组级门禁执行；CSA/Hacker：按仓库特例 N/A。Linux 临时目录及假 CLI 验证，不代表原生 Windows 或真实 tmux/herdr/Agent 已验证。完整证据及逐项验收见 report.md。

### 2026-09-08 集成后原作者收尾

最终交付 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 已在本地/远端 develop；fetch 后独立 rev-parse 一致。自身首次代码 387d236 映射 2eb6bbcd88acf88aad394da22323ffce5e765758，最新任务 96b5957 映射 06412f4f51faed0d1291e157824dfabf66cb8faa，两个映射和最终提交对本地/远端 develop 的祖先检查全部 exit=0。

本卡工作区、本地及远端 probe-result-classification 分支已删除；远端删除一次 TLS 失败后重试成功，最终 ls-remote 无匹配。未修改组工作区、组分支或主控临时集成 ref，未启动 P3/其他 todo，未 dismiss。首次实施和验证原文保留。

PM 首轮通过结论沿用，QA 首轮及集成夹具增量无门禁项，CSA/Hacker N/A；原文与映射证据见 [收尾通知](wrapup-notice-original.md)、[收尾核验](wrapup-verification.md)、[最终验证日志](final-validation.md)。QA-S2 suggest/Rejected 及两类实机验证缺口原样保留，Reviewer 未撤回 QA-S2。已使用受控 move --result completed 原子设置 RESULT 并迁移 done；随后定向 kander check 输出 ok: 1 tasks，exit=0。普通 update 不允许修改受管 RESULT，首次此类写入被拒且未落盘，改用上述专用入口完成。

## SUMMARY

实施、自动验证、适用审核、集成和本卡资源清理已完成；最终交付 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88，本地/远端 develop 一致，自身重写后提交已逐一验证。验收 4/4，原生 Windows 与真实终端/Agent 验证缺口按契约披露。QA-S2 suggest/Rejected 保持原作者完整理由，仍为未采纳建议；无新增门禁。done 后定向 kander check 通过（ok: 1 tasks）；完整八字段报告见 [report.md](report.md)。代码最终所在分支 develop；不改变原组关系，P3/其他 todo 保持暂停。
