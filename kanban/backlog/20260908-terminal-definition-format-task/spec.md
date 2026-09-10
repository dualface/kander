# Declarative terminal definition format and parser, with tmux moved to an embedded definition file

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-terminal-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:17
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

定义声明式终端定义文件格式, 实现从定义文件构造 `terminal.Backend` 的通用解析器, 并把内置 tmux 后端迁为一份嵌入的定义文件. 完成后, 贡献者为 zellij, wezterm 等终端复用器添加支持的交付物是一个 JSON 文件, 不需要改 Go 代码; 内置 tmux 与用户提供的定义走同一机制.

## USER_DECISIONS

- 声明式定义文件是主要扩展方式; 输出解析只有三种原语; 不做 shell 插值, 占位符逐元素替换; 这些边界写入文档作为不可扩大的契约.
- 定义格式带版本号字段.
- tmux 先迁, 因其没有 socket 钩子; herdr 由下一张卡处理.
- 本轮只建卡, 不启动.

## EXPECTED_OUTCOME

- 定义文件格式 (JSON) 至少包含: `schema_version`, `name`, `binary` (PATH 名或绝对路径), `launchers` (该定义提供的 launcher 名及各自的选项覆盖, 用于 tmux 与 tmux-session 共用一份定义), `ops` (每个 `Backend` 操作一个条目), `errors` (错误分类规则). 每个 op 条目是一个有序的 `steps` 数组 (单步操作只有一个元素); 每个 step 含 `argv` 模板, 可选 `output` 解析声明 (复用 `internal/process` 的输出解析结构, 其权威描述与实现由 `20260909-process-output-parser-task` 交付, 本卡只引用不复述字段集合; 终端定义限制 `source` 为 `stdout` | `stderr`, 且**当前只使用整份文档形态** —— 终端 step 的产物要走 `fields` 结构化映射, tmux 的多行 `-F` 输出靠 `candidates` 与 `fields` 处理, 不是"逐行套用同一 parse 再拼成一段文本", 现在开 `ndjson` 只增加未测试面; 需要时再按该卡的权威描述扩用), 可选 `fields` (把解析结果映射到该操作的结构化返回字段, 例如 pane 事实的存活、前台进程、copy-mode、agent 状态), 可选 `poll` (`interval`, `timeout`, `until` 取 `matched` | `nonempty` | `json_field:<path>=<值>`, 用于等待标记与就绪), 可选 `on_error` (取 `fail` | `continue` | `meta_missing`, 用于元数据缺失回退与多步聚焦的降级), 可选 `timeout`, 可选 `store` (给本步解析结果命名, 后续步骤与本操作的返回可用 `{step.<name>.<field>}` 引用), 可选 `when` (有界条件: `prev_ok` | `prev_failed` | `field:<name>=<值>` | `field_missing:<name>`, 只引用前面步骤已存储的结果), 可选 `stop_on_success` (本步成功即短路, 跳过后续步骤, 用于 "主字段查到就不读旧字段"). 需要在多个候选目标中选一个的操作 (例如 tmux-session 的会话复用) 用操作级 `candidates`: 每个候选是一组 steps 加 `select_when` 条件 (例如项目标记等于当前项目), 按序尝试, 命中即用, 都不命中时执行 `fallback` steps (例如新建会话或新建窗口), 对应 `internal/launch/tmux.go:202`, `:217`, `:237` 现在的主字段/旧字段回退、按 owner 选择复用或换候选、再决定 new-session/new-window 的行为. 组合流程由通用的 `DeclarativeBackend` 按 steps 顺序执行, 不由调用方拼装; 这套 steps/poll/on_error/fields 机制是本卡交付的一部分, 先以 `20260908-terminal-backend-dispatch-task` `plan.md` 中 tmux 与 herdr 的纸面表达验证为输入固定下来, 再迁移 tmux.
- 占位符集合由 `Backend` 各操作的输入参数决定并在文档列出, 至少含 `{cwd}`, `{label}`, `{pane}`, `{container}`, `{command}`, `{key}`, `{value}`, `{marker}`, `{text}`. 替换规则与 agent 定义一致: 逐元素替换, 空值丢弃相邻 flag, 控制字符拒绝, 不做 shell 插值. 花括号规则在 agent 定义基础上补充转义: 模板中只有白名单内的 `{name}` 是占位符, 字面花括号写作 `{{` 与 `}}`, 因此 tmux 的格式表达式在定义中写作 `#{{session_id}}`; 模板校验只拒绝未转义且不在白名单内的单花括号序列, 运行时替换进去的值 (路径, 文本, 标记) 不受花括号限制. 控制字符分两层: 模板元素允许 JSON 转义的 `\t` (tmux 的 `-F` 格式串用 TAB 作字段分隔, 对应 `internal/launch/tmux.go:238` 与 `internal/probe/tmux.go:102` 现在含真实 TAB 的 argv), 其他控制字符 (含换行与 NUL) 在模板校验时拒绝; 运行时替换进去的值只拒绝换行与 NUL (沿用 "命令含换行或 NUL 则拒绝启动" 的既有规则), 不限制 TAB 与花括号.
- 错误分类声明: `errors.gone` 与 `errors.meta_missing` 各为一组匹配规则, 规则取值为退出码, `stderr` 正则, 或 `stdout_json:<dotted.path>=<值>` / `stderr_json:<dotted.path>=<值>` (从 JSON 输出中取错误码, 对应 herdr 现在从 stderr 或 stdout 解码 `error.code` 的行为), 分别映射到接口的 "容器已消失" 与 "元数据缺失" 错误; 其余失败为普通命令失败. liveness, notify, takeover 依赖这一分类决定回滚, 降级或报错, 语义与现有 tmux 实现一致.
- 地址与能力: 定义声明 `address` (WINDOW 中冒号后不透明地址的字段顺序与解析规则) 与 `capabilities` (有容器, 支持聚焦, 支持元数据, 报告前台进程), `DeclarativeBackend` 据此实现接口的地址编解码与能力标志; liveness, notify, dismiss, focus 对自定义定义走与内置相同的接口路径.
- 启动前提: 定义提供的每个 launcher 声明 `requires`: 必需环境变量 (名称, 可选取值, 例如 `TMUX` 与 `TMUX_PANE` 对应 `internal/launch/tmux.go:149`, `HERDR_ENV=1` 与 `HERDR_WORKSPACE_ID` 对应 `internal/launch/tmux.go:73`), `platform` (`posix` | `windows` | `any`), `inside_session` (是否必须在该终端的现有会话内运行), 以及 `binary` 存在; `foreground` 与 `console` 不是定义提供的 launcher, 仍是 `20260908-terminal-backend-dispatch-task` 保留的 Go 无容器后端, 其前提 (foreground 三路标准流为 TTY, console 原生 Windows, 对应 `internal/launch/tmux.go:93` 附近) 与 `auto` 的选择顺序及 "不回落到 foreground/console" 一起作为宿主策略保留在 `internal/terminal`, `auto` 在定义提供的 launcher 之间按各定义的 `auto_priority` 与 `requires` 判定. 前提不满足时在创建容器前拒绝并说明缺什么.
- 钩子承载 (通用部分由本卡交付, herdr 的具体钩子由 `20260908-terminal-herdr-hooks-task` 提供): 定义格式含 `hooks` 对象 (挂载点 -> 已注册钩子名), `internal/terminal` 提供钩子注册表与三态结果类型 (成功 / 降级并附说明 / 失败), 引用未注册钩子名的定义被拒绝; 本卡只交付机制与测试用的假钩子, 不实现任何 socket 协议.
- `internal/terminal` 新增通用的 `DeclarativeBackend`: 读取定义, 按 steps 构造 argv, 执行, 轮询, 解析输出并映射字段, 分类错误. 内置 tmux 后端的 Go 实现删除, 由嵌入的 `tmux.json` 定义经 `DeclarativeBackend` 提供, 行为与现有 tmux 后端一致.
- 定义文件搜索路径与优先级: 嵌入定义 < 全局 share 目录 `terminals/<name>.json` < 项目安装 share 目录 `terminals/<name>.json`; 同名覆盖整份定义. `launcher` 配置与 `kander start --launcher` 接受任何已加载定义提供的 launcher 名, 经 `20260908-terminal-backend-dispatch-task` 提供的 launcher 名注册口加入, `config` 仍不 import `terminal`; doctor 报告每份用户定义的校验结果.
- 文档 `docs/terminal-definitions.md` 描述格式, 错误分类, 搜索路径, 版本号策略, 以及 "不做 shell 插值, 不加脚本能力" 的固定边界; 涉及公共输出解析结构与占位符转义规则时**只链接 `20260909-process-output-parser-task` 交付的 `docs/output-parsing.md`, 不复述字段集合与原语清单** —— 两份文档各描述一遍同一个公共结构, 只是把分叉源从卡下沉到文档.

