# Repository Guidelines

本文件是 Kander 仓库自身的开发契约. 仓库对外发布的工作流规则在 `rules/`, 那些文件是交付物, 不是本仓库的包边界说明.

## 本仓库特例

- 本仓库第二阶段安全角色 `CSA` 和 `Hacker` 一律标记 N/A, 不运行; `PM` 和 `QA` 保持适用.
- 审核 base 以来全部改动都是 Markdown 规则或文档时, 不运行审核. 只要包含任一脚本, 代码或其他非 Markdown 文件, 就按适用规则运行 `PM` 和 `QA`; `CSA` 和 `Hacker` 仍按上一条标记 N/A.

## 语言约定

- 提交备注一律英文: 标题, 正文, 尾注全部用英文, 不留中文. 已有历史已统一为英文, 后续提交不得回退.
- 代码注释一律英文: `.go` 的行注释, 块注释和文档注释, 以及 `.sh` / `.ps1` 等脚本内的注释都用英文.
- 本条只管注释与提交备注. 面向用户的字符串仍走 `internal/i18n` 双语资源, 不因本条改写; Markdown 文档 (`AGENTS.md`, `README.md`, `rules/`, `docs/`) 保持中文.

## Go 模块与包图

- 模块路径: `github.com/dualface/kander`.
- 单二进制入口: `cmd/kander`. `main.go` 只调用 `internal/cli.Run`; 同目录其余文件以空白 import 接入实现包, 触发各包的 `init` 绑定. 禁止再拆第二个命令入口.
- 不带子命令运行 `kander` 直接打开终端看板: `internal/cli` 暴露 `DefaultRunner`, 由 `internal/tui` 在注册时设置, `internal/cli` 不反向依赖 `internal/tui`.
- `internal/cli` 集中维护命令名与 Runner 注册表. `doctor`/`config`/`version` 及 board 命令在本包接线; launch/liveness/notify/review/takeover 由实现包覆写对应 Runner. `check` 先接 board 结构检查, 完整二进制再由 liveness 覆写为结构检查加存活段.
- 包职责:

| 包                  | 职责                                                                 |
| ------------------- | -------------------------------------------------------------------- |
| `internal/cli`      | 命令名与 Runner 注册表, 全局 `--lang`, 参数解析和分发                    |
| `internal/config`   | 安装作用域, `config.json` schema/修复, 语言/launcher/Agent/模型/规则/TUI 读取口 |
| `internal/version`  | 构建时间戳与 Git hash 组成的统一版本号                                   |
| `internal/i18n`     | go-i18n 消息目录与模板渲染; 不依赖 config, 语言由调用方传入              |
| `internal/fs`       | POSIX no-follow 与 Windows 句柄/reparse/DACL/锁                          |
| `internal/process`  | Agent CLI 解析, UTF-8 任务文件, argv/env 调用构造                         |
| `internal/board`    | 看板定位, 卡片校验, 状态迁移及不含存活探测的生命周期命令                 |
| `internal/launch`   | start/resume, 接管启动, 存活确认与失败回滚                               |
| `internal/probe`    | herdr/tmux pane 事实采集                                                 |
| `internal/liveness` | check 存活段, 会话反查及 subscribe JSON Lines 事件流                      |
| `internal/notify`   | notify 直投, 忙/过期判断, resume 恢复与失败回滚                           |
| `internal/takeover` | dismiss 及 resume 接管成功后的旧容器清理                                 |
| `internal/window`   | 卡片 `WINDOW` 元数据回写与失败恢复原文                                     |
| `internal/review`   | `kander review` 单一审核门禁                                             |
| `internal/tui`      | 裸 `kander` 的终端看板与 Huh 选项面板                                    |
| `internal/menu`     | doctor/config, 环境探测与修复, 选项面板共用的 `menu.Session`             |

- 运行时看板数据目录仍是主 worktree 的 `kanban/`, 覆盖仍是 `KANBAN_DIR`. 配置键 `kanban_agent` / `kanban_agents` / `models.kanban` 保持 onevoke schema, 不改名.
- `rules` 保存 collaboration/code/git/review/task_intake/task_groups/reporting 七个可选模块开关. 新配置默认全开; 合法旧配置缺整个 rules 段时保留原七项全开, 段内缺项关闭. 解析与 doctor 修复复用 internal/config, 开关独立于 `welcome_complete` 初始化状态. task_groups 依赖 git; TUI 选项面板复用 `menu.Session.SetRules`, 启动/恢复/接管/通知在副作用前复核任务组依赖. 卡片任务组解析复用 `board.TaskGroupFrom`, 包括旧讨论区字段.
- `review_stages.<role>` 为四个审核角色保存 `auto` / `skip` / `required`, 缺失整个段或段内角色时默认 `auto`; 本仓库上方的角色特例优先于该配置.
- `models.kanban.<agent>` 的模型按任务规模存放在 `large_model` / `small_model`. codex/claude/grok 的推理档位存放在 `large_effort` / `small_effort`, 并继续接受旧共享键 `model`: 规模模型为空时回落到它. Cursor 只接受两个规模模型键, 不接受共享 `model` 或推理档位. 选项面板只编辑规模键.
- `models.review_roles.<role>` 保存角色自己的 `model` / `effort` 覆盖. 默认及合法旧配置可为空; 空项由 `config.ReviewModelFor(cfg, agent, role)` 在运行时回落到调用方所给 agent 的 `models.review.<agent>` 值. 进入选项面板的「审核与模型」分区时, `menu.Session` 才尝试用当前 Reviewer 的值填缺项; Reviewer 的源值为空或无该字段时仍保留空值. `kander review` 可显式指定 reviewer, 未必等于配置里该角色的 Reviewer.

