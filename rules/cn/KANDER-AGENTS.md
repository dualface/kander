# Kander 工作流规则入口

本入口只负责定位与按需加载, 不默认接管开发流程. 安装器复制同一套 Markdown 原文到规则根, 不生成定制规则文件.

## 作用域

本文件所在目录即「规则根」, 据此确定作用域与下列路径:

| 逻辑名   | 全局安装                       | 项目安装                            |
| -------- | ------------------------------ | ----------------------------------- |
| 规则根   | `~/.agents`                    | `<主 worktree>/.kander/rules`       |
| 命令根   | `~/.local/bin`                 | `<主 worktree>/.kander/bin`         |
| 配置文件 | `~/.config/kander/config.json` | `<主 worktree>/.kander/config.json` |
| 资源目录 | `~/.local/share/kander`        | `<主 worktree>/.kander/share`       |

- 全部设置存配置文件.
- 项目安装的载荷只在主树 `.kander/`, 任务 worktree 共享, 不建副本、镜像或符号链接.
- 下文及各分册中的 `kander` 均指当前作用域入口. 全局安装可用命令根绝对路径或已加入 PATH 的 `kander`; 项目安装必须用 `<命令根>/kander` 的绝对路径 (Windows 为 `<命令根>\kander`), 不替换为 PATH 中的全局命令.

## 先读取配置

- 每个新会话先运行当前作用域 `kander config --json`, 读取规范化配置, 再读取同目录的 `KANDER-BASE-RULES.md`. 最小工具协议不受可选模块开关控制.
- 配置读取或校验失败时停止受影响的 Kander 操作并报告, 不猜测开关值; 用户自有流程和无关问答继续.
- 按下表开关与任务需要加载. 关闭模块不读、不执行、不经引用加载; 用户明确要求可例外加载.

## 可选模块

| 配置项                | 分册                            | 何时读                          |
| --------------------- | ------------------------------- | ------------------------------- |
| `rules.code`          | `KANDER-CODE-RULES.md`          | 启用时, 修改代码或验证          |
| `rules.collaboration` | `KANDER-COLLABORATION-RULES.md` | 启用时, 开始协作任务            |
| `rules.git`           | `KANDER-GIT-RULES.md`           | 启用时, 操作分支、提交或集成    |
| `rules.task_intake`   | `KANDER-TASK-INTAKE-RULES.md`   | 启用时, 收到 Bug 或功能开发需求 |
| `rules.task_groups`   | `KANDER-TASK-GROUP-RULES.md`    | 启用时, 规划或执行任务组        |
| `rules.review`        | `KANDER-REVIEW-RULES.md`        | 启用时, 判断审核触发及执行流程  |
| `rules.reporting`     | `KANDER-REPORTING-RULES.md`     | 启用时, 任务结束汇报            |

- 使用看板命令时需要读 `KANDER-KANBAN-RULES.md`.
- `task_groups` 依赖 `git`.
- 关 `task_intake` 照样能手动建卡并跑单卡.
- 关 `git` 后单卡沿用用户自己的工作目录、分支、交付流程.
- 关 `review` 后不自动要审核, 仍可明确调 `kander review`.
- 关 `reporting` 不免除真实填卡片结果和必要执行记录.

## 规则优先级

- 当前会话明确的用户指令 > 离目标文件最近的项目级 `AGENTS.md` 或 `CLAUDE.md` > 用户自己的全局规则 > 已启用的 Kander 模块及当前作用域配置 > 模块默认值.
- 同目录 `AGENTS.md` 与 `CLAUDE.md` 冲突且用户指令未消解时, 只停止受影响操作并询问用户.