## ACCEPTANCE_CRITERIA

- [ ] 定义格式的 Go 结构体与校验函数存在; 校验覆盖: `schema_version` 未知拒绝, 必需 op 缺失拒绝, steps 非空, `output` (终端子集 `stdout` | `stderr`; 终端定义声明逐行流形态时拒绝, 与 EXPECTED_OUTCOME "终端侧当前只使用整份文档形态" 一致)/`fields`/`poll`/`on_error`/`when`/`store`/`stop_on_success`/`candidates`/`requires`/`hooks` 取值集合与引用合法性 (`when` 只能引用前面步骤的 `store`, 钩子名已注册), `errors` 规则取值集合与正则可编译, 占位符白名单与 `{{`/`}}` 转义 (含 `#{{session_id}}` 用例), 模板元素只允许 `\t` 这一种控制字符 (含带 TAB 的 tmux 格式串通过、含换行的模板拒绝两个用例), 空元素拒绝, 运行时值只拒绝换行与 NUL, `address` 与 `capabilities` 结构; 错误信息含文件路径, op 名, step 序号与字段名.
- [ ] `DeclarativeBackend` 实现 `terminal.Backend` 全部操作; 单元测试用假可执行程序 (测试内构造) 覆盖每个 op 的 argv 构造, 多步 steps 顺序执行, `poll` 的命中与超时, `on_error` 的 continue 与 meta_missing 回退, `when` 条件执行, `store` 结果传递, `stop_on_success` 短路, `candidates` 的命中/换候选/fallback, `fields` 映射, `requires` 不满足时创建容器前拒绝, 假钩子的三态结果传递, 三种输出解析 (复用共用实现, 终端侧不使用也不测试逐行流形态, 该分支由公共实现自己的测试覆盖; 终端侧不新写解析器), 退出码/stderr 正则/stdout_json 三种错误分类与超时.
- [ ] 嵌入的 `tmux.json` 存在, 提供 `tmux` 与 `tmux-session` 两个 launcher 名 (各自声明 `requires`); 三项行为验收: 主元数据字段查到时不再读旧字段, 会话名被其他项目占用时换下一候选, 本项目已有会话时复用而不新建; `internal/terminal/tmux` 的 Go 实现删除; 改动前针对 tmux 后端的全部测试 (含从 launch, probe, takeover, notify, focus 迁来的用例) 在声明式实现上保持通过, 未删改期望值. 从定义加载到实际 argv 的用例: `-F` 格式串中的 TAB 与 `#{session_id}` 等表达式在最终 argv 中与改动前逐字节一致;
- [ ] 一个测试用的第三方定义 (例如以 shell 脚本模拟的假终端) 放在临时 share 目录后, `kander config --json` 与 doctor 列出该 launcher 且校验通过; 端到端测试用该假后端跑通完整生命周期: `kander start --launcher <name>` 创建容器并写入可被解析的 `WINDOW`, `check` 的存活分类 (alive/stopped/gone), `notify` 的直投与标记确认, `resume` 的接管, `dismiss` 的退出与关闭, focus 的聚焦或按能力标志降级; 删除或损坏该文件后 doctor 报告具体错误, 不影响内置 launcher.
- [ ] 项目 share 目录的同名定义覆盖全局定义, 全局覆盖嵌入; 由测试固定.
- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `docs/terminal-definitions.md` 新增; `AGENTS.md` 包表更新; `rules/KANDER-KANBAN-RULES.md` 中命令表里 `start`/`resume` 的 `--launcher` 取值说明改为 "内置 launcher 名或已加载终端定义提供的 launcher 名", "Launchers" 一节的 "Six launchers" 与 auto 解析说明、"Start Checks and Rollback" 的 launcher 前置条件列表同步改为 "定义提供的 launcher 的前提来自其定义的 `requires` (环境变量、平台、是否须在现有会话内、二进制), 内置 tmux/herdr 的具体前提 (TMUX/TMUX_PANE, HERDR_ENV/HERDR_WORKSPACE_ID) 在规则中保留为示例; foreground/console 的宿主前提 (三路 TTY, 原生 Windows) 与 `auto` 只在容器型 launcher 间解析、不回落的策略原样保留"; `docs/terminal-backend.md` 注明 tmux 已为声明式实现.

