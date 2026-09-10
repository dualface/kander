Review focus:
(1) 订阅事实（20260908-subscription-facts-task）：JSONL 新增 schema_version/subscription_id/seq/observed_at/task_revisions/task-update 是否与同一次协调读取绑定；组引用每次重新展开后成员增减（含跨组）是否完整、不把无法确定的成员当作已满足依赖；读取不完整是否终止告警并退出而不是静默降级；board 的 ScanContext/Revision/GroupMembership 与旧 Scan/ScanTargets 的兼容边界。
(2) 持久派回协议（20260908-durable-dispatch-protocol-task）：dispatch 意图按稳定 ID 的幂等创建与读取、accept/complete 的原子持久证明、过期 epoch 授权是否被拒绝；notify/resume 发送前绑定意图、不确定投递按同 ID 对账重试；终端回显不再等同 accepted 是否真的在实现里成立；未绑定的普通模式兼容是否保留。
(3) 订阅运行时有界（20260908-subscription-bounded-runtime-task）：存活采集与状态扫描解耦后，慢探测期间扫描与取消是否仍及时；stdout 有限队列与单 writer 的背压上限；消费者停读或取消时订阅能否退出且不残留 goroutine/进程；调用方 context、旧 stop、平台信号、输出失败四条路径的回收是否一致。
(4) 三卡的集成缝：本批 rebase 过程中 AGENTS.md 与 internal/i18n/locales/{en,ja,zh-CN}.json 有 4 处冲突由 bounded-runtime 作者逐块合并（保留上游派回索引/消息与本卡订阅索引/消息），请重点复核这些合并块是否有丢项、重复或与实现不符。
(5) 文档与注释是否与实现一致（docs/subscription-facts.md、AGENTS.md 索引、英文发布协议、三语消息）。

Verification records:
三张卡的执行者各自在其最终提交上实跑并记录：subscription-facts 003e5fecf4048d8da8151d0431ea1cd040912a36 上 go test -count=1 ./... exit=0（19 包、821 子用例通过、1 skip）与 board/fs/liveness/i18n 的 -race exit=0；durable-dispatch-protocol c5dcfbc401d5748da7eb0c89011ea422f51c33cc 上交付自检七项通过；subscription-bounded-runtime 在 rebase 后的 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 上重跑验证（首轮曾出现既有 TestBatchEarlierCallerDeadlineWins 计时断言失败，作者把计时起点提前而非放宽阈值）。主控将在本批 target 上另行实跑 build/vet/全量测试。

Environment gaps:
原生 Windows 与真实 tmux/herdr/Agent 未运行；交叉编译、假 CLI 与 Linux /proc 检查不代表这些实机环境通过。这属于已披露的验证缺口，不得据此单独判定 blocking/high/medium。

本批次说明：
base 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5 是组分支创建锚点（等于当时 develop），target ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 是三卡交付全部 ff 接收后的组分支 HEAD。三张卡的实现分别由三个执行 Agent（均为 codex）完成，主控未代改任何代码。这是本组第一批，无前轮 finding。