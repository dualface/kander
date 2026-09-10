# 规划依据与复现附件

本目录保存 2026-09-07 在 bcf8a07 上完成的只读分析和隔离复现。原仓库五包的 go test -race 通过；额外 13 个测试成功复现坏行为，尚未修复。

- [订阅链路分析](subscription-audit.md)
- [重复卡补充分析](duplicate-cards.md)
- [订阅复现源码](liveness-reproductions.go.txt)
- [探测期限复现源码](probe-reproductions.go.txt)
- [通知回显复现源码](notify-reproductions.go.txt)
- [旧路径回滚与护栏竞态复现源码](duplicate-reproductions.go.txt)

源码以 .go.txt 保存为调查证据，不加入仓库测试套件。它们断言“当前缺陷可以复现”；后续实现任务应反转断言验证修复，不能直接把这些测试通过当作实现验收通过。原分析中 /tmp 路径是当时环境记录；本目录附件才是本轮规划的持久副本。Windows 和真实 Agent 界面未在调查中实机验证。

正式方案见上级 [plan.md](../plan.md)，以八张当前 spec.md 的契约与独立卡审修正为准。
