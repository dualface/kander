# 自定义 agent 定义：可执行名、进程名、参数模板与会话语义

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 01:31
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1A:wX:p1W
- STARTED_AT: 2026-09-08 10:12
- FINISHED_AT:
- TASK_BRANCH: agent-executable-override
- RESULT:

## GOAL

让 agent 不再是写死的四个，而是可以按安装声明。原始诉求是 GitHub issue #1 结尾的 "Related"：`config.AgentExecutables`（`internal/config/config.go:53`）是硬编码 `var` map，配置里没有覆盖入口，于是 npm trampoline（启动名 `claude`、pane 前台名 `node`）与改名包装器（`kander-codex`）这两类环境即便 agent 正常运行也永远过不了探测与前台名校验。用户进一步决定把它做成完整的自定义 agent 定义，覆盖可执行名只是其子集。

需要解决的是四件互相独立的事：

1. **可执行名**：PATH 名或绝对路径，用于启动与可用性探测（`internal/launch/session.go:374` `requireAgentProgram`、`internal/menu/agents.go:139` `findAgents`）。
2. **pane 进程名**：`#{pane_current_command}` 的期望值（`internal/launch/tmux.go:338`、`internal/notify/probe.go:125`、`internal/liveness/lookup.go:40`、`internal/takeover/ops.go:222`）。npm trampoline 下它与第 1 项不相等，这是必须拆成两个字段的直接原因。
3. **model / effort 参数形状**：`internal/launch/session.go:326` `agentArguments` 里四家各不相同——codex 的 effort 是单个 `--config model_reasoning_effort="X"`，claude/grok 是 `--effort X`，cursor 没有 effort；越权开关也各不相同；prompt 统一是最后一个位置参数（`internal/launch/commands.go:150`）。
4. **会话身份语义**：`resume` 与 `notify` 直投全靠它。claude/grok 用 kander 生成的 UUID，cursor 要先 `create-chat` 分配 chat id，codex 启动时没有 id、事后扫 `CODEX_HOME` rollout 反查。

## USER_DECISIONS

- 把本卡契约从「仅覆盖可执行名与 pane 进程名」改写为「自定义 agent 定义」，覆盖可执行名作为其子集，一张卡做完（用户在 2026-09-08 会话中明确选择方案 1）。
- 与 `20260908-tmux-pane-exec-prefix-task`（issue #1 的 `exec` 前缀修复）仍是两张独立的卡，互不阻塞。
- 自定义 agent 至少包含 name 与 path 两个字段。

## EXPECTED_OUTCOME

- `config.json` 新增 agent 定义节：既能覆盖内置四个 agent（只填 path / 进程名即可解掉 trampoline 与改名两种形态），也能声明全新的 agent 名。
- model 与 effort 的传递有两条路：声明兼容方言（复用现有四种参数形状），或给出 argv 模板（`{model}` `{effort}` `{session}` 三个占位符，按元素替换）。两者都不通过 shell 拼接，现有注入屏障不变。
- 会话语义可声明：生成 UUID、由分配命令取得、或不支持恢复；不支持恢复时 `resume` 明确拒绝、`notify` 只能走 recover，且在配置校验阶段就告知，而不是运行时才失败。
- 自定义 agent 出现在选项面板「任务执行与模型」的 agent 下拉里可被选中；内置 agent 的可执行名与进程名可在面板直接编辑。
- 未配置任何 agent 定义时，行为与当前完全一致。

## ACCEPTANCE_CRITERIA

