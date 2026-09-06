# Kander 看板命令协议

## 适用范围

- 实际使用看板命令时读取本文件. 先按 `KANDER-AGENTS.md` 读取配置, 再按需加载已启用模块; 本文不默认要求建卡引导、Git 工作流、审核或固定报告.
- 本文件约束当前作用域 `kander` 命令管理的看板. 用户指令和目标项目规则优先; 卡片只保存任务契约和执行记录, 不覆盖用户决策, 项目规则或安全门禁.
- Agent 操作看板前先完整读取规则根下的本文件, 再读目标卡片.

  下文命令入口按 `KANDER-AGENTS.md`「作用域」选择.

## 存储与定位

- `kanban/` 是不进 Git 的本机共享数据, 唯一实例位于主 worktree 根目录, 只供同主机同文件系统的 Agent 使用.

  任务 worktree 不建副本, 镜像或符号链接.

  远程 Agent 不可见.

  Windows 上符号链接、junction 和其他 reparse point 一律视为不安全入口, `kander` 通过已校验的 Win32 句柄读写和迁移.

  POSIX 继续使用 no-follow 文件操作.

  任一安全校验失败都停止, 禁用文件管理器或普通路径 API 绕过.

- 定位顺序是 `KANBAN_DIR` -> 当前 Git 仓库主 worktree 的 `kanban/` -> 从当前目录向上查找 `kanban/`.

  `KANBAN_DIR` 仅用于测试, 非 Git 项目或明确覆盖.

  正常 Git 项目从任意 worktree 这样定位:

```sh
MAIN_WORKTREE="$(git worktree list --porcelain | sed -n '1s/^worktree //p')"
KANBAN_DIR="$MAIN_WORKTREE/kanban"
```

- `kanban/` 不属于 Kander 安装载荷, 定位不因全局或项目安装而改变.

  看板操作本身不建分支, 不提交, 不 push, 不审核.

  卡片对应的代码任务仍按项目规则执行.

  禁止提交 `kanban/` 或修改项目 `.gitignore` 传播它.

## 命令契约

创建入口、查询和迁移状态只用 `kander`, 禁用 `mv`, `cp` 或文件管理器替代; 正文编辑与同状态形态升级按「入口与文档」「任务规模与分组」执行.

```text
kander init [project-path]
kander list [--mobile] [backlog|todo|working|review|done|archived|trash]
kander show <task-id>
kander new [--large] <feature|bug|chore|research> <slug> <title...>
kander move <task-id> <backlog|todo|working|review|done|archived|trash>
kander pick [task-id]
kander start [--agent codex|claude|grok|cursor] [--launcher auto|tmux|tmux-session|herdr|foreground|console] [task-id]
kander resume [--agent codex|claude|grok|cursor] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher ...] <task-id>
kander notify [--pane HERDR-PANE-ID] [--timeout SECONDS] (--message TEXT | --message-file FILE) <task-id>
kander dismiss [--timeout SECONDS] <task-id>
kander check [--all] [task-id ...]
kander guard-write <path>
kander subscribe [--refresh SECONDS] [--heartbeat SECONDS] <task-group> <task-id>... [--watch <task-id|task-group-id>...]
kander            # 打开终端看板
```

`kander show` 在卡片正文前输出当前状态与绝对路径, 供写卡前重新定位; `kander move` 成功输出迁移后的新路径. `kander guard-write` 供宿主项目写入前 hook 判定目标路径是否会在状态目录复活旧卡, 放行退出 0, 拒绝非零.

新写使用 `@kander_session`、`@kander_project` 与 `# kander-notify:`.

读取、反查与身份匹配时, `@kander_session` 与 `@onevoke_session` 同为会话标记, `@kander_project` 与 `@onevoke_project` 同为项目标记, `# kander-notify:` 与 `# onevoke-notify:` 同为 notify 指令前缀.

任一侧非空且与卡片会话一致即命中, 两侧皆空才算标记缺失.

`resume` 及 `notify` 的恢复/接管通道成功后必须把新容器地址回写到卡片 `窗口`.

foreground/console 归一为 launcher 名.

启动或存活校验失败时恢复调用前原文, 不改卡片状态.

`notify` 与 `resume` 都不迁移卡片: `review -> working` 由被通知或被唤醒的执行 Agent 在处理事项前自行执行 `kander move <task-id> working`, 该状态变化即为「已收到并开工」的回执.

`notify` 探查已记录的 `窗口` 时按三类处理. 本分类优先于后文的恢复概述.

- (1) **忙态**: pane 存在且 Agent/会话匹配, 但 herdr 非 `idle`/`done` 或 tmux 在 copy-mode.
  - 在同一 `--timeout` 预算内按固定间隔重试探查.
  - 超时非零返回「目标 Agent 忙, 未投递」.
  - 不启动恢复实例、不创建正文载荷、不改卡片状态或正文.
  - tmux 会话标记缺失属身份不可证, 不算忙或过期, 走恢复通道.
- (2) **地址过期**: pane 不存在/已退出, Agent/会话不匹配, 或 tmux 前台进程不匹配.
  - herdr 按 Agent 与会话从 `pane list` 反查.
  - tmux 用 `list-panes -a -F` 按非空 `@kander_session` 或 `@onevoke_session`、`pane_dead=0`、Agent 可执行名三重过滤.
  - 唯一命中后重跑完整目标校验, 通过才回写兼容地址并直投.
  - `tmux` 回写 session id; `tmux-session` 回写 session name.
  - 空 `窗口` 的 tmux 旧卡不反查.
- (3) **反查失败**: 0 个/多个命中、反查不可用或新 pane 复检失败时, 按既有链路恢复.
  - 同时报原地址过期原因与反查原因.