## THREAT_MODEL

定义文件由本机用户放置在自己的 share 目录, 以当前用户身份执行其声明的程序, 与现有 `agents.*.path` 一致, 不构成新的用户间权限边界. kander 保留的边界: 不做 shell 插值, 拒绝控制字符, 拒绝 reparse point 与符号链接指向的定义文件 (沿用 `internal/fs` 现有 no-follow 读取), 定义文件不含也不需要任何凭据.

## OUT_OF_SCOPE

- 已有问题: tmux 前台进程名匹配, copy-mode 判定等既有语义只迁移不改; 排除.
- 并发与跨平台加固: Windows 下 argv 编码沿用 `internal/process` 现有实现, 不新增 Windows 原生验证; 排除.
- 共享契约与文档: `docs/terminal-definitions.md`, `AGENTS.md`, `rules/KANDER-KANBAN-RULES.md` 中直接受影响的三处 (命令表 `--launcher`, "Launchers", "Start Checks and Rollback") 纳入, 因为开放 launcher 名直接改变这些条款的承诺; 注意 "Start Checks and Rollback" 亦被 `20260908-agent-definition-embed-task` 改写 (新增 `mode: pane` 的前置拒绝与 started 判定), 两组不可并行改写该节, 本卡执行时先确认该卡的改写已在 `develop` 上并在其之上追加; 其余规则不改; 纳入.
- 相邻功能与后续阶段: herdr 迁移与钩子清单属 `20260908-terminal-herdr-hooks-task`; 一致性检查命令属 `20260908-terminal-conformance-test-task`; 选项面板中编辑终端定义属后续阶段, 本卡只在面板的 launcher 候选列表中列出已加载定义; 排除.