- [ ] `config.json` 新增 agent 定义节，每个 agent 至少支持：`path`（PATH 名或绝对路径）、pane 进程名、方言、argv 模板、会话语义；schema 校验接受合法值、拒绝非法值；配置为空时行为与现状逐字节一致。
- [ ] 回退链有单元测试：pane 进程名缺省回退为 path 的 base，path 缺省回退为内置表，内置表缺省回退为 agent 名。
- [ ] `internal/config` 暴露查询函数（启动名 / 进程名 / 参数构造所需定义），上述 6 处调用点全部改走这些函数，不再直接读硬编码 map。
- [ ] 方言路径：声明兼容方言（codex|claude|grok|cursor）的自定义 agent，其 model / effort / 越权开关 / 会话参数与对应内置 agent 逐参数一致，有测试逐项断言。
- [ ] 模板路径：`args.start` 与 `args.resume` 两套 argv 模板，占位符 `{model}` `{effort}` `{session}` 只做**整个 argv 元素内的文本替换**，绝不拼进 shell 命令串；占位符解析为空时丢弃该元素及其紧邻的前一个 flag 元素（与现状 `--model` 空值省略行为一致）；prompt 由 kander 追加为最后一个位置参数，不进模板。上述规则各有测试。
- [ ] 方言与模板同时给出时以模板为准；两者都没给出时配置校验失败并给出可执行提示。
- [ ] 会话语义支持 `generated`（kander 生成 UUID）、`allocated`（先跑声明的命令取 id 并解析）、`none`（不支持恢复）三种；`none` 时 `resume` 明确拒绝并说明原因，`notify` 不尝试直投而走 recover，且该降级在 `kander check` / 配置校验中可见。
- [ ] 校验：path 非空、无控制字符；含路径分隔符时按绝对路径校验存在且可执行，否则按 PATH 名解析；进程名非空、无控制字符；未知字段与非法方言被拒绝。
- [ ] 选项面板：自定义 agent 进入执行 agent 下拉；每个 agent（含内置）的模型字段下新增缩进的「可执行名」「pane 进程名」两行 inline 输入，占位符提示「留空 = 内置默认」，并复用现有 `ModelField.Key()` 的去重语义（`internal/tui/options_form.go:515`）；改动保存后 `config.json` 与 `kander config --json` 可见。
- [ ] `kander doctor` 与面板环境探测使用覆盖后的可执行名，改名的 agent 不再被报成不可用。
- [ ] 与 review 侧既有 `*_REVIEW_BIN` 环境变量覆盖（`internal/review/settings.go:201`）的优先级被显式确定、写进注释与文档，并有测试锁定。
- [ ] `go build ./...`、`go vet ./...`、`go test ./...`、`gofmt` 无输出；`kander check` 通过。
- [ ] 手工验证并记录到 IMPLEMENTATION：(a) 改名包装器（如 `kander-codex`）仅靠配置通过 `kander start` 存活校验与 `kander check`；(b) 一个模板路径的自定义 agent 能被 `start` 起来并通过存活校验。

## THREAT_MODEL

配置文件由本机当前用户拥有，能改配置的人本来就能改 PATH 与 `config.json`，因此自定义 agent 不新增信任边界。必须守住的边界：模板占位符只做 argv 元素内替换，绝不参与 shell 命令串拼装（Windows 侧仍走 `internal/process` 既有的环境变量编码通道）；拒绝控制字符；`allocated` 会话的分配命令同样以 argv 形式执行、不经 shell。pane 进程名比对本来就不是防同用户伪造的安全边界，本卡不改变这一点。reviewer 要求 CLI 以只读模式运行，其只读约束由内置定义保证，因此本卡不把自定义 agent 放进 reviewer 名单。

## OUT_OF_SCOPE

- 既有问题：不修 tmux 包裹 shell 导致前台名变成 `sh` 的失配。理由：属于同一 issue 的另一失配来源，已由 `20260908-tmux-pane-exec-prefix-task` 处理，两卡互不依赖。
- 加固：不实现 `discovered` 会话语义（codex 那种事后扫描 rollout 反查）作为可配置项。理由：其扫描根、文件格式与匹配规则与 codex 强耦合，通用化的收益低于风险；内置 codex 方言继续使用现有实现。
- 共享契约与文档：不把自定义 agent 接入 reviewer（`internal/review/settings.go` 的表与 `*_REVIEW_BIN` 保持原样，只确定优先级）。理由：reviewer 的只读约束依赖每个 CLI 的具体参数，交给用户配置会削弱审核的只读保证。新配置节的文档更新属于本卡。
- 相邻功能：不动 launcher 选择、prompt 生成、notify 投递机制与 TUI 其他面板；面板只负责选择 agent 与编辑可执行名/进程名两个字段，方言、argv 模板、会话语义的编辑本轮只在 `config.json` 手工完成。理由：在 TUI 里编辑嵌套结构的收益低，且不阻塞本卡目标。