显式 `--pane` 覆盖不做过期地址反查.

**启动参数与元数据**

- `start` 的 Agent、launcher、模型档位默认取 Kander 配置, 初始化未完成则用默认值.
- `--agent` 与 `--launcher` 只覆盖本次.
- 大任务 (含 `spec.md` 的目录卡) 用 `kanban_agents.large`, 小任务 (单文件卡) 用 `kanban_agents.small`, 缺省均取 `kanban_agent`.
- 成功输出规模和实际 Agent.
- `start` 默认免确认, 将会话标识写入 `会话`:
  - Claude/Grok 为 UUID; Cursor 为 chat id; Codex 只记 Agent 名.
- 紧邻的 `窗口` 字段写投递地址:
  - herdr: `herdr:<tab-id>:<pane-id>`.
  - tmux/tmux-session: `<launcher>:<session-id>:<window-id>:<pane-id>`.
  - foreground/console: launcher 名.
- tmux/tmux-session 先建占位 window/pane, 持久化 `窗口`, 再 `respawn-pane` 启动 Agent, 用 `tmux set-option -p -t <pane-id> @kander_session <会话-id>` 写 pane 标记.
- Claude/Grok/Cursor 用卡片 id, Codex 复用 `notify`/`resume` rollout 解析.
- 地址写入、启动或标记写入失败均关闭本次 window 并回滚卡片.
- 旧卡缺两字段时按序插在 `负责人` 后, 不批量改写未启动旧卡.

**恢复原会话**

- `resume` 按卡片 `会话` 唤醒原 Agent, 保留上下文:
  - Claude/Grok 用 `--resume <uuid>`; Cursor 用 `--resume <chat-id>`.
  - Codex 用 `codex resume <session-id>`.
  - Codex session id 在 `CODEX_HOME` (默认 `~/.codex`) 的 rollout 记录中检索.
  - 只匹配以该任务 start/resume prompt 开头的用户消息, 不匹配仅提到任务 ID 的主控会话.
  - 找不到则失败.
- 只接受 `review/` 或 `working/` 中的卡, 必须且只能给非空的 `--message` 或 `--message-file`.
- `--timeout` 必须是大于 60 的有限秒数且默认 120 秒.
- `resume` 不迁移卡片状态: `review/` 卡的 prompt 会要求被唤醒的 Agent 先自行执行 `kander move <task-id> working` 再处理事项.
- 启动或存活校验失败时恢复原文档; 文档恢复失败时报错写明卡片实际所在目录.
- launcher 与 `start` 相同.
- 拉起后复用 `notify` 恢复分支的同一存活判据, herdr 与 tmux/tmux-session 校验可寻址终端, foreground 与 console 在完整 timeout 观察期内要求进程不退出.
- herdr 校验本次 `tab create` 直接返回的 pane: `agent` 必须匹配且状态只接受 `idle`, `working`, `blocked`.
- pane 上报非空 `agent_session.value` 时还必须与卡片会话精确匹配, 没有上报有效身份时不以缺失本身判失败.
- 这里判断的是已知新 pane 是否存活, 与直投和反查必须靠会话身份确定目标的职责不同.
- 该降级不是同用户安全边界.
- 秒退时清理新实例, 非零退出并附可取得的 Agent 原始输出, 只有校验通过才按 `start` 的输出格式报告 `已唤醒`.
- 没有 `会话` 记录的卡 (未经 `start` 启动) 不能 `resume`.
- `start`, `resume` 和需要恢复进程的 `notify` 在所有平台都把完整 prompt 写入 UTF-8 临时任务文件, 内含任务 ID、固定要求和消息正文.
- Agent 命令行只接收一句包含该绝对路径的指令.
- 任务文件内要求 Agent 完成后尝试删除, 删除失败或遗留不影响结果.
- 这类文件不做 POSIX 权限或 Windows ACL 检查与收紧.
- 原生 Windows 优先使用 Agent `.exe`.
- Codex, Claude, Grok 或 Cursor 只有 `.cmd`/`.bat` 时, 通过显式 `cmd.exe /d /s /v:off /c` 和 Agent 适配层的参数编码启动.

**接管新会话**

- `resume` 缺省按「恢复原会话」保留原 Agent 与上下文.
- 显式 `--agent <name>` 表示用户授权接管, 即使名称与原 Agent 相同也分配全新会话, 不迁移旧 CLI 上下文: 新 Agent 先从卡片、任务 worktree、Git 状态和实施记录重建进度, 再处理消息.
- 未经 `start`、没有原 `会话` 记录的卡仍不可接管.
- Cursor `create-chat` 等新会话准备失败时卡片不变.
- 启动前以新 Agent、新会话和新 launcher 覆写 `负责人`/`会话`/`窗口`, 不改 `开始时间`.
- 启动或存活校验失败时整文恢复并复原原状态.
- tmux/tmux-session 的 Codex 接管会发现本次精确 session id, 写为 `codex <id>` 并设置 pane 标记.
- herdr 与进程型 Codex 缺少发现通道, 保留 `codex` 并在后续恢复时按 rollout mtime 解析.
- 新 Agent 通过存活校验后、输出 `已接管` 前, 命令按 `dismiss` 的身份和单 pane 拓扑门禁尝试优雅退出并关闭原 herdr/tmux 容器.
- 原 Agent 已死时仅在容器拓扑可证时关闭, 原地址为空、foreground/console 或与新地址相同时记为 N/A.
- 校验、退出或关闭失败只输出 `原容器保留` 及原因, 不强杀、不回滚新 Agent, 命令仍成功.
- 清理与存活观察各可使用一次完整 `--timeout`, 最坏耗时为约 2 倍 timeout.
- 成功先输出 `已清理原容器: ...`, 再输出 `已接管: ...`.