## DISCUSSION

```text
PREREQUISITES: 20260908-terminal-backend-dispatch-task,20260909-process-output-parser-task
```

- 设计结论: 三种解析原语的具体集合 (原样, JSON 字段路径, 正则单捕获组) 是设计选择, 与 agent 审核模板共用同一实现; 若终端迁移需要更强的 JSON 选择能力, 扩展 `json_field` 的路径语法而不是增加第四种原语. 公共结构本轮新增的流形态字段 (见 `20260909-process-output-parser-task` 的权威描述) 与这条不冲突: 它们描述的是输出怎么切分、哪些片段算数、怎么合并, `parse` 的取值集合仍是三种. 终端侧当前只使用整份文档形态.
- 设计结论 (本轮): 公共输出解析结构与占位符转义规则的**权威描述固定在 `20260909-process-output-parser-task` 一处**, 本卡、`20260908-agent-definition-embed-task` 与 `20260908-agent-review-template-task` 只引用不复述. 该结构已因三处各自复述而分叉过两次 (第三轮统一 `source` 取值, 第四轮同步 A1), 复述本身就是分叉源; 本卡的文档条款同样只指向权威描述.
- 设计结论: 定义格式一比一映射 `Backend` 接口操作, 不多不少; 若迁移 tmux 时发现某操作无法用 argv + 三种输出原语表达, 应调整接口拆分操作粒度, 而不是给格式加脚本能力.
- 设计结论: 错误分类必须显式声明. 这是与 "只是一张命令表" 的关键差别, 缺了它存活检查会把命令失败误判成容器消失.
- 设计结论: 同名覆盖整份定义, 不做字段级合并, 避免用户与内置定义版本错位时出现半新半旧的后端.
- 版本策略: `schema_version` 只在破坏兼容时递增; 加可选字段不递增.

