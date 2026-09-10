# 规划修订记录

- 本轮请求：结合 co-work 创建的三卡与订阅、通知、重复卡讨论，给出全面方案并创建/更新卡片。
- 本轮变更：保留原三卡 ID，新增五张目录卡，划为两个四卡任务组。
- 原组：20260907-review-archive-group。顺序 S（写入事务）、D（目录迁移）、A（审核归档）、R（结论门禁）。
- 新组：20260907-orchestration-reliability-group。顺序 P（探测）、N（派回）、E（订阅）、O（编排恢复）。前组完成并实际交付后才启动新组。
- 原三卡 spec 及旧审卡记录保存在各卡 planning-history/20260907-before-integration.md；旧 PASS 不代表本版通过。
- 本版采用技术方案 C：执行端保留自己的处置原文，编排端引用，工具机械汇总批次；未声称用户已选择原卡 A/B。
- 已运行 kander check（八个明确 ID），结果 ok: 8 tasks。
- 现有仓库代码没有被本轮规划修改；没有开始实现、迁移看板、启动执行 Agent、提交或部署。
- 独立卡审已完成：八卡全部 PASS；三项必须修正和三项建议均经增量复核关闭，原文见 card-review.md。各卡 SELF_REVIEW/CARD_REVIEW 已回填。
- 原两张 todo 卡保持状态；原第三卡与五张新卡在审查通过后由 kander pick 移至 todo。最终八卡均 todo，OWNER/SESSION/WINDOW 为空，未启动。
- 最终定向 kander check 返回 ok: 8 tasks；Git 工作区无本轮代码或受跟踪文档改动，看板和规划证据保持本机数据。