**通知与恢复**

- `notify` 是主控向原执行 Agent 派回事项的单一接口.
- 它与 `resume` 一样只接受 `review/` 或 `working/` 卡, 必须且只能给非空的 `--message` 或 `--message-file`, `--timeout` 必须是大于 60 的有限秒数且默认 120 秒.
- 地址优先级为显式 `--pane` 覆盖、卡片 `窗口` 快路径、缺窗口时按 Agent 与会话 id 扫描 `herdr pane list`.
- 覆盖与反查均继续用 `pane get` 验证 pane 存在、Agent 和 `agent_session.value` 完全匹配且既有 pane 状态为 `idle` 或 `done`.
- 卡片已记录 id 的 Claude/Grok/Cursor 直接比对, 只有缺 id 的旧 Codex 卡复用 `resume` 的 rollout 检索.
- Kander 不设 Agent 白名单, herdr 反查覆盖范围取决于当前版本及各 `source: herdr:<agent>` 集成是否实际报告会话身份.
- 唯一命中后把 `herdr:<tab-id>:<pane-id>` 写回 `窗口`.
- 0 个或多个命中不投递.
- 直投与反查负责确定既有目标身份, 因此 pane 未上报有效会话身份时继续拒绝并进入恢复链, 不使用恢复存活校验的降级判据.
- tmux/tmux-session 的旧卡缺窗口时仍不能反查.
- 有地址时同时验证 `pane_dead=0`, `pane_in_mode=0`, `pane_current_command` 与 Agent 可执行名一致, 并要求 pane 的 `@kander_session` 或 `@onevoke_session` 用户选项至少其一非空, 且该非空值与卡片解析出的会话 id 完全一致.
- 两者皆空时因身份不可证按无直投通道处理并回落, 不降级为进程名级放行.
- 选项不一致属于地址过期, 先按本节三类探查结果反查, 仅在唯一命中并复检通过后直投.
- tmux 用户选项和 herdr `agent_session` 都处于同用户权限内, 只用于避免误投, 不构成抵御同用户恶意伪造的安全边界.
- 只有直投地址及探查通过后才创建正文载荷.
- Windows 临时根只取 GetTempPathW 返回的词法路径并由 no-follow 边界逐分量拒绝 reparse point.
- 正文写入仅当前用户可访问、创建时即收紧的 `0700` 临时目录及 `0600` 文件, foreground/console 回落不创建载荷.
- 终端只收到一行以 `# kander-notify:` 开头、含绝对路径和 marker 的指令.
- 匹配该指令行时同时识别 `# onevoke-notify:`.
- herdr 必须用 `agent prompt <pane-id> <instruction>` 投给 pane 内已在运行的 Agent TUI: 它按 pane 实际的 bracketed-paste 模式送正文, 再延时补一次编码后的 Enter, 并在 Agent 已停在审批或提问 UI 时先行拒绝而不是把正文塞进那个对话框.
- 不得改用面向 shell 的 `pane run` (正文与结尾 CR 同批写入, Cursor 等 TUI 不把它当提交, 正文会停在输入栏), 也不得把文本交给只接受按键名的 `pane send-keys`.
- tmux 继续用 `send-keys -l` 后单独发送 `Enter`.
- herdr 用 `pane wait-output --match <marker> --source recent` 做字面子串匹配, 允许 TUI 在 marker 前加渲染前缀.
- tmux 用有界 `capture-pane` 并按字面子串确认 marker, 同样允许渲染前缀.
- 投递动作成功即成功返回, 命令不迁移卡片; `review/` 卡的消息正文会前置「先执行 `kander move <task-id> working` 再处理事项」的要求, 该状态变化即为开工回执.
- marker 超时只警告「已投递, 未在超时内确认」, 不恢复第二个进程.
- foreground/console、无直投通道、探查或投递动作失败才由命令内部恢复原会话.
- process 型恢复必须在完整 timeout 观察期内保持存活, foreground 验证后继续占用当前终端直至 Agent 退出.
- herdr 恢复存活按 `resume` 的新 pane 判据执行, 其中状态仍只接受 `idle`, `working`, `blocked`, 拒绝 `done` 与 `unknown`.
- 恢复校验失败后的 tab/window/process 清理失败必须与原错误合并报告, 并提示新 Agent 可能仍存活.
- 主控不另行调用 `resume`.
- 两路都失败时非零退出并同时报告原因, 卡片正文与原状态不变.

**遣散与终端清理**

- `dismiss` 只接受 `done/` 或 `archived/` 卡, 不改卡片正文和状态.
- `--timeout` 必须是大于 60 的有限秒数且默认 120 秒.
- 它按卡片 `窗口` 定位 herdr pane 或 tmux/tmux-session pane.
- 缺 `窗口` 的旧 herdr 卡, 以及 pane 不存在/已死, Agent, 会话标记或前台进程不匹配的过期地址, 复用 `notify` 的唯一会话反查.
- herdr 以命中 pane 实际所属 tab 为容器, tmux 以 `display-message` 读取命中 pane 实际所属 session/window, 不回写卡片 `窗口`.
- 0 个或多个命中都拒绝.
- 忙态不反查且继续拒绝.
- 投递前复用 `notify` 的 Agent 与会话精确匹配: herdr 另要求 `agent_status` 为 `idle` 或 `done`, tmux 另要求 pane 存活、不在 copy-mode 且前台进程匹配.
- 被校验 pane 的当前 tab 或 session/window 必须与定位出的容器精确一致, 且容器只能包含该 pane.
- pane 被移动或容器另有 pane 时必须在投递前拒绝, 等待退出期间再次验证归属并在关闭前复核容器拓扑.
- Claude/Codex 送 `/exit`, Grok/Cursor 送 `/quit`.
- herdr 用 `agent prompt`, tmux 用 `send-keys -l` 后单独发送 `Enter`.
- 只有确认 Agent 进程已退出才关 herdr tab 或 tmux window.
- tmux window 已随 Agent 自动消失时视为已关闭.
- 任何身份、状态、容器归属、投递、退出确认或关闭失败, 以及超时时都非零返回并保留当时现场, 不强杀、不降级关容器.
- `foreground`/`console` 没有可关的终端容器, 在不做部分动作的前提下报错.

