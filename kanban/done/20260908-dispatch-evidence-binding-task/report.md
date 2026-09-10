# 派回证据绑定交付报告

最终交付 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`，分支 `dispatch-evidence-binding`，工作树 `/home/dualf/works/kander/worktrees/dispatch-evidence-binding`。来源组分支 `group/20260908-bindings-recovery-group`，最终基线 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。已推送任务分支；本轮只提交 review，PM/QA、组交付与 develop 集成由编排端执行。

## 实际变化

- fix 意图新增结构化 run/finding/batch/前驱绑定，消费 R 的原件、assignment、作者处置和 legacy 显式映射。创建与每次发送尝试都按任务 ID 重读验证；缺失、跨批、错误任务、前驱错误、过期轮次、缺副本均拒绝。
- 已有本卡作者原件按完整 lineage、原作者及相对 artifact ID 固定引用；不要求未来处置提前存在。同 ID 恢复不重绑后产生的记录，卡片 move 不使原件引用失效。
- wrap-up 以同 dispatch 的相对 integration 原件绑定封闭审核范围及实际 develop 集成。普通交付要求 source 等于封闭目标；已授权 rebase 可显式提供新基线，核对完整补丁，仅归一化 blob 哈希和行号，保留空白、上下文、模式与二进制内容。已清理 Git CWD 的旧派回仍可读取原授权/回执，不伪称重新核验 Git。
- 代收尾先在投递锁内对账，再核验新鲜 stopped 事实及双 revision。无意图先创建；unknown、非零、超时、租期到期、无 SESSION、仍活动均不能单独授权。已合法回收也要再次证明 stopped，并记录此前授权决定。事务保存旧 epoch、隔离旧写入后授予 wrap-up-only epoch，保留原意图期限。
- 专用 grant 仅允许受授权清理、接受/完成回执与追加记录；不允许普通正文/运行时身份改写、代码类文档写入、通知/启动、普通接管升级或原作者 disposition。记录保留原 OWNER 和作者历史，真实代办作者与原因在不可覆盖的 grant 中。
- 对齐已交付订阅接口：board 快照提供当前 epoch 的接受期限。订阅消费逻辑未增加新功能；既有协议用例改用 sync，wrap-up 夹具携带真实形状的审核/集成绑定。
- 中文仓库文档、AGENTS 包职责、英文发布规则、三语消息同步交付。board 无 review/notify/launch 反向依赖。

## 验收自检

以下为作者实现自检，不代替独立 PM/QA；冻结的 spec 契约和勾选状态未改写。

| 契约项 | 结果与证据 |
| --- | --- |
| 1：R 类型及作者绑定 | 已实现；合法首次 fix 无作者处置仍成功，已有作者需按原身份引用。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 2：发送前完整性与身份 | 已实现；缺 run/finding、错 batch/target/task/前驱、缺报告/作者原件均拒绝，未生成错误发送尝试。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 3：移动、同 ID、legacy | 已实现；review/working 移动后引用有效，重试不增 revision、不替换作者；仅显式 legacy 映射可消费。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 4：集成绑定 | 已实现；实际临时 Git 正反例覆盖正确祖先、无关 source/target/ref、合法 rebase 与空白差异拒绝；缺集成原件不能完成。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 5：对账与隔离 | 已实现；unknown/accepted 的并发回执使旧 CAS 失败，confirmed stopped 与合法回收记录可隔离旧 epoch；新意图与旧授权原件保留。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 6：四类事实及范围 | 已实现；无 SESSION、unknown、alive 均拒绝，仅 stopped 可授予；旧 epoch、普通写入、原作者结论入口、任意完成 SHA 均拒绝。仅追加收尾记录可成功完成。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |
| 7：交付验证与门禁 | 自动化检查通过，实机缺口已列明；PM/QA 待编排端组审核，CSA/Hacker 依本仓库 AGENTS.md 为 N/A。`6f5ed32072275c3bb9d96c9e40bd606b949b9c17` |

## 最终验证

全部日志见 [validation.txt](validation.txt)，统计含子用例。

- `go test -json -count=1 ./...`：19 包、974 测试项通过，1 跳过；`6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
- `go test -race -json -count=1 ./internal/board ./internal/launch ./internal/notify ./internal/liveness ./internal/review ./internal/window`：6 包、684 测试项通过，1 跳过；`6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
- `go vet ./...`、`go build ./...`、对全部修改 Go 文件运行 `gofmt -l`、`git diff --check 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa HEAD`：全部退出 0，格式与差异检查无输出；`6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
- 修改 25 个 Go 文件，最多 448 行；逐文件行数、冲突标记 0 个与人工检查记录见日志；`6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
- `GOOS=windows GOARCH=amd64 go build -o /tmp/kander-dispatch-binding-windows.exe ./cmd/kander`：退出 0，仅交叉构建；`6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
- `kander check 20260908-dispatch-evidence-binding-task`：1 个任务通过，实际会话 alive；执行工作树提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`，使用当前作用域已安装的 kander；仅检查真实卡片结构与会话，不作为新二进制测试。

## 交付历史与兼容

创建基线 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。首提交 rebase 到 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 时无冲突；已推送的任务分支按规则执行一次 force-with-lease，未改写组分支或 develop。最终三个提交：

- `135d5ca7c62e4cc9f4b87020ef66464ede91faa2`：审核引用与收尾专用授权。
- `71a6571c76be2e0f9f8a51e53c5ae6beeb1c6f11`：当前 epoch 期限与订阅夹具兼容。
- `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`：封闭审核范围与 rebase 补丁证明。

开发期出现的订阅夹具失败均因新 fix 要求绑定；通用协议用例已改为 sync，全部最终验证重新执行。没有把旧日志作为最终证据。

新 fix/wrap-up 需要 evidence；sync 与普通未绑定通知保留原协议。历史未绑定意图只允许读取/对账，不补造证据。工具不自动集成、不删除真实工作区，不扩大用户接管权限；fencing 约束受控入口，不能阻止绕过工具的任意本机代码/文件系统操作。

## 审核、未决与后续

PM/QA 尚未执行，由编排端接收后统一安排；本轮没有审核 run ID 或作者处置原件需要引用。CSA/Hacker 为 N/A，依据仓库 AGENTS.md 的明确特例。

- 原生 Windows 锁/DACL/reparse/进程行为未实测；交叉构建不能替代。
- 真实 tmux/herdr/Agent 投递与退出流程未实测；测试仅使用临时目录、假 CLI、真实临时 Git 和既有子进程 kill/restart 回归。
- 编排端仍须接收本分支、完成适用 PM/QA、闭批与授权集成，再派回收尾。任务 worktree、任务分支与交互会话保留。
- 当前没有已知未修复的实现缺陷。无法证明 Git 补丁一致、退出事实或原件归属的场景明确拒绝，不用人工描述代替机器证据。
