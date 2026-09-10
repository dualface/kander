本轮包含的修复: 无（首轮）。
审核焦点:
(1) 功能开发：PM — 解析器、嵌入定义、审核模板三条交付的需求完整与流程闭环；QA — process 解析、launch argv、审核调用的回归与测试覆盖。
(2) 契约变更：PM — 影响面与文档/规则同步（AGENTS.md、docs/custom-agents.md、docs/output-parsing.md、KANDER-BASE-RULES.md、KANDER-KANBAN-RULES.md、KANDER-REVIEW-RULES.md）；QA — 定义驱动 argv、输出解析、reviewer 名单的调用方适配与兼容。
(3) 本区间三条交付的集成接缝：审核模板消费解析原语；launch/config/menu 消费嵌入的 agent JSON；空 argv 占位符省略；本区间不含 A3 退出命令/会话钩子。
核验记录:
- git -C /home/dualf/works/kander/worktrees/20260908-agent-definition-group rev-parse HEAD = e1c01277075599cb0395bf62f16855cebf04b060
- git status --porcelain 为空
- git merge-base --is-ancestor 8abdfe2e6430a00e19383f84f23088a8ff743be0 HEAD 成功
- 区间 8abdfe2..HEAD 共 8 个提交 / 58 个文件，+4843/-574
- 实现由执行 agent 在任务分支完成，由编排快进到组分支；编排说明不能替代对代码行为的核验。
环境缺口: 组 worktree 可读。本机无 Windows 原生核验；交叉编译以各卡 ACCEPTANCE_CRITERIA 为准。