## DISCUSSION

- 现场核对（HEAD `7e6fe20`）：`AgentExecutableName()`（`internal/config/config.go:357`）查不到就回退 agent 名；调用点 6 处，分属「启动/探测」与「前台名比对」两类用途——npm trampoline 下这两个值不相等，单键覆盖救不了，这是拆两个字段的硬理由。
- `agentArguments`（`internal/launch/session.go:326`）里四家的差异已逐项核对：codex 的 effort 是 `--config model_reasoning_effort="X"` 单元素、resume 是子命令 `resume <id>`；claude/grok 是 `--effort X` + `--session-id`/`--resume`；cursor 无 effort，且 chat id 需 `create-chat` 预先分配。因此「只给 name + path」无法表达传参，必须补方言或模板。
- 设计判断（本卡作出，非用户决策）：方言为主路径、模板为逃生舱；模板只做 argv 元素内替换并对空值做「丢元素 + 丢前一个 flag」的处理，正是现状 `modelID != ""` 才拼 `--model` 的行为规则化；`discovered` 会话语义不通用化；自定义 agent 本轮不做 reviewer；面板本轮只编辑可执行名与进程名。若用户对其中任一项有不同偏好，属于实现前可一次性调整的细节。
- effort 的合法取值各家不同（cursor 根本没有），配置校验只做「非空 / 无控制字符」这类弱校验，真值由 agent 自身拒绝。
- review 侧另有一份硬编码表（`internal/review/settings.go:134-152`）且已支持 `*_REVIEW_BIN` 覆盖（`:201`）。本卡不合并两张表，但必须把「配置节 vs 环境变量」的优先级定死并测试，否则两套覆盖会互相打架。
- 规模提示：本卡横跨 config / launch / menu / tui / 文档与测试，仍按 small 单文件卡执行；若实现中确认需要独立的 plan 与 report，由 owner 在 working 状态按规则做形态升级。
- SELF_REVIEW: 目标与产出覆盖用户本轮确认的方案 1（自定义 agent 定义，含可执行名覆盖子集）与「至少 name + path」两项决策；把方言/模板/会话三选项写成可判定的验收条件，并在 DISCUSSION 明确标注哪些是本卡的设计判断而非用户决策；边界排除了 `discovered` 会话、reviewer 接入、`exec` 前缀修复，且均不影响本卡目标达成；验收项可判定（配置校验、回退链、逐参数一致性、模板替换规则、面板字段、两项手工验证）。自查未发现必须由用户先行决策才能开工的歧义。

## IMPLEMENTATION

- 2026-09-08：工作目录 `/home/dualf/works/kander/worktrees/agent-executable-override`；分支 `agent-executable-override`；交付目标 `develop`；base `26edb64fcfb654a96afedc30bbaea27e5e918ec7`；提交 `56ec24d4ca46285288cccf219d971a3a8d970523` 已推送。完成 agents 配置、方言/模板、三种会话策略、六处查询接线、面板编辑与独立 reviewer 可用性判断。未获合入授权，审核后保留分支等待确认。
- 交付自查（以下均针对 `56ec24d4ca46285288cccf219d971a3a8d970523`）：
  1. `git diff --check`、`git diff 26edb64fcfb654a96afedc30bbaea27e5e918ec7..HEAD --check`：无输出；冲突标记人工核查未发现。
  2. `git diff --name-only <base>..HEAD` 逐个统计物理行：36 个 Go 文件，最大为 `internal/config/config.go` 864 行，全部低于 1000 行。
  3. `git diff <base>..HEAD -- docs/custom-agents.md README.md AGENTS.md rules/KANDER-KANBAN-RULES.md`：配置、会话、模板及 review 优先级文档同步，相关注释已更新。
  4. `rg` 检查新增 API 的调用及移除的查询包装层：无新增死代码；原六处已使用配置查询。
  5. 人工逐项核查测试目的：方言逐参数断言、模板空值/注入字面值、schema/回退/深拷贝、会话生成/分配/none、面板去重保存、doctor/存活反查/通知/关闭及 review 优先级分别覆盖不同契约；无冗余测试。
  6. `go build ./...`、`go vet ./...`：退出 0、无输出；`gofmt -l <本次所有 Go 文件>` 无输出。`go test ./... -json`（等价全量命令，JSON 用于准确计数）：19 包通过，1079 个测试条目通过（含子测试）、1 个平台测试跳过、0 失败。
  7. Windows 替代证据：`GOOS=windows GOARCH=amd64 go test -exec=true ./internal/config ./internal/launch ./internal/menu ./internal/tui ./internal/liveness ./internal/notify ./internal/takeover ./internal/probe ./internal/review` 9 包交叉编译通过；未在原生 Windows 执行测试。
