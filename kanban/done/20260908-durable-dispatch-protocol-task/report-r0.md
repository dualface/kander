# 持久派回协议交付报告

任务：20260908-durable-dispatch-protocol-task。本轮实现交付，申请进入 review；最终验收与集成由主控负责。

最终提交：`c5dcfbc401d5748da7eb0c89011ea422f51c33cc`；分支 `durable-dispatch-protocol` 已推送同名远端。组基线 `003e5fecf4048d8da8151d0431ea1cd040912a36`；创建时基线为 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`。工作区 `/home/dualf/works/kander/worktrees/durable-dispatch-protocol` 保留。提交包含最新组分支的订阅事实变更；rebase 后把执行授权游标接入 context 扫描，并增加回归，未替换组成员的实现。

## 交付行为

派回身份和确认期限持久保存；接受/完成由受控 move 与卡片状态同事务提交。重复投递先查业务回执；终端提示回显不再被当成开工。接管轮换 epoch，拒绝旧正文/WINDOW/处置/回执写入；不能确定是否已发送时保留执行资源并对账。具体接口、存储结构、兼容和维护恢复见仓库 `docs/durable-dispatch.md`。

## 逐项作者自验

1. board 已定义稳定 ID、任务、三种 kind、消息哈希、相对引用、时间/期限、revision 与六种状态；见 internal/board/dispatch.go。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

2. 受控创建/读取复用 S 事务；同 ID 同输入幂等，跨任务/消息/基线冲突拒绝；发送前生成 ID，重试保留原期限。意图测试及创建 kill/restart 覆盖。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

3. move working 原子接受；move review/done 原子保存状态、revision、交付 SHA 及处置引用；重复接受/完成返回原件。快速回合及 done 恢复测试覆盖。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

4. 单卡单授权，受控正文、WINDOW、作者处置和回执执行 epoch/CAS 检查；显式接管轮换 epoch。锁仅覆盖投递操作，不占据 Agent 会话；未绑定历史不补回执。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

5. 重复/冲突/旧 epoch、28 个事务中断恢复子场景、快速 review-working-review/done、并发正文/WINDOW 保留均有行为断言。保证限于受控入口，不承诺任意外部副作用 exactly-once。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

6. notify/actionable resume 发送前 prepared，再进入 delivery-unknown；同 ID 先查 accepted/completed；unknown/busy/失效身份不恢复第二执行者。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

7. 提示文本使用带 ID/epoch 的受控 move；回显仅诊断。generic working/无组旧模式保留，绑定卡禁止通过旧恢复路径绕过授权；英文规则及三语消息同步。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

8. 消费 P3 context 预算和身份有效性，只有可靠 stopped 可恢复；显式 --agent 沿用用户授权接管语义。旧游标拒绝写回，可能已发送的启动失败保留窗口、WINDOW 和载荷。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

9. PromptEchoDoesNotCountAsAcknowledgement 反转旧缺陷；发送前后 kill、早到回执、同 ID 对账、短调用预算不重置持久期限、失败回滚与 move 并发有回归。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

10. 代码、必要文档和三语资源已交付；最终全量、race、build/vet/格式/交叉编译通过。原生平台缺口已披露，PM/QA 待主控运行，CSA/Hacker N/A；本项不声称审核门禁已关闭。（最终提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`）

## 最终验证

`go test -json -count=1 ./...`：19 包、877 个测试/子测试通过，1 项跳过，0 失败；提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`。

`go test -race -json -count=1 ./internal/board ./internal/launch ./internal/notify ./internal/window ./internal/cli ./internal/i18n ./internal/liveness ./internal/probe ./internal/review`：9 包、622 个测试/子测试通过，1 项跳过，0 失败；提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`。

`go build ./...`、`go vet ./...`、Windows amd64 全包交叉构建及 board/notify/launch 测试二进制交叉编译、`git diff --check`、28 个 Go 文件 gofmt 均通过；提交 `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`。命令和逐包输出摘要见本卡 `validation.txt`。唯一跳过项为 Windows 控制台测试，不计作通过。

## 审核与边界

PM/QA 适用，尚未执行，由主控安排；本轮无实现审核 run ID。CSA/Hacker 按仓库特例 N/A。原有 CARD_REVIEW 仅为契约审查，不代表本实现通过。

原生 Windows 及真实 tmux/herdr/Agent 未运行，不能以假 CLI、Linux 进程测试或交叉编译冒充。wrap-up-only 代办授权、R finding 语义绑定、订阅 dispatch 事实、编排检查点/恢复仍由后继卡交付。不会为任意外部 Git/网络/文件编辑副作用保证 exactly-once。

中间 rebase 冲突处理脚本曾使 JSON/Go 校验失败，已修复并在最终提交重跑验证；详见 validation.txt。现无已知未修复实现缺陷，仍待独立审核及上述实机证据。

本轮只推送任务分支并移交 review，不集成组分支/develop、不清理工作区或分支、不结束 Agent 容器。遵照启动任务文件，移卡后结束本轮等待主控派回。
