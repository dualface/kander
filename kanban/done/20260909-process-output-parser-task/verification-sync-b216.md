# b216 基线同步验证

- 派发：`codex-agentdef-parser-sync-b216-20260909`，epoch 4，accepted receipt `replayed=false`，接受时卡片 revision 59。
- 任务工作树：`/home/dualf/works/kander/worktrees/20260909-process-output-parser`；接受前 HEAD `df567055d3d884c9acb4b2d5bec0a926b87b1a5a`，工作树干净。
- 已成功 fetch 并核实组基线 `b216ca25cb328300b3a45c54bc00a5ca442a89d7` 与派发一致；没有意外漂移。
- 首次 `git fetch origin` 退出 128：`GnuTLS, handshake failed: The TLS connection was non-properly terminated.` 随后仅 fetch 组与任务两个远端引用成功（退出 0）；成功后才 rebase，没有绕过 TLS 验证。
- `git rebase b216ca25cb328300b3a45c54bc00a5ca442a89d7`：无冲突；最终提交 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`。
- `git range-diff d97964c06adb942b94b25c7e5c5bfb04795172e4..df567055d3d884c9acb4b2d5bec0a926b87b1a5a b216ca25cb328300b3a45c54bc00a5ca442a89d7..e756f7d672bb4cc2a488a476485f6821e7b43d2e`：`1: df56705 = 1: e756f7d`。
- 对 rebase 前后 `git diff` 的原始字节作比较：完全相同；SHA-256 `7a3dee1b2d195d541580d1ab10ba73d6e8e2a6bb3e1a45a428bcc8f7fc28955e`。新提交相对组基线仍是 3 文件 +65/-3，没有加入额外改动。

## 最终提交验证

以下命令全部在 `e756f7d672bb4cc2a488a476485f6821e7b43d2e` 运行，退出码均为 0：

- `go test -json -p 2 ./... -count=1`：1480 pass / 0 fail / 1 skip（含子测试），21 包通过；process 74 pass / 0 fail。
- `go build ./...`。
- `go vet ./...`。
- `GOOS=windows go build ./...`；仅交叉编译，不声明 Windows 原生行为已经运行验证。

空 select 与 equals 校验的重点测试均通过：

- `TestValidateOutputRejectsIllegalCombinations`。
- `TestValidateConditionEqualsJSONValues`。
- `TestDecodeOutputSpecRejectsEmptySelectOnJSON`。
- `TestNDJSONAbsentIsPerLine`。

## Delivery Self-Check 1–7

1. `git diff --check b216ca25cb328300b3a45c54bc00a5ca442a89d7 e756f7d672bb4cc2a488a476485f6821e7b43d2e`：退出 0，无输出；`git status --porcelain` 为空。
2. 本轮仅重放既有一个提交，无新增文件或额外代码；原补丁逐字节不变，仍满足上一轮记录的 1000 行限制。
3. 文档与校验补丁一起完整重放；源文件和文档没有冲突处理或额外改写。
4. 没有新增函数、状态或死代码；沿用已验证的相同补丁。
5. 没有增加、删除或放宽测试；原回归测试全部保留。
6. 最终提交的直接受影响 process 测试已在全量运行中执行，74 项通过（含子测试）。
7. 全量结论绑定本节精确命令、提交与计数；跳过与历史失败不隐去。

## 历史失败保留

上一轮在 `df567055d3d884c9acb4b2d5bec0a926b87b1a5a` 默认并行全量测试中出现一次 `TestSubscriptionDispatchDeadlineDoesNotWaitForProbe` 的 events 临时文件缺失；当时单项 5 次与降低包并行度的全量复跑通过。该事实和原始失败输出继续保留在 [verification.md](verification.md)，未改写为从未失败。本轮从开始就使用 `-p 2`，没有删改失败用例，未复现该失败；根因仍未确定。

## 远端与看板

- 使用绑定旧远端 SHA `df567055d3d884c9acb4b2d5bec0a926b87b1a5a` 的 `--force-with-lease` 更新自己的任务分支成功。
- 推送后 `git ls-remote` 核实任务分支为 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`、组分支仍为 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`；组分支没有由本执行者修改。
- `kander check 20260909-process-output-parser-task`：退出 0，`ok: 1 tasks`。
- 旧批次关闭证据：`reviews/batches/agent-def-batch-one/closed.json`，target 为 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，PM 使用 `pm-agent-def-b1-r6`、QA 使用 `qa-agent-def-b1-r8`，closed_at 为 `2026-09-09T13:27:46.185711073Z`。旧审核范围不重开。
- 新交付等待编排器串行接收并安排后续批次；现有计划仍 unsealed。没有进入 done，没有清理任务分支或工作树。
