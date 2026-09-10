本轮是同一批次失败首轮的重试，不是增量审核。tui-project-options-pm-1/qa-1、pm-2/qa-2、pm-3/qa-3 均因报告缺少 kander-findings 围栏而 execution_status=failed，不能作为 predecessor。批次运行时 target 已是 7c2e345c385be33087c3925ab4be18d11109a353。CSA/Hacker 按仓库 AGENTS.md 为 N/A。

审核焦点：对照任务契约核验收口、继承/覆盖/恢复、Global 与 Project 保存隔离、切 tab 不保存、真实点击与 TUI 同步、Project 审核角色重置不建立覆盖。

7c2e345 相对 47d5740 的修复：honor Options clicks、切 tab 时同步 TUI、overlay reviewer reset seeding。

验证记录：go test ./... -count=1 -timeout 240s 于 7c2e345：1304 pass / 0 fail / 1 skip / 21 packages。Delivery Self-Check 已记入卡片。

输出硬性要求：报告必须包含恰好一个名为 kander-findings 的围栏 JSON，对象含 FINDINGS 与 NON_BLOCKING 两个数组。空数组合法。不要把结论只写在摘要里而省略该围栏。