## TUI 技术栈

终端界面统一用 Charm 一套库, 不再自绘终端后端:

| 库         | 用途                                                        |
| ---------- | ----------------------------------------------------------- |
| Bubble Tea | 运行时: alt-screen, 输入, 鼠标, resize, `tea.Exec` 挂起终端 |
| Lip Gloss  | 样式与布局: 栏目用 `JoinHorizontal` 组合, 弹窗边框, 主题色  |
| Bubbles    | `viewport` (详情与报告滚动), `spinner` (环境探测)           |
| Huh        | 选项面板的全部表单: 根菜单与各分区                          |
| Glamour    | 任务卡正文的 Markdown 渲染                                  |

- 看板与详情的几何 (栏目 X/宽度, 卡片行) 由本包自己算, 鼠标命中, 拖选复制都依赖它; 不要改成由组件库托管布局.
- 选区与光标一律在去掉 ANSI 之后的纯文本上计算 (`ansi.Strip`), 渲染时再按 span 重新着色.
- 弹窗用 `overlay()` 按显示列合成到底层画面上, 不是整屏替换.
- `internal/menu` 不得 import `internal/tui`; TUI 选项面板单向复用 `menu.Session` 配置逻辑.

## 子命令

Runner 注册表包含: `doctor` `config` `version` `review` `init` `list`/`ls` `show` `new` `move` `pick` `start` `resume` `notify` `dismiss` `check` `subscribe`. `help` 是直接输出顶层帮助的特殊分支, 不进入 Runner 注册表. 裸 `kander` 打开终端看板; 全局 `--lang {cn,en}`.

## TUI 测试

- `internal/tui` 的用例留在本包内, 覆盖稳定的渲染约束与交互行为: 明暗主题画布背景, 屏幕填充, 可见栏目和焦点, Markdown 转换, ANSI 去除后的内容, 偏好读写, 按键与鼠标, 选项面板及 PTY 启动冒烟.
- 不为频繁变化的边框、徽标、状态栏文案建立整屏快照. 完整视觉效果仍在真实终端检查.

## 测试命令

POSIX 与 Windows 共用 (Windows 专项测试在非 Windows 上 skip):

```sh
go test ./...
```

针对单包:

```sh
go test ./internal/fs
go test ./internal/config
```

本仓库尚无 Python. 提交前在模块根运行 `go test ./...`; 当前未设覆盖率阈值. 测试必须用临时目录隔离, 不改写用户真实看板或 `$HOME` 配置.

## Windows 句柄 / reparse / DACL

Go 运行时写入配置, 看板迁移, 审核 runtime 或 Git exclude 时经过 `internal/fs`. `install.sh` 与 `install.ps1` 是例外: 项目安装时由脚本自行检查符号链接/reparse point, 再用平台普通文件 API 将 `/.kander/` 写入 Git exclude.

- 从卷/UNC anchor 逐分量拒绝符号链接, junction 和其他 reparse point.
- 配置读写不检查或自动收紧权限: POSIX 保存保留既有文件 mode, 新文件与目录遵循 umask; Windows 新文件与目录继承父目录 ACL.
- `internal/fs` 管理的其他私有对象创建瞬间即当前用户独占的受保护 DACL, 不得先按继承 ACL 发布再收紧.
- 阻塞独占锁用 `LockFileEx`; POSIX 对应 `flock`.
- `internal/fs` 的原子替换相对固定父句柄; 任一安全后端失败显式报错, 不静默回落普通路径 API. 安装脚本不提供同等的固定句柄或文件锁语义.

## 发布规则

- `rules/KANDER-AGENTS.md` 是发布规则入口, 先读取当前作用域的 `kander config --json`. `KANDER-BASE-RULES.md` 与 `KANDER-KANBAN-RULES.md` 是工具协议, 其余七个模块分册按开关和需要加载, 全部位于 `rules/`; 不生成定制规则文件, 不经交叉引用加载关闭模块.
- 根目录 `AGENTS.md` 只约束本仓库开发; 改发布工作流时改 `rules/`, 不要把实现细节写进发布分册, 也不要把包图写进 `KANDER-AGENTS.md`.
- 运行时创建的 `kanban/` 是本机共享数据, 不得提交, 也不得写入项目 `.gitignore`.