**初始化看板**

- `init` 幂等创建看板及 7 个状态目录 (`backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`), 既有看板重跑一次即补建缺失目录, Git 项目只更新本地 `info/exclude`.
- Windows 新目录必须相对固定父句柄以 `CREATE_NEW` 创建并在创建时应用当前用户独占的 protected DACL, 创建竞态失败须拒绝操作.
- 既有目录只迁移叶目录 ACL.
- Git exclude 的父链逐分量拒绝 reparse point, 既有 ACL 不变, 去重读取和追加在同一固定叶句柄及文件锁内完成.

**启动方式**

- 六种 launcher: `auto` 在启动当时解析, 不把结果写回配置.
- auto 选择顺序: 处于 herdr (`HERDR_ENV=1`) 时按 `herdr` 启动, 否则处于 tmux 时按 `tmux` 启动, 同时处于两者时 herdr 优先, 两者都不在则失败且不领取, 不回落到 `tmux-session`, `foreground` 或 `console`.
- `tmux` 在启动者当前 session 里后台建任务 window, 要求 `start` 本身跑在 tmux 内.
- `tmux-session` 按主树路径确定专属 session (`kb-<目录名>-<路径摘要>`):
  - 不存在则创建; 已存在且 `@kander_project` 或 `@onevoke_project` 匹配本项目时复用.
  - 同项目共用一个 session, 每卡一个后台 window.
  - 不要求 `start` 在 tmux 内运行; 启动后不切换客户端.
  - 输出 session 名、window id 和 attach 提示.
- `herdr` 要求 `HERDR_ENV=1` 且 herdr 在 PATH, 在当前 workspace 后台新建 tab (`--no-focus`, 标签复用 `window_name()`) 后先等根 pane 就绪, 再在该 pane 执行与 tmux 相同的 Agent 命令, 不使用 `herdr agent start`.
- `foreground` 在当前终端前台运行并等待 Agent 退出.
- `console` 仅支持原生 Windows, 在独立控制台窗口启动 Agent 后立即返回 PID.
- `console` 没有 session/window 复用、attach 或输出抓取能力, 不是 tmux 或 `tmux-session` 的等价实现.
- POSIX 默认 `auto`, Windows 默认 `console`.
- Windows 拒绝 `tmux` 和 `tmux-session`; herdr 有原生 Windows 版本, `herdr` 在 Windows 可用, 选项面板在装了 herdr 时提供它.
- 配置同样接受 Windows 上的 `auto`, 但它在 Windows 只会落到 herdr; 选项面板不提供 `auto`.
- 送进终端容器的 Agent 命令要经该容器的 shell 再解析一次: POSIX 按 sh 拼接; Windows 假定 herdr pane 是 PowerShell, argv 一律编码进 `%VAR%` 变量后由 `cmd.exe /d /s /v:off /c` 还原, 不依赖 PowerShell 向原生程序传参.
- Agent 命令含换行或 NUL 时拒绝启动, 不向容器发送半条命令.

**herdr 会话上报**

- herdr 的 `pane run` 成功且会话 reference 非空时, `start` 与 `resume`/`notify` 恢复共用会话上报路径:
  - 通过 `HERDR_SOCKET_PATH` 调用 `pane.report_agent_session`.
  - 传本次 pane id、卡片 Agent、reference、`source=herdr:<agent>` 与单调递增的 `seq`.
  - 在有界预算内用 `pane get` 读回相同 `agent_session.value`.
- 空 reference (包括新启动且尚未发现 id 的 Codex) 不上报.
- socket、响应或读回失败只输出一条告警, 不使启动失败, 不回滚卡片或关闭 tab.
- herdr 自身集成仍可上报同一身份.
- Kander 的路径用于补齐未触发集成钩子的启动方式.

**检查与存活分类**

- `check` 默认检查除 `done/` `archived/` 外的无效入口, 有错非零退出.
- 对 `todo/`, `working/`, `review/` 卡另检查契约完整性: 必填章节缺失或残留 `<填写>` 占位符、验收条件没有 `- [ ]` 条目, 均计入无效项.
- `--all` 纳入两栏.
- 指定任务 ID 时仅检查目标及跨状态/形态冲突, 无关无效入口不影响结果, 目标在 `done/` 或 `archived/` 也检查.
- 均解析适用卡片的 `前置任务`, 确认引用存在、依赖无环.
- 定向检查遍历可达依赖, 包括跨组环.
- 默认对 `done/` 或 `archived/` 前置卡只确认存在, `--all` 才检查其自身及完整可达图.
- 依赖未满足不使 `check` 失败.
- 无参和 `--all` 探测全部 `working/` 卡.
- 定向只探测指定的 `working/` 卡, 不探测 `review/`.
- 汇总行前输出四态存活段: `alive` 为 Agent 与可用会话身份匹配.
- `stopped` 为 pane 消失或 Agent/进程不匹配且反查未命中.
- `drifted` 为会话唯一反查到新 pane, 附新地址.
- `unknown` 为会话/窗口无效、foreground/console、程序不可用、状态或探测失败.
- herdr 身份缺失但 Agent 和状态有效仍为 `alive`, 注明无法直投.
- Codex 空 reference 不反查.
- 探测错误折算 `unknown`, 不写卡、不影响 `check` 退出码.
- `subscribe` 须显式组 ID 和非空成员 ID, 校验成员归属.
- `--watch` 可重复指定外部卡或组 ID, 组按依赖解析读取口展开当前全部成员, 不校验外部目标归属.
- 外部目标不存在、展开为空、与成员重复或展开相互重复时, 订阅前失败.
- 裸 `kander` 只读展示看板, 不创建、迁移或启动 Agent.