- 手工验证（`56ec24d4ca46285288cccf219d971a3a8d970523`）：用该提交构建 `/tmp/kander-agent-under-test`，运行 `/tmp/kander-agent-smoke.py`。临时目录 `/tmp/kander-agent-smoke-4dxcs0ru` 内使用独立配置、看板与 Go CLI 测试替身，真实 tmux-session 启动。仅配置 codex.path 为改名程序后，`start` 成功、前台名 `kander-codex`、`check` 为 alive / `ok: 1 tasks`；自定义 template 使用 start argv 模板，`start` 成功、前台名 `template-agent`、`check` 为 alive / `ok: 1 tasks`。捕获 argv 确认 prompt 最后追加、空 model flag 被省略。两个测试窗口已关闭。测试替身验证进程/传参/探测链路，不声称真实 LLM 服务认证或输出已验证。
- 边界：herdr 仍需要自身识别 agent；全新模板 agent 使用 tmux/tmux-session。`none` 通知运行 start 模板，不直投；持久派回仍保留停止事实/回执门禁。审核使用批次 `agent-definitions-batch`，PM/QA 待执行，CSA/Hacker 按仓库规则 N/A。报告原件只引用 run ID。

## SUMMARY

<FILL_IN>

## CONTRACT_DECISIONS

