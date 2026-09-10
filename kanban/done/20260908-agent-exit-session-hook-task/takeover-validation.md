# 接管验证记录

- 日期：2026-09-09；执行者：Codex；dispatch `2a6b7cee1fbd1e37058b1402903678b3`，epoch 2。
- 最终提交：`2d77411e634f7de764da00f1c3dff0203c1f3dfd`；文件树：`69aeb31beaf87ff22f75d7db2c58ff20b035783d`。
- 基线：组分支 `group/20260908-agent-definition-group`，`d97964c06adb942b94b25c7e5c5bfb04795172e4`。
- 原交付 `387f5918c340b307735612cba98373ce21e66de6` 无冲突 rebase 为 `52308bee6c1f457ede86be300f8aeaa2978d3159`；`git range-diff` 显示补丁相同。最终提交在此基础上修复接管发现的遗漏。

## 接管审计与回归

1. 原实现拒绝 Cursor 显式 `allocated` 配置，与冻结合同的既有模式兼容要求冲突。加入回归后，`go test ./internal/config -run 'TestDialectSessionCompatibility/cursor-allocated' -count=1` 先失败：`Dialect cursor cannot use session mode allocated without args templates`。改按钩子的预启动分配能力判断兼容性，保留 Cursor/Claude 的 allocator 覆盖；回归随后通过。没有恢复按 agent 名分支。
2. 原嵌入定义遇到未注册钩子只报 `Invalid embedded agent definition codex.json: session.mode`，缺少钩子名。新增回归先失败，修复后错误包含实际 session.mode，回归通过。
3. 原新增文档出现中文标题和说明，不符合仓库英文文档要求。已改为 `Hook Catalog` 与英文说明；漂移测试限定在该节的注册表条目内检查，避免其他段落提及名字却漏列清单。

## 构建与测试

- 提交前最终文件树运行 `go test -json ./... -count=1`：1438 pass / 0 fail / 1 skip；21 packages pass。之后仅提交这些已测文件，没有继续修改代码。唯一 skip 是当前 Linux 环境不执行的 `TestWindowsConsoleLauncher`。
- 在最终提交 `2d77411e634f7de764da00f1c3dff0203c1f3dfd` 上运行 `go build ./...`、`go vet ./...`、`GOOS=windows go build ./...`：全部退出 0。Windows 仅交叉构建，未声称原生运行通过。
- 在最终提交上运行 `go test -json ./internal/config ./internal/takeover ./internal/launch ./internal/notify ./internal/liveness -count=1`：561 pass / 1 fail / 1 skip；config、takeover、launch、notify 四包通过，liveness 失败。
- 失败为 `TestSubscriptionDispatchDeadlineDoesNotWaitForProbe` 在断言辅助进程事件日志时出现 `open /tmp/TestSubscriptionDispatchDeadlineDoesNotWaitForProbe2427955020/003/events: no such file or directory`。
- 最终提交定向重跑 `go test -json ./internal/liveness -run '^TestSubscriptionDispatchDeadlineDoesNotWaitForProbe$' -count=5`：5 pass / 0 fail。
- 最终提交整包重跑 `go test -json ./internal/liveness -count=1`：159 pass / 1 fail，同一测试同类缺失 events 错误。没有把失败记为通过。
- 从组基线 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 通过 `git archive` 提取到独立临时目录，原样运行 `go test -json ./internal/liveness -count=1`：160 pass / 0 fail。
- 仅在该临时基线副本中，在此测试准备 100ms 派发截止时间后插入 `time.Sleep(150 * time.Millisecond)`，定向运行同一测试，复现同类缺失 events 错误。这是人为调度延迟诊断，不是基线自然失败，也不计作验收测试。仓库测试未修改。
- 对照支持该测试依赖短截止窗口内辅助进程必定启动的时序假设；保留验证不稳定风险，交由组级审核评估，不宣称提交后全部复验通过。该记录不是 Reviewer run，没有产生审核 run。

## Delivery Self-Check

1. `git diff --check d97964c06adb942b94b25c7e5c5bfb04795172e4...2d77411e634f7de764da00f1c3dff0203c1f3dfd` 与工作区差异检查退出 0；无冲突标记、尾随空格或 EOF 漂移。
2. 逐个比较本卡触及的 Go 文件与基线：新增及修改文件均未超过 1000 行；最大修改文件 `internal/launch/session.go` 为 466 行。
3. 会话策略、退出命令、钩子清单及发布规则两处契约已同步；仓库文档英文，卡记录中文。
4. 按名退出表与 dialect/session 推导已删除；新增钩子能力、错误校验及克隆函数都有实际调用。
5. 新测试覆盖实际发现的兼容性与诊断缺口；清单测试检查正确章节。没有删除或跳过已有测试。
6. 最终提交上构建及四个相关包通过；liveness 的完整复验失败与单测重跑结果如上，明确保留验证缺口。
7. 全量测试的命令、次数、提交文件树与真实计数如上；未概括为所有复验都通过。

## 组级按名分支与 Git

- `rg -n '"(codex|claude|grok|cursor)"' internal --glob '*.go' --glob '!**/*_test.go'` 仅命中 `internal/review/settings.go` 两处默认 reviewer 字符串返回；不存在按内置 agent 名进行 switch/if 的业务分支。没有替前置卡修改 review 包。
- fetch 后，本地任务 HEAD 与 `origin/agent-exit-session-hook` 均为 `2d77411e634f7de764da00f1c3dff0203c1f3dfd`；远程组 HEAD 仍为 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，且 `git merge-base --is-ancestor d97964c06adb942b94b25c7e5c5bfb04795172e4 2d77411e634f7de764da00f1c3dff0203c1f3dfd` 退出 0。工作区干净。
- 已使用明确旧 SHA 的 `--force-with-lease` 更新本任务分支；没有修改组分支、develop 或其他卡分支。
- 前置卡已有交付 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 与 `b653e810e748783979daba1b6a21f200c143722f` 均在组分支中；接管期间前置卡进入各自 working 轮次，不能据旧记录替编排器确认其新轮完成。
- `kander review progress`：`agent-def-embed-cycle` pending，包含 `unsealed-plan`、`unassigned-member`、`agent-def-batch-one`。本卡尚未进入审核批次，未产生本卡 PM/QA run；CSA/Hacker 按项目规则 N/A。