**订阅事件**

- `subscribe` 每行 JSON 含 `event`, `group_id`, 任务 ID 到状态的 `tasks` 映射.
- 初始 `event` 为 `snapshot`.
- 状态变化为 `state-change`, 附 `changed` 数组, 每项含 `task_id`, `from`, `to`.
- 心跳为 `heartbeat`.
- 有被监控 `working/` 卡时, heartbeat 附 `liveness` 映射, 每项含 `agent`, `status`, `channel`, `detail`, 复用 `check` 四态分类器.
- 探测失败折算 `unknown`, 不终止订阅, 快速 refresh 不探测.
- 无 `working/` 卡时省略 `liveness`.
- 传 `--watch` 时, `tasks` 含成员及展开的外部任务, 每条事件附 `watched` 外部 ID 数组.
- 未传不输出 `watched`.
- 外部状态变化同样产生 `state-change` 并重置心跳.
- `--refresh` 为扫描间隔秒数, 默认 1.
- `--heartbeat` 为无状态变化后的心跳秒数, 默认 900.
- 均须有限且大于 0.

**看板显示与操作**

- 裸 `kander` 在 alt-screen 启动 TUI, 加载与报错均在备用屏幕内, 退出恢复终端.
- 同屏默认 5 栏, `-`/`=` 增减并保存.
- 栏宽平分终端, 放不下「设定栏数 × 最小宽度」时减栏, 至少一栏, 切换时保持选中栏可见.
- 栏目为圆角面板, 名称与任务数嵌上边框, 两端箭头提示更多栏目.
- 选中栏边框高亮栏目色, 其余低对比度.
- 卡片标题用栏目色, 选中卡整块反色.
- 主题、刷新、单栏等偏好读 `config.json` 的 `tui` 段, 在选项面板修改.
- 单行顶栏左侧标题与搜索框, 右侧栏数与更新时间, 下留一行空白.
- 底部状态栏左侧栏目数与卡片数, 右侧两个常用按键, 临时复制结果或错误占整栏.
- 方向键或 `hjkl` 切栏目/任务.
- 单击聚焦或选卡, 双击详情, 拖选文本自动复制到系统剪贴板.
- 滚轮翻卡或滚正文, PgUp/PgDn 翻页.
- `/` 或点顶栏搜索区搜索, `y` 复制任务 ID, Enter 开详情, `a` 切存档栏, `t` 循环 auto/light/dark, `o` 开选项, `?` 开按键浮层 (任意键关闭), `r` 刷新, `q` 退出.
- 搜索覆盖标题、任务 ID、任务组、类型、负责人、状态.
- 详情用同款面板与嵌边框标题, 正文按 Markdown 渲染.
- `hjkl`/方向键移动光标, 滚轮滚动, Ctrl-d/u 半页, Ctrl-f/b 或 PgUp/PgDn 整页, `gg`/`G` 到顶/底, `/` 搜正文, `n`/`N` 跳匹配, `v`/`V` 字符/行选择后 `y` 复制, 拖选也自动复制.
- 默认每 30 秒按任务 ID 原位刷新, 尽量保留选中项与滚动位置.
- 扫描忽略无效入口, 不注入 CLI「运行 kander check 查看」警告.
- Go TUI 支持 Windows.
- 库初始化失败须报告原因.

**选项面板**

- `o` 打开选项面板.
- 分区: 界面偏好 (主题、最大同屏栏数、最小栏宽、自动刷新、单栏、全部栏目、默认语言).
- 任务执行与模型 (大/小任务 Agent、模型、推理档位、launcher).
- 审核与模型 (四角色 Reviewer、环节策略、模型、推理档位).
- 规则模块 (七个开关, 可逐项、全开、全关, 任务组依赖 Git).
- 可就地检查环境.
- 标签在值上方或同行, 暗色标签、粗体值, 聚焦值加左右箭头.
- 同 Agent/角色字段不留空行, 不同 Agent/角色间空一行.
- 模型、推理档位和审核环节缩进一级, launcher 独立置任务执行屏末尾.
- 大小任务和四个角色各有独立模型与推理档位, 共用 Agent/Reviewer 也不合并.
- 角色缺值时填所选 Reviewer 默认值, 更换 Reviewer 时重置该角色默认值.
- 更换 Agent/Reviewer 后同屏模型字段同步更新.
- 配置统一写 `config.json`: `tui` 偏好立即生效并保存, 不夹带其他未保存改动.
- 其余分区 Enter 提交, 根菜单「保存并应用」保存当前配置.
- 不读、迁移或删旧 `tui.json`.
- `↑`/`↓` 移字段, `←`/`→` 改值, `Enter` 提交本节并返回.
- 改值即写内存会话, `Esc` 返回且保留改值.
- 安装 tmux 等环境副作用仅在整节 `Enter` 确认后执行.
- `q` 或再次 `o` 关闭.
- 有未保存改动时选择「保存并关闭 / 放弃改动并关闭 / 继续编辑」.
- 标题常驻未保存标记.
- 鼠标点击行聚焦, 再点或双击确认, 滚轮移行.
- 面板只读写配置, 不创建、迁移或启动任务卡.