```json
{
  "at": "2026-09-08 01:53",
  "decision": "用户在 2026-09-08 会话中，针对 GitHub issue #1 \"Related\" 诉求的两种走法（1. 把本卡契约改写为自定义 agent 定义；2. 本卡保持最小、另开一张自定义 agent 卡）明确选择 1。\n\n同时确认：自定义 agent 至少包含 name 与 path 两个字段；关键待解问题是 model 与 effort 参数如何传递。\n\n据此改写的冻结字段：GOAL、USER_DECISIONS、EXPECTED_OUTCOME、ACCEPTANCE_CRITERIA、OUT_OF_SCOPE 由「仅覆盖可执行名与 pane 进程名」扩展为「自定义 agent 定义（可执行名、pane 进程名、方言/argv 模板、会话语义、面板可选可编辑）」，原范围成为其子集。与 20260908-tmux-pane-exec-prefix-task 的分卡关系不变。\n",
  "Before": {
    "ACCEPTANCE_CRITERIA": "- [ ] `config.json` 新增按 agent 的覆盖节(每个 agent 可分别给出「启动可执行名」与「pane 进程名」两项),schema 校验接受合法值、拒绝非法值,缺省与现状行为逐字节一致。\n- [ ] 「pane 进程名」缺省时回退为「启动可执行名」的 base,「启动可执行名」缺省时回退为内置表,内置表缺省回退为 agent 名——回退链有单元测试覆盖。\n- [ ] `internal/config` 暴露两个查询函数(启动名 / 进程名),6 处调用点(`launch/session.go`、`menu/agents.go`、`launch/tmux.go`、`notify/probe.go`、`liveness/lookup.go`、`takeover/ops.go`)全部改用对应函数,不再直接读硬编码 map。\n- [ ] 校验:启动可执行名不得为空串、不得含路径分隔符与控制字符(只接受 PATH 上的名字,不接受绝对路径);pane 进程名同样不得为空串或含控制字符;未知 agent 名的键被拒绝。\n- [ ] 选项面板在「任务执行与模型」与「审核与模型」两屏,于每个 agent / reviewer 的模型字段下新增两行缩进 inline 输入,占位符提示「留空 = 内置默认」;两屏选中同一 agent 时按现有 `ModelField.Key()` 同款去重机制只显示一次;改完保存后 `config.json` 与 `kander config --json` 可见。\n- [ ] `kander doctor` / 选项面板的环境探测使用覆盖后的可执行名,覆盖生效后不再把改名的 agent 报成不可用。\n- [ ] 与 review 侧既有的 `*_REVIEW_BIN` 环境变量覆盖(`internal/review/settings.go:201`)的优先级被显式确定并写进注释与文档,且有测试锁定。\n- [ ] 新增单元测试:回退链、校验拒绝、6 处调用点取值、面板字段去重与写回;`go build ./...`、`go vet ./...`、`go test ./...`、`gofmt` 无输出;`kander check` 通过。\n- [ ] 手工验证并记录到 IMPLEMENTATION:构造一个改名包装器(如 `kander-codex`),仅通过配置让 `kander start` 存活校验与 `kander check` 通过。",
    "EXPECTED_OUTCOME": "- `config.json` 新增一个按 agent 覆盖的配置节,可分别覆盖「启动用的可执行名」与「pane 前台进程名期望值」;未配置时与当前内置默认(`codex` / `claude` / `grok` / `cursor-agent`)完全一致。\n- 覆盖值对 6 处调用点统一生效:PATH 解析与 agent 可用性探测走覆盖后的可执行名;tmux 前台名校验、notify 直投与探针、liveness 反查、dismiss/takeover 走覆盖后的进程名期望值。\n- npm trampoline(启动名 `claude`、前台名 `node`)与改名包装器(启动名与前台名都是 `kander-codex`)两种形态,均可只靠配置通过 `start` 存活校验、`notify` 直投与 `kander check`。\n- 选项面板「任务执行与模型」「审核与模型」两屏可直接编辑这两个字段,留空表示用内置默认。\n- `kander config --json` 输出这个配置节;非法值在保存/读取时被拒绝并给出可执行的提示,不会写出一个会让 `start` 静默失败的配置。",
    "GOAL": "实现 GitHub issue #1 结尾 \"Related\" 的诉求:`config.AgentExecutables`(`internal/config/config.go:53`)是写死的 `var` map,配置里没有任何覆盖入口,`AgentExecutableName()` 只查这张表。于是两类环境即便 agent 正常运行也永远过不了校验或探测:\n\n- npm trampoline:入口是包装脚本,pane 前台真正占住的是 `node` 之类,不是 `claude` / `codex`。\n- 改名的包装器:例如自建 `kander-codex`,PATH 上没有 `codex`,前台名也不是 `codex`。\n\n关键区别:这张表同时承担两件事,而这两类环境下它们的取值并不相同——\n\n- 启动/探测用的可执行名(PATH 查找):`internal/launch/session.go:374` `requireAgentProgram`、`internal/menu/agents.go:139` `findAgents`。\n- pane 前台进程名的期望值:`internal/launch/tmux.go:338`、`internal/notify/probe.go:125`、`internal/liveness/lookup.go:40`、`internal/takeover/ops.go:222`。\n\n因此需要让这两项分别可按安装覆盖,并在选项面板里可配置,让用户不必再靠改名、软链、包装二进制去迁就硬编码表。",
    "OUT_OF_SCOPE": "- 既有问题:不修 tmux 包裹 shell 导致的前台名变成 `sh` 的失配。理由:那是同一 issue 的另一个失配来源,已由 `20260908-tmux-pane-exec-prefix-task` 处理,两卡互不依赖。\n- 加固:不为 agent 引入完整的自定义 agent 注册(新增 agent 种类、参数模板、session 解析)。理由:本卡只让已有四个 agent 的可执行名与进程名可覆盖,新增 agent 是另一个数量级的契约变更。\n- 共享契约与文档:不改 `AgentExecutables` 之外的 agent 相关契约(参数拼装、session 发现、rollout 查找);更新配置文档中新配置节的说明属于本卡。\n- 相邻功能:不动 launcher 选择、review 的其他设置项与 TUI 其他面板;`*_REVIEW_BIN` 只确定优先级,不废弃、不迁移。",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "- 该诉求与 issue #1 的 `exec` 前缀修复分成两张独立的卡,本卡只做可配置化。"
  },
  "After": {
    "ACCEPTANCE_CRITERIA": "- [ ] `config.json` 新增 agent 定义节，每个 agent 至少支持：`path`（PATH 名或绝对路径）、pane 进程名、方言、argv 模板、会话语义；schema 校验接受合法值、拒绝非法值；配置为空时行为与现状逐字节一致。\n- [ ] 回退链有单元测试：pane 进程名缺省回退为 path 的 base，path 缺省回退为内置表，内置表缺省回退为 agent 名。\n- [ ] `internal/config` 暴露查询函数（启动名 / 进程名 / 参数构造所需定义），上述 6 处调用点全部改走这些函数，不再直接读硬编码 map。\n- [ ] 方言路径：声明兼容方言（codex|claude|grok|cursor）的自定义 agent，其 model / effort / 越权开关 / 会话参数与对应内置 agent 逐参数一致，有测试逐项断言。\n- [ ] 模板路径：`args.start` 与 `args.resume` 两套 argv 模板，占位符 `{model}` `{effort}` `{session}` 只做**整个 argv 元素内的文本替换**，绝不拼进 shell 命令串；占位符解析为空时丢弃该元素及其紧邻的前一个 flag 元素（与现状 `--model` 空值省略行为一致）；prompt 由 kander 追加为最后一个位置参数，不进模板。上述规则各有测试。\n- [ ] 方言与模板同时给出时以模板为准；两者都没给出时配置校验失败并给出可执行提示。\n- [ ] 会话语义支持 `generated`（kander 生成 UUID）、`allocated`（先跑声明的命令取 id 并解析）、`none`（不支持恢复）三种；`none` 时 `resume` 明确拒绝并说明原因，`notify` 不尝试直投而走 recover，且该降级在 `kander check` / 配置校验中可见。\n- [ ] 校验：path 非空、无控制字符；含路径分隔符时按绝对路径校验存在且可执行，否则按 PATH 名解析；进程名非空、无控制字符；未知字段与非法方言被拒绝。\n- [ ] 选项面板：自定义 agent 进入执行 agent 下拉；每个 agent（含内置）的模型字段下新增缩进的「可执行名」「pane 进程名」两行 inline 输入，占位符提示「留空 = 内置默认」，并复用现有 `ModelField.Key()` 的去重语义（`internal/tui/options_form.go:515`）；改动保存后 `config.json` 与 `kander config --json` 可见。\n- [ ] `kander doctor` 与面板环境探测使用覆盖后的可执行名，改名的 agent 不再被报成不可用。\n- [ ] 与 review 侧既有 `*_REVIEW_BIN` 环境变量覆盖（`internal/review/settings.go:201`）的优先级被显式确定、写进注释与文档，并有测试锁定。\n- [ ] `go build ./...`、`go vet ./...`、`go test ./...`、`gofmt` 无输出；`kander check` 通过。\n- [ ] 手工验证并记录到 IMPLEMENTATION：(a) 改名包装器（如 `kander-codex`）仅靠配置通过 `kander start` 存活校验与 `kander check`；(b) 一个模板路径的自定义 agent 能被 `start` 起来并通过存活校验。",
    "EXPECTED_OUTCOME": "- `config.json` 新增 agent 定义节：既能覆盖内置四个 agent（只填 path / 进程名即可解掉 trampoline 与改名两种形态），也能声明全新的 agent 名。\n- model 与 effort 的传递有两条路：声明兼容方言（复用现有四种参数形状），或给出 argv 模板（`{model}` `{effort}` `{session}` 三个占位符，按元素替换）。两者都不通过 shell 拼接，现有注入屏障不变。\n- 会话语义可声明：生成 UUID、由分配命令取得、或不支持恢复；不支持恢复时 `resume` 明确拒绝、`notify` 只能走 recover，且在配置校验阶段就告知，而不是运行时才失败。\n- 自定义 agent 出现在选项面板「任务执行与模型」的 agent 下拉里可被选中；内置 agent 的可执行名与进程名可在面板直接编辑。\n- 未配置任何 agent 定义时，行为与当前完全一致。",
    "GOAL": "让 agent 不再是写死的四个，而是可以按安装声明。原始诉求是 GitHub issue #1 结尾的 \"Related\"：`config.AgentExecutables`（`internal/config/config.go:53`）是硬编码 `var` map，配置里没有覆盖入口，于是 npm trampoline（启动名 `claude`、pane 前台名 `node`）与改名包装器（`kander-codex`）这两类环境即便 agent 正常运行也永远过不了探测与前台名校验。用户进一步决定把它做成完整的自定义 agent 定义，覆盖可执行名只是其子集。\n\n需要解决的是四件互相独立的事：\n\n1. **可执行名**：PATH 名或绝对路径，用于启动与可用性探测（`internal/launch/session.go:374` `requireAgentProgram`、`internal/menu/agents.go:139` `findAgents`）。\n2. **pane 进程名**：`#{pane_current_command}` 的期望值（`internal/launch/tmux.go:338`、`internal/notify/probe.go:125`、`internal/liveness/lookup.go:40`、`internal/takeover/ops.go:222`）。npm trampoline 下它与第 1 项不相等，这是必须拆成两个字段的直接原因。\n3. **model / effort 参数形状**：`internal/launch/session.go:326` `agentArguments` 里四家各不相同——codex 的 effort 是单个 `--config model_reasoning_effort=\"X\"`，claude/grok 是 `--effort X`，cursor 没有 effort；越权开关也各不相同；prompt 统一是最后一个位置参数（`internal/launch/commands.go:150`）。\n4. **会话身份语义**：`resume` 与 `notify` 直投全靠它。claude/grok 用 kander 生成的 UUID，cursor 要先 `create-chat` 分配 chat id，codex 启动时没有 id、事后扫 `CODEX_HOME` rollout 反查。",
    "OUT_OF_SCOPE": "- 既有问题：不修 tmux 包裹 shell 导致前台名变成 `sh` 的失配。理由：属于同一 issue 的另一失配来源，已由 `20260908-tmux-pane-exec-prefix-task` 处理，两卡互不依赖。\n- 加固：不实现 `discovered` 会话语义（codex 那种事后扫描 rollout 反查）作为可配置项。理由：其扫描根、文件格式与匹配规则与 codex 强耦合，通用化的收益低于风险；内置 codex 方言继续使用现有实现。\n- 共享契约与文档：不把自定义 agent 接入 reviewer（`internal/review/settings.go` 的表与 `*_REVIEW_BIN` 保持原样，只确定优先级）。理由：reviewer 的只读约束依赖每个 CLI 的具体参数，交给用户配置会削弱审核的只读保证。新配置节的文档更新属于本卡。\n- 相邻功能：不动 launcher 选择、prompt 生成、notify 投递机制与 TUI 其他面板；面板只负责选择 agent 与编辑可执行名/进程名两个字段，方言、argv 模板、会话语义的编辑本轮只在 `config.json` 手工完成。理由：在 TUI 里编辑嵌套结构的收益低，且不阻塞本卡目标。",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "- 把本卡契约从「仅覆盖可执行名与 pane 进程名」改写为「自定义 agent 定义」，覆盖可执行名作为其子集，一张卡做完（用户在 2026-09-08 会话中明确选择方案 1）。\n- 与 `20260908-tmux-pane-exec-prefix-task`（issue #1 的 `exec` 前缀修复）仍是两张独立的卡，互不阻塞。\n- 自定义 agent 至少包含 name 与 path 两个字段。"
  }
}
```
