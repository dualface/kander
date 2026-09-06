# Kander 最小工具协议

- 使用 Kander 前按 `KANDER-AGENTS.md` 读取当前作用域配置. 本文件只约束工具, 不规定交流、架构、代码验证、Git 或自动审核流程.
- 使用看板命令时读取 `KANDER-KANBAN-RULES.md` 的结构、状态、领取、通知与恢复协议; 无须启用可选模块.

**单次审核**

- `kander review` 是可明确调用的单次审核工具, 参数为 `[agent] <CWD> <base-commit> <commit> <role> <task-goal|绝对 spec 路径> [review-context] [reviewed-commit]`.
- 目标须为干净 Git worktree, base 须为 commit 祖先.
- 命令维持 Reviewer 只读与输出校验.
- 调用不启用完整审核或 Git 流程, 不要求目标分支为 `develop`.

- 开关不改变参数、数据结构、路径校验或进程隔离. 工具边界失败须报告, 禁用普通文件操作或直接控制 Agent 绕过.

**安装与任务文件**

- 安装由二进制自身完成: 首次运行未安装的 `kander` 进入交互向导, 或运行 `kander install` 重跑.
- Windows 不自动修改 `PATH`.
- 含特殊字符的自动化须用进程 API 的 argv 数组直调命令根的 `kander`, 禁拼 PowerShell/cmd 命令字符串.
- 所有平台的执行 Agent 与 Reviewer 均从 UTF-8 临时文件读取完整任务, 启动参数只含 CLI 必需控制项与一句文件路径指令.
- 文件要求 Agent 完成后尝试删除.
- 删除失败或遗留不影响结果.

**权限与清理**

- 审核私有目录与文件仅当前用户可访问: POSIX `0600`/`0700`.
- Windows 创建即使用关闭继承的受保护 DACL, 禁先发布再收紧.
- Windows 审核根句柄不共享 WRITE/DELETE, 持有至敏感文件写入、Reviewer 运行、进程树收集及清理结束, 阻止改名与原地 reparse 切换.
- 清理从固定句柄逐层拒绝 reparse point, 预算有界, 失败即审核失败.
- 配置不检查、迁移或收紧权限: POSIX 保留既有 mode, 新对象遵循 umask.
- Windows 新配置文件与目录继承父 ACL.
- Windows 配置从卷/UNC anchor 逐分量拒绝 reparse point, 读取与原子替换用固定句柄.
- 看板和 Git exclude 同样拒绝符号链接、junction 等 reparse point.
- Git exclude 保留既有 ACL, 在同一固定句柄内去重追加.
- 禁绕过命令直接操作这些边界.