- 命令只做结构和机械校验; 授权, 依赖和终止理由由 Agent 按本文件判断.

## 状态模型

目录是状态唯一真源; 卡片正文不设 `status` 字段.

- `backlog/`: 已记录但尚未承诺执行.
- `todo/`: 用户已确认, 契约完整, 尚未领取.
- `working/`: 已领取, 正在实现, 验证, 审核或集成; 任务组卡在修复轮次和集成后的收尾也回到这里.
- `review/`: 仅任务组卡使用. 开发、验证和任务分支交付记录已完成, 等主控将该交付 ff 到组分支, 再安排适用审核与最终集成. 此状态本身不保证交付已进组分支, 主控须核对后才能放行组内依赖. 执行 Agent 迁入后结束本轮响应并保留交互式 CLI 会话; 修复、同步或集成成功后的收尾由主控 `notify` 派回, 原执行 Agent 收到后先自行 `kander move <task-id> working` 再处理.
- `done/`: 已满足完成门禁的近期任务.
- `archived/`: 不占活跃看板的完成, 取消, 重复或不修复记录.
- `trash/`: 用户明确要求删除, 但尚未永久清理的入口; 不是任务状态.

```text
backlog <-> todo -> working -> done -> archived        (单卡流程)
                      |  ^
                      v  |  修复轮次与收尾迁回 working
                    review -> done                     (任务组流程; 直迁仅限主控代做收尾)

todo -> backlog                                       取消承诺, 退回待排期
backlog, todo, working, review -> archived            仅限用户授权的终止
除 trash 外任意状态 -> trash                            仅限用户明确要求
```

- 进 `todo/` 须完成任务目标, 预期成果, 验收条件 (至少一条顶层 `- [ ]` 且有内容的可判定条目) 和不在本轮范围, 且这四个章节不残留 `<填写>` 占位符, 并附「建卡后自审」的 `自审:` 记录行 (大任务与任务组成员卡另附 `卡审:` 行); 进 `review/` 须已填写 `任务分支`; 进 `done/` 的门禁见「执行与完成」, 其余见「终止与清理」.
- 旧版看板没有 `review/`: 其余 6 个状态目录齐全时, 任一 `kander` 命令首次定位看板即自动补建 `review/`, 不要求用户重跑 `init`.

  其他状态目录缺失时停止普通看板操作, 可用前述初始化命令补建.

  `review` 被文件/符号链接占用时始终失败.

## 入口与文档

### 不变量

- 状态目录的每个直接子项是一张卡: 小任务为 `YYYYMMDD-short-slug-task.md`, 大任务为同名目录且必须含普通文件 `spec.md`.

  `short-slug` 只含小写 ASCII 字母, 数字和连字符.

  去掉扩展名的入口名即任务 ID.

- 任务 ID 全看板唯一, 不得跨状态重复或同时存在文件和目录形式. 迁移移动整个入口; 入口名创建后不改, 不复制后删, 不留副本. 大任务目录内只用相对链接, 保证迁移后有效.
- 卡片路径随状态迁移变化. 任何写卡前必须以任务 ID 重新定位, 以 `kander show <task-id>` 输出的状态与路径为准; 发现原路径文件不存在, 一律视为卡片已迁移, 禁止在原路径新建, 必须重新定位后再写. 新建卡片只能经 `kander new`. 宿主项目可在写入前 hook 中调用 `kander guard-write <path>` 拦截此类误写.
- 卡片不得包含 token, 凭据, 敏感服务地址或不应留在本机的个人数据.

### 小任务模板

```markdown
# <任务标题>

- 类型: Feature | Bug | Chore | Research
- 任务组:
- 创建时间: YYYY-MM-DD HH:MM
- 负责人:
- 会话:
- 窗口:
- 开始时间:
- 完成时间:
- 任务分支:
- 结果:

## 任务目标

<改什么, 为什么改>

## 用户决策

<用户已确认的方向和取舍; 没有则写 N/A>

## 预期成果

<完成后可观察, 可验证的状态>

## 验收条件

- [ ] <条件>

## 威胁模型

<安全任务写资产, 可信主体和攻击者能力; 非安全任务写 N/A>

## 不在本轮范围

- <按既有问题, 加固, 共享契约与文档, 相邻功能四类逐一写明排除或纳入, 每条附理由>

## 讨论与决策

<关键结论; 任务组卡片还要在开头记录前置任务>

## 实施与验证

<计划, 分支, commit, 验证命令, 结果, 环境缺口和阻塞>

## 完成总结

<实际成果, 偏差, 未处理问题和验收结论; 完成前留空>
```

### 大任务文档

- `spec.md` 必需, 含小任务的元数据及契约章节: 任务目标, 用户决策, 预期成果, 验收条件, 威胁模型, 不在本轮范围, 讨论与决策.
- `plan.md` 按需创建, 记录实施步骤, 影响模块, 验证, 发布和回滚计划, 不得修改 `spec.md` 契约.
- `report.md` 完成时创建, 记录实际改动, 最终 commit, 验证, 偏差, 未处理问题, 风险和验收结论; 不建空文件.

### 契约与记录