- SELF_REVIEW: 通过. 格式要素 (版本号, launchers, ops, errors, 三种输出原语, 占位符规则, 搜索路径与覆盖) 均有对应验收; 威胁模型覆盖定义文件执行边界; 边界把 herdr, 一致性检查与面板编辑留给后续卡. 依赖 `20260908-terminal-backend-dispatch-task` 已写入 PREREQUISITES. SIZE large 合理.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent). 发现: (1) USER_DECISIONS 把三种原语具体化, 用户只确认了 "只有三种", 已改回并把具体集合移入 DISCUSSION, 与 A2 统一为共用实现; (2) launcher 校验开放需说明不让 config import terminal 的机制, 已引用 B1 的注册口. share 目录与发布规则中的 launcher 取值说明核实存在.
- 规则复核 (2026-09-08, 建卡会话再次对照任务组规则): 本卡复用 `internal/process` 的输出解析实现且验收要求不新写解析器, 该实现由组外卡 `20260909-process-output-parser-task` 交付, 属跨组依赖, 已补入 PREREQUISITES; 组外卡须到 `done/` 且交付在 `develop` 上可用才释放.
- 设计结论: steps/poll/on_error/fields 是格式的组合机制, 由 `DeclarativeBackend` 统一执行; 若 B1 的纸面验证发现某操作仍无法表达, 应先调整接口拆分粒度 (回改 B1 的 plan.md) 而不是加脚本.
- 计划: 执行者启动时先写 `plan.md`, 建议阶段: (1) 以 B1 的纸面验证固定格式 (steps/poll/on_error/fields/errors/address/capabilities); (2) 结构体与校验; (3) `DeclarativeBackend` 与假可执行测试; (4) tmux.json 迁移并跑通迁来的全部测试; (5) 搜索路径、注册口与 doctor; (6) 假后端全生命周期端到端; (7) 规则与文档.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) blocking: 每 op 只有 argv + 单 output 表达不了现有 tmux 的轮询、回退、会话复用与多步聚焦, 已定义 steps/poll/on_error/fields 组合机制并要求以 B1 的纸面验证为输入; (2) 占位符拒绝规则与 tmux `#{session_id}` 冲突, 已定义 `{{`/`}}` 转义与模板/运行时校验边界; (3) 只验收 create/run, 已改为假后端全生命周期 (start, WINDOW 解析, liveness, notify, resume, dismiss, focus); (4) 规则改法用词与遗漏两节已修正; (5) 补计划. 五项已修正.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: (1) 公共解析器 `source` 取值与 A2 分叉, 已统一为 A2 交付的全集并在本卡限制子集; (2) 组合机制缺条件执行、结果传递、短路与候选策略, 已定义 `when`/`store`/`stop_on_success`/`candidates` 并补 tmux 三项行为验收; (3) 规则改法用 capabilities 与 binary 表达不了 TMUX/HERDR 环境前提, 已定义 `requires` 与 `auto_priority` 并改规则同步措辞; (4) 与 herdr 卡的 hooks 通用承载责任冲突, 已把 hooks schema、注册表与三态结果归本卡交付. 四项已修正.
- CARD_REVIEW (第四轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第三轮复审). 发现: (1) 一律拒绝控制字符与 tmux 格式串含 TAB 的现有 argv 冲突, 已分模板/运行时两层并允许模板 `\t`, 补逐字节 argv 用例; (2) `requires` 措辞覆盖了 B1 保留的 foreground/console 宿主前提与 auto 策略, 已限定为定义提供的 launcher 并在规则同步中保留宿主前提. 两项已修正.

- SELF_REVIEW (2026-09-09, 合同变更后复核): 通过. 本轮改动限于: 依赖从整张审核模板卡收窄到 `20260909-process-output-parser-task`; 公共结构与占位符转义规则改为只链接其 `docs/output-parsing.md`, 不复述字段集合与原语清单; 终端侧明确当前只使用整份文档形态并给出理由; 注明 "Start Checks and Rollback" 亦被 `20260908-agent-definition-embed-task` 改写、两组不可并行、本卡在其之上追加. 本卡自身的格式、steps/poll/fields/candidates 契约与验收未改动. 无待决的用户问题.
- CARD_REVIEW (2026-09-09, 本轮四次往返): 通过 (独立子代理, 新会话, 只读本组卡片与用户原始需求, 2026-09-09 共四轮往返). 发现并已修正: (1) 验收 1 与验收 2 对逐行流形态一拒一测互相打架, 已改为终端侧不使用也不测试、该分支由公共实现自己的测试覆盖; (2) 复述被下沉到 `docs/terminal-definitions.md`, 已改为只链接权威文档并写明 "两份文档各写一遍只是把分叉源从卡下沉到文档"; (3) DISCUSSION 点名具体字段名, 已改为指向权威描述; (4) 一条历史记录仍指向拆卡前的承载卡, 已改指新卡; (5) 卡内 "B1" 别名与协调侧称呼冲突, 已改用任务 ID. 末轮结论为通过, 无遗留修正项.
## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
