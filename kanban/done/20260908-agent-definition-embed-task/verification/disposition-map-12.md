# 作者处置 Git 映射核验（epoch 12）

本轮 dispatch：`codex-agentdef-dispositions-embed-r2-20260909`；作者 codex。已取得 accepted 且 replayed=false。仅追加作者记录，不改代码、组分支或任务分支；最终交付保留 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`，待接收的新交付继续排队。

组审核目标：`d97964c06adb942b94b25c7e5c5bfb04795172e4`。逐条验证原 fix_commit 的 `git merge-base --is-ancestor <old> <target>` 退出 1，新 fix_commit 同命令退出 0；原 finding 审核提交亦为新 fix_commit 的祖先。逐条比对报告 SHA-256 与保留的 report_hash。不得以历史 rebase 文本代替该祖先检查。

## 新作者原件

- `reviews/pm-agent-def-b1-r4/dispositions/pm-01-git-map-codex-e12.json`；previous_record_id=`pm-01-fix-r1`；status=fixed；fix_commit=`77affddb46336b7b131c4d8ba9ac6f6c1e85d6f4`。
  独立逐行核验旧 be3f6c3 与现 77affdd 的 agent.go fail 条件和 durable blocked 测试；git patch-id --stable 均 cf44aaa9252e046a1d9569a84012a20c0031b4ef。pane 不再进入 DeliveryUnknown 短路，随后关闭容器；notify_resume.go/commands.go 的失败调用链恢复原文。相关生产文件由 77affdd 到组目标 d97964c 的 git diff 为空。 原记录中的 fix_commit 不是组目标祖先（git merge-base --is-ancestor 退出 1）；新 fix_commit 是组目标祖先（退出 0），并位于原审核提交之后。本次仅追加 Git 证据映射，原 status/original/report_hash/mechanical 与旧作者原件保留。
- `reviews/pm-agent-def-b1-r4/dispositions/pm-02-git-map-codex-e12.json`；previous_record_id=`pm-02-fix-r1`；status=fixed；fix_commit=`1f1924b7c991cf94ee0bb0bd6a221c73031d45a5`。
  独立核验 HasReviewTemplate 对内置名读取嵌入定义，对自定义名仅认可用户显式审核声明；path+dialect helper 从 ReviewAgentNames、Validate reviewer 及面板候选中排除。旧 eacc14c 与 1f1924b 的 git patch-id --stable 均 bf859c4d04dd1271543ddcbf33f04fc77d7ffd12，三处对应回归测试在该历史提交通过。组目标上的后续 allowReviewInherit 收紧保留此修复。 原记录中的 fix_commit 不是组目标祖先（git merge-base --is-ancestor 退出 1）；新 fix_commit 是组目标祖先（退出 0），并位于原审核提交之后。本次仅追加 Git 证据映射，原 status/original/report_hash/mechanical 与旧作者原件保留。
- `reviews/pm-agent-def-b1-r4/dispositions/pm-09-git-map-codex-e12.json`；previous_record_id=`pm-09-fix-r1`；status=fixed；fix_commit=`fbef4fa0100ea0417508a2e4f7c4c58ebaa34093`。
  独立核验 ReviewModelSupportsEffort 按 models.review.<agent> 是否存在 effort 键判定，menu.ReviewModelFieldsFor 使用该函数；cursor 的执行 args overlay 不产生审核 effort 输入。旧 3be9cd8 与 fbef4fa 的 git patch-id --stable 均 4f7d571bee5f40faba0354518d5966ed9b7c3517。相关生产文件由该提交到 d97964c 无差异。 原记录中的 fix_commit 不是组目标祖先（git merge-base --is-ancestor 退出 1）；新 fix_commit 是组目标祖先（退出 0），并位于原审核提交之后。本次仅追加 Git 证据映射，原 status/original/report_hash/mechanical 与旧作者原件保留。
- `reviews/pm-agent-def-b1-r6/dispositions/pm-10-git-map-codex-e12.json`；previous_record_id=`pm-10-fix-r1`；status=fixed；fix_commit=`d97964c06adb942b94b25c7e5c5bfb04795172e4`。
  独立对照旧 2361d6e、组内 b653e810e748783979daba1b6a21f200c143722f 与 d97964c：b653e81 已替换 docs/custom-agents.md:81 的错误继承承诺并删除不可达配置断言；d97964c 保留该文档与 A2 成对声明断言，补入本卡 dialect:claude 成对声明后 Env/Inspection/HomeEnv 不填充的断言。AgentFor 只在整块 nil 时填充，validateReviewDefinition 拒绝单项声明，文档和测试与真实行为一致。两条组内提交合起来包含旧修复的文档与测试效果，非声称 d97964c 单个补丁与旧提交逐字相同。 原记录中的 fix_commit 不是组目标祖先（git merge-base --is-ancestor 退出 1）；新 fix_commit 是组目标祖先（退出 0），并位于原审核提交之后。本次仅追加 Git 证据映射，原 status/original/report_hash/mechanical 与旧作者原件保留。
- `reviews/qa-agent-def-b1-r6/dispositions/qa-01-git-map-codex-e12.json`；previous_record_id=`qa-01-fix-r1`；status=fixed；fix_commit=`77affddb46336b7b131c4d8ba9ac6f6c1e85d6f4`。
  QA-01 与 PM-01 同根因。旧 QA 记录绑定的是包含修复的交付末端 3be9cd8；本次绑定实际引入 pane durable fail 修复的组内 77affdd，其补丁与原 PM-01 be3f6c3 的 patch-id 一致。独立确认 agent.go 的 Mode != pane 条件及调用方恢复原文路径；sendAttempted 的 argv 时点保持，但 pane 不据此返回 DeliveryUnknown。不是采用审核建议挪动时点的另一种实现。 原记录中的 fix_commit 不是组目标祖先（git merge-base --is-ancestor 退出 1）；新 fix_commit 是组目标祖先（退出 0），并位于原审核提交之后。本次仅追加 Git 证据映射，原 status/original/report_hash/mechanical 与旧作者原件保留。

## 历史提交实测

以下测试运行于对应 SHA 的独立临时 Git archive，未改组工作树，也没有把新交付测试混入旧审核目标。共 23 项测试及子测试通过，0 失败。

- 提交 `77affddb46336b7b131c4d8ba9ac6f6c1e85d6f4`：`go test -json ./internal/launch -run ^(TestPaneDeliveryDurableBlockedClosesTab|TestPaneDeliveryBlockedWinsBeforeTimeout|TestPaneDeliveryHerdrPromptRejected|TestPaneDeliveryReadyTimeout)$ -count=1`；退出 0，4 项通过。临时原始日志 `/tmp/kander-embed-map-77affdd.jsonl`。
  覆盖：`TestPaneDeliveryBlockedWinsBeforeTimeout`, `TestPaneDeliveryHerdrPromptRejected`, `TestPaneDeliveryReadyTimeout`, `TestPaneDeliveryDurableBlockedClosesTab`。
- 提交 `1f1924b7c991cf94ee0bb0bd6a221c73031d45a5`：`go test -json ./internal/config ./internal/menu -run ^(TestReviewAgentNamesFollowsDefinitions|TestCustomAgentsModelsAndRepair|TestDoctorAndPanelProbeAgentOverride)$ -count=1`；退出 0，3 项通过。临时原始日志 `/tmp/kander-embed-map-1f1924b.jsonl`。
  覆盖：`TestReviewAgentNamesFollowsDefinitions`, `TestCustomAgentsModelsAndRepair`, `TestDoctorAndPanelProbeAgentOverride`。
- 提交 `fbef4fa0100ea0417508a2e4f7c4c58ebaa34093`：`go test -json ./internal/config ./internal/menu -run ^(TestAgentSupportsEffortUsesDefinition|TestReviewModelFieldsHideCursorEffortAfterArgsOverlay)$ -count=1`；退出 0，2 项通过。临时原始日志 `/tmp/kander-embed-map-fbef4fa.jsonl`。
  覆盖：`TestAgentSupportsEffortUsesDefinition`, `TestReviewModelFieldsHideCursorEffortAfterArgsOverlay`。
- 提交 `d97964c06adb942b94b25c7e5c5bfb04795172e4`：`go test -json ./internal/config -run ^(TestReviewAgentNamesFollowsDefinitions|TestReviewTemplateValidation)$ -count=1`；退出 0，14 项通过。临时原始日志 `/tmp/kander-embed-map-d97964c.jsonl`。
  覆盖：`TestReviewTemplateValidation/pair-args-only`, `TestReviewTemplateValidation/pair-review-only`, `TestReviewTemplateValidation/cwd`, `TestReviewTemplateValidation/home-policy`, `TestReviewTemplateValidation/stderr`, `TestReviewTemplateValidation/empty-arg`, `TestReviewTemplateValidation/unknown-placeholder`, `TestReviewTemplateValidation/stdin-none-missing`, `TestReviewTemplateValidation/stdin-dup`, `TestReviewTemplateValidation/prompt-traverse`, `TestReviewTemplateValidation/prompt-unknown-ref`, `TestReviewTemplateValidation/ok`, `TestReviewTemplateValidation`, `TestReviewAgentNamesFollowsDefinitions`。

## 原件与交付保持

五份旧作者 JSON 的 SHA-256 在提交新处置后逐一比对不变。新记录保留 original、report_hash、mechanical（有则保留）及 fixed 状态，由本轮 codex 单独承担 basis/verification；每条均以 previous_record_id 连接原件，不冒签旧作者。

PM-01 与 QA-01 同根因，但身份分别保留。PM-10 的组内效果由 b653e81 的文档/不可达断言修正与 d97964c 的省略字段测试共同组成，未声称单提交补丁逐字等同。

此轮不触发审核、不聚合/关闭组批次、不进入 done、不清理任何任务分支或工作树。PM-06 原 rejected 结论及上轮报告的其他未解决项仍保留，本轮没有额外 finding。