- 领取后填写负责人, 开始时间和任务分支, 无分支写 `N/A`.

  `start` 同时写入相邻的 `会话` 与 `窗口` 字段, 旧卡缺字段时插在负责人之后, 手工领取的卡留空.

  命令迁入 `done/` 时填写完成时间.

  结果只在进入 `done/`, `archived/` 或 `trash/` 前填写.

- 卡片进入 `todo/` 后, 任务目标, 用户决策, 预期成果, 验收条件, 不在本轮范围以及任务组关系冻结. 修改任何一项都要先取得用户明确决策.
- 「不在本轮范围」如实界定任务边界, 不把未确认的扩展目标写入验收条件. 审核模块启用时再按 `KANDER-REVIEW-RULES.md` 的审核契约细化范围.
- 实施期只追加关键决策, 验证, 环境缺口, commit, 阻塞和下一步, 不复制会话流水. 稳定的架构, API 和长期规则仍须写入仓库文档或项目规则.

## 任务规模与分组

- 新卡默认小任务, 复杂任务可使用目录卡.

  形态升级只由 backlog 当前编辑者或 working 负责人进行, 保持同一 ID, 原内容转入 spec.md, 不保留副本.

  todo 中不改变形态.

  已启动卡不因此重新 start 或更换 Agent.

- 卡片保留可选的任务组字段及依赖记录. 关闭 task_groups 时不自动拆组, 独立单卡仍可使用. 开启时按 KANDER-TASK-GROUP-RULES.md 规划和执行, 必须同时开启 git.
- 建卡引导属于 KANDER-TASK-INTAKE-RULES.md, 仅在 rules.task_intake=true 时读取; 用户主动操作看板不要求开启它.
- kander new 在 backlog 创建模板, 调用者按已确认内容填写任务目标、预期成果和验收条件, 不将建议写成用户决定.

### 建卡后自审

- 创建者填写完整契约后必须自审, 在进入 `todo/` 前完成; 发现问题先修正再复核, 未通过不得推进. 此步骤属于建卡流程, 不依赖 `task_intake` 或 `review` 开关.
- 对照用户目标、已确认计划与项目规则检查:
  - 目标与成果一致, 没有遗漏已确认需求, 没有把建议或假设写成用户决策.
  - 任务边界清楚, 本轮范围与排除项不冲突, 不将达成目标所必需的工作排除在外.
  - 约束准确且可执行, 符合用户决策、项目规则及实际接口和环境, 没有互相矛盾的要求.
  - 验收条件覆盖目标与成果, 可执行、可判定, 没有遗漏关键条件或引入范围外要求.
- 在 `讨论与决策` 中以独立一行 `自审: <结论>` 记录自审结论与修正项 (ASCII 冒号, 可为列表项); 进入 `todo/` 的门禁校验该行存在. 可依据既有决策修正的内容直接修正; 需要新增或改变用户决策的歧义, 明确列出并等待用户决定, 不自行补成契约.
- 大任务目录卡与任务组成员卡在自审之外还须独立审卡: 由不共享建卡会话上下文的独立 Agent (新会话或子 Agent) 只读卡片与用户原始需求, 按上述四条出具结论; 创建者修正后在 `讨论与决策` 以 `卡审: <结论>` 行记录结论与审卡者, 门禁同样校验该行. 工具只校验记录行存在, 审卡者的独立性与结论质量仍由创建者如实保证, 不得由建卡 Agent 自己补写 `卡审:` 行敷衍门禁.

## 领取, 启动与协调

- 未指定任务且 `todo/` 有多张卡时列候选让用户选; 任务组按已确认依赖排序, 不逐卡询问. 开工条件不足时只报缺口, 不领取或退回 `backlog/`.
- 动代码前必须先取得 `working/` 中的唯一入口. 两种领取方式互斥:

```sh
# 委派给新执行 Agent: start 原子领取并启动
kander start [--agent codex|claude|grok|cursor] [--launcher auto|tmux|tmux-session|herdr|foreground|console] <task-id>

# 用户明确要求当前 Agent 执行既有任务卡: 只迁移, 随后手工填写负责人和开始时间
kander move <task-id> working
```

- `kander move <task-id> working` 仅适用于用户明确要求当前 Agent 执行既有任务卡.

  仅在采用已启用的建卡引导时, 选择「确认计划并走看板」必须用 `start`.

  不得先 `move ... working` 再 `start`.

  `start` 只接受 `todo` 卡.

  同文件系统上的入口迁移就是领取原语, 只有迁移成功者取得任务.

  失败后重查, 不建替代卡, 不另加 lock 服务, 数据库或 ID 分配器.

**启动检查与回滚**

- `start` 在启动前检查 Agent, launcher 和 TTY.
- `auto` 先解析当前环境, 再检查实际 launcher:
  - `tmux`: 已在 tmux session 内.
  - `tmux-session`: tmux 可用, 启动时选定项目 session 名.
  - `herdr`: `HERDR_ENV=1`, herdr 在 PATH, 且有 `HERDR_WORKSPACE_ID`.
  - `foreground`: 三个标准流均为 TTY.
  - `console`: 原生 Windows.
- 前置检查失败不领取.
- 创建进程, tmux session, tmux window, herdr tab, herdr pane 就绪等待或 `pane run` 失败时恢复文档并迁回 `todo/`.
- herdr 就绪等待或 `pane run` 失败还须关闭本次新建的 tab, tmux/tmux-session 的 Codex 会话发现或 pane 会话标记写入失败还须关闭本次新建的 window.
- 新建 tab 的 shell 接管终端前送入的命令文本会被丢弃, 因此 `pane run` 必须在 pane 渲染出首帧输出之后, 就绪等待有上限, 超时按失败处理.
- tmux/tmux-session 只有在会话发现与 pane 标记写入成功后才算启动成功.
- herdr 的成功条件见本节后面的 best-effort 条款.
- foreground/console 在进程创建成功后即算启动成功, 后续退出不自动回滚.
- `console` 成功时输出 PID 后立即返回.
- 成功输出实际 launcher.
- `auto` 须显示解析结果 (`herdr` 或 `tmux`).

