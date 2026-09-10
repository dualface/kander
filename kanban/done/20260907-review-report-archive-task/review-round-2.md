# A 增量审核非阻断处置

任务：20260907-review-report-archive-task。作者：原执行端 Codex。完整通知和 QA 增量原报告见 [原文](review-round-2-original.md)；本文是作者核实记录。

## 结论及交付

- 本轮仅修改发布规则的一处措辞；运行时代码、测试代码及托管区保护逻辑均无交付变更。
- 完整交付 SHA：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；本地及远端任务分支一致。
- 最新组 HEAD / 本轮起点：`e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`。fetch 成功，对齐该组 HEAD 的 rebase 无冲突、没有重写提交。
- 固定审核 base 仍为 `839f72119b22ce8b48811fc9819de9643a4028f8`；QA 增量输入 target 为 `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`，reviewed-commit 为 `4b0a3971b08e1a6c0b42451c50b14373de61ce03`。
- 按本轮通知事实：QA 增量 exit=0、无 gate，QA-01 至 QA-08 全部闭合；主控对 PM001/PM002 机械修正完成核对并实际运行推进测试通过，PM 未重跑。本执行端没有重新审核，已通过结论的承接与闭批由主控处理。

## QA-09 suggest：核实成立，保留当前策略

`appendReviewIndex` 已有区段取标题行尾；新建区段遇正文任一 CRLF 则选 CRLF。混合行尾可能出现，索引仍能解析，归档完整性检查仍通过。这是格式取舍，未发现数据损失、重复索引或执行状态回归。

临时隔离探针通过真实 Prepare/Finalize/Publish、UpdateDocument 和 CheckBoard 核实：

| 场景 | 托管正文内部 CR 数 | 整卡转 LF 的受控 update |
| --- | --- | --- |
| LF 卡首次发布后只将 REVIEWS 标题改 CRLF，再发布第 2 个 run | 1 | 拒绝修改 REVIEWS |
| 同一路径发布至第 3 个 run | 2 | 拒绝修改 REVIEWS |
| 首次发布前仅普通 NOTE 中含一处 CRLF，发布 1 个 run | 0 | 成功 |
| 同一路径发布 2 个 run | 1 | 拒绝修改 REVIEWS |

拒绝信息为 `update 不得修改受管字段或文档：REVIEWS`。拒绝后正文和 revision 均不变；四项 CheckBoard 均通过。单条索引末尾空白会被 SectionBody 的 TrimSpace 去掉，因此不能笼统断言所有行尾归一化都会被拒；真正触发条件是受保护区段正文比较结果变化。

处置：不采用“主导行尾”或“既有索引行尾”算法，本任务没有整卡行尾归一化契约。改变行尾选择仍不能消除已有混合正文，也不能代替机器记录保护。保持现有确定规则、解析兼容与严格托管区保护；不为可选格式操作放宽 update。不构成阻断，作为有依据的非阻断保留项交回主控。若将来需要自动归一化，应另行定义受控迁移协议，而非默许改写机器正文。

## QA-10 suggest：核实成立，采纳文档修正

`PublishReviewRun` 只对该 run 尚未发布的卡调用 working/review 状态校验；已有发布回执时执行 verifyCardReview。

隔离探针先完整发布，再经 UpdateDocument 填好测试卡 SUMMARY，以 MoveWithOptions(Result=completed) 实际进入 done。相同 run 重试及 ReviewPublicationComplete 均成功，卡仍处于 done，正文与 revision 不变。针对同一 done 卡创建新 run 则返回 working/review 状态错误。

因此，规则原来的 `both at intent creation and at publication` 比实际路径宽泛。改为 `both at intent creation and when a card is first published`，与 docs/review-evidence.md 的“向未发布卡发布时”对齐。该修正只明确既有行为，不放宽任何实现状态约束，也不新增 done 门禁。

## 验证及保留事实

- 临时探针源码见 [核实源码](review-round-2-probe.txt)，执行过程见 [测试记录](review-round-2-tests.txt)。探针只在隔离临时看板运行，核实后移出仓库，不作为本次测试代码交付。
- 首次探针对 header-two 错设“归一化成功”预期，实际被 REVIEWS 保护拒绝而 exit=1；检查追加时对上一条行尾的选择后改正探针预期，重跑四个行尾场景和已发布 done 场景全部通过。保留初始及纠正后日志，不把探针预期错误记作产品缺陷。
- `go test ./...`、`git diff --check` 通过；rebase 最新组 HEAD 后全量复验通过。
- 本轮只有 Markdown 规则措辞变化，未重复运行 race/Windows 交叉编译；前轮实际通过记录仍在 [首轮处置](review-round-1.md)。Windows 原生环境仍未验证，不以交叉编译声称已验证原生行为。
- PM007 保留范围外：日志确实留存完整原件副本，后续事务读取全部操作记录；未测量长期性能、未实现清理。原始实测和后续建议保持在首轮处置。
- PM005 反证保留：尚未发布就进入 done 时发布失败，done 不可移回 working。QA-10 的已发布回执重试成功不改变此结论，不能将二者混为自动恢复方案。
- 原审核报告的只读权限、未实测作者测试声明、未删除任务文件事实原文保留。
- 未部署、未迁移真实看板、未修改组分支、未集成或清理。

## 交回

QA-09 保留为非阻断格式建议；QA-10 已修正规则。未新增已知阻断。原文、作者处置、交付与验证记录通过 S 基线 show/update 受控入口保存，RESULT 保持为空。卡片返回 review，等待主控接收并按已通过结论承接规则闭批；任务分支、worktree 与会话保留。