- herdr 在 `pane run` 成功后即算启动成功; 后续会话身份上报和读回是 best-effort, 失败只告警, 不进入 `LaunchFailure` 的 tab 关闭与卡片回滚路径.
- `start` 的临时任务文件只写任务 ID 和固定要求, Agent 命令行只传一句读取该文件的指令.

  执行 Agent 先按入口核对配置, 再读本协议、卡片和项目规则, 按适用流程准备实际工作目录.

  确实使用任务分支时填写任务分支.

  任务组的启动任务文件中“退出”表示结束本轮响应, 不要求主动退出交互式 Agent CLI 或关闭终端容器. 非交互式调用自然结束时, 后续派回由 `notify` 选择恢复; 交互式会话保留到用户明确同意遣散.

- 领取后只有执行负责人可修改或迁移 `working/` 入口; 协调和编排 Agent 只读督办. 明确交接后由新负责人接管, 不得并发写.
- 启动后的协调责任按启动方式分:
  - foreground 单卡: 启动者在 Agent 退出后检查结果, 直到任务完成或明确交接.
  - tmux、tmux-session 或 herdr 单卡: 执行 Agent 在独立 window 或 tab 直接向用户汇报, 启动者不巡检.

    启动成功后立即告知用户本会话不跟踪该任务进度, 当前 session 可以结束, 下一个任务另开会话.

    `tmux-session` 还要一并给出 session 名和 attach 命令.

    `herdr` 还要给出 tab id 和 pane id.

    `auto` 解析为 `herdr` 或 `tmux` 后按对应单卡规则协调.

    用户明确要求跟踪时改按 foreground 单卡协调.

  - console 单卡: 执行 Agent 在独立 Windows 控制台直接向用户汇报, 启动者不抓取输出.

    启动成功后告知用户 PID 及本会话不跟踪进度.

    该 PID 只用于只读判断进程是否仍存在, 不能用于 attach 或恢复输出.

    用户明确要求由启动者跟踪时改按 foreground 单卡协调.

  - 任务组: 仅在模块已启用且依赖满足时, 按 `KANDER-TASK-GROUP-RULES.md` 编排, 进行适用的审核、集成和收尾, 启动成功不解除该责任.

## 执行与完成

- 单卡按任务契约、用户和项目规则, 以及当前已启用的 Kander 模块完成交付.

  Git 模块关闭时不要求 develop、worktree、提交、push 或合回.

  审核模块关闭时不自动要求审核.

  用户自己的审核、PR 或验收条件仍须满足.

- 根据卡片记录确认实际工作目录, 记录实施与验证及未处理问题.

  完成任务契约和所有适用交付步骤后填写完成总结或 report.md, 填 结果: completed, 执行 kander move <task-id> done 和 kander check.

- 失败或暂停时保持实际状态并记录阻塞和解除条件. 不适用的 Git 或审核步骤写 N/A, 不把未执行的验证写为通过.
- 任务组成员仅在启用 task_groups 和 git 时按 KANDER-TASK-GROUP-RULES.md 执行 review、集成和收尾. 不能把这些门禁应用到独立单卡.
- 对用户的固定报告格式只在 rules.reporting=true 时读取 KANDER-REPORTING-RULES.md. 关闭时用用户自己的汇报形式, 卡片结果和必要执行记录不省略.
- 已进入 done 的卡片不因事后发现新问题而退回或复用; 新问题另建卡并指向原卡.

## 终止与清理

- 用户明确取消, 判定重复, 决定不修或接受替代方向后, 才可将 `backlog/`, `todo/` 或 `working/` 卡 (以及 review 状态卡) 直接归档.

  实现困难, 验证失败或暂时阻塞不算授权.

  结果只能是 `cancelled`, `duplicate` 或 `wontfix`, 均写原因, `duplicate` 还须指向替代卡.

  `completed` 只用于 `done -> archived`.

- 卡片迁入 `archived/` 或 `trash/` 后, 执行或操作该卡的 Agent 按用户约定汇报结果 (仅在 `rules.reporting=true` 时使用 `KANDER-REPORTING-RULES.md` 模板), 末行状态写实际去向和结果.
- `done/` 保留近期完成项, 用户确认无需展示后再归档. 只有用户明确要求删除具体卡片时才移入 `trash/`; 迁移前写 `结果: trashed`, 原因和时间. 不自动清空或永久删除; 永久删除须逐项授权.

## 异常恢复

- `working/` 卡中断, 无负责人或长期无进展时, 协调 Agent 先用 `kander notify <task-id> --message <现状与要求>` 通知原执行 Agent.

  命令自行选择直投或恢复, 非零退出时由用户决定交接或终止.

  用户决定换 Agent 时只用 `kander resume --agent <name> <task-id> --message <现状与要求>` 建立接管新会话, 不手工迁移会话、不再次 `start`.

  其他 Agent 不得自行接管, 迁移或归档.

  进程退出不改变 `working/`, 不退回 `todo/`.

- 出现重复 ID, 跨状态副本, 文件与目录同 ID, 大任务缺 `spec.md`, 目标冲突, 状态目录缺失或不可写时, 停止受影响操作并保留现场. 不通过删除, 改名或移动来绕过报错.
- 看板无 Git 历史; 误删先查 `trash/` 和本机备份, 不伪造内容.
