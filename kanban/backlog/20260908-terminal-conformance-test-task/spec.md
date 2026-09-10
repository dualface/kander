# kander terminal test <name>: terminal definition conformance check

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-terminal-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:18
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

新增 `kander terminal test <name>` 子命令, 对一份终端定义在真实终端程序上逐操作跑一遍并校验解析结果, 作为外部贡献者提交新终端定义的验收工具, 也是用户排查存活误判的第一手段. 没有它, 贡献者写定义文件只能靠猜, 用户遇到的是存活检查误判这类间接故障.

## USER_DECISIONS

- 终端侧最后一步做一致性检查命令, 作为外部贡献新终端定义的验收工具.
- 本轮只建卡, 不启动.

## EXPECTED_OUTCOME

- `kander terminal test <name> [--keep] [--skip-focus]`: 按定义创建一个带唯一标签的容器, 依次执行 `Backend` 的每个操作并校验: 等待就绪; 在 pane 中运行一条输出唯一标记的命令并用 "等待标记" 操作确认; 写入元数据后读回并比对; 在 pane 中启动一个已知前台程序 (例如 `sleep`) 后读取前台进程并比对; 读取 pane 所属容器与 pane 数; 可选切焦点 (`--skip-focus` 跳过); 投递字面文本与 Enter 并确认回显; 关闭容器; 关闭后再次读取 pane 事实, 确认被分类为 "容器已消失" 而非普通失败. 每步输出 pass/fail 与实际 argv, stdout/stderr 摘要.
- 任一步失败退出非零, 并在退出前尽力关闭已创建的容器; 定义文件加载失败时在创建容器前报错. `--keep` 只影响破坏性收尾: 跳过 "关闭容器" 与 "关闭后 gone 分类" 两步并逐步输出 skip, 打印保留的容器地址; 其余步骤照常执行. 切焦点一步需要已附着的客户端: 无客户端时该步输出 skip 并说明原因 (等同 `--skip-focus`), 不算失败. 定义的 `capabilities` 声明不支持的能力 (例如不支持元数据或不报告前台进程) 对应的成功检查按能力跳过并输出 skip, 但仍校验该操作返回接口契约规定的 "不支持" 结果而不是普通失败; 声明支持却失败的仍判失败.
- `kander terminal list`: 列出已加载的定义, 来源 (嵌入, 全局, 项目) 与校验结果. `doctor` 复用同一列表.
- 命令名与文案进入 `internal/cli` 命令表与 i18n 三语目录.

## ACCEPTANCE_CRITERIA

- [ ] `internal/cli` 命令表新增 `terminal`, 子命令 `test` 与 `list`, 用法文案在 cn/en/ja 三份 locale 中存在, i18n 缺键测试通过.
- [ ] `terminal test` 覆盖 `Backend` 的每个操作, 检查步骤清单与接口操作一一对应, 由测试断言 "接口每个方法都有对应检查步骤", 防止后续新增操作漏检; 每个步骤知道自己依赖的能力标志, 按定义的 `capabilities` 决定执行成功检查还是 "不支持" 契约检查.
- [ ] 用两个假终端定义验证按能力跳过: 一个不支持元数据, 一个不报告前台进程; 各自对应步骤输出 skip 且 "不支持" 返回符合接口契约时整体通过, 若该操作返回普通失败则整体失败.
- [ ] 用测试内构造的假终端定义 (shell 脚本或测试二进制) 验证: 全部通过时退出 0 并打印每步 pass; 故意让 "读元数据" 返回错值时退出非零, 输出指出该步与实际值, 且容器已关闭; 加 `--keep` 时关闭与 gone 检查两步输出 skip, 容器保留并打印其地址, 其余步骤仍执行.
- [ ] 对嵌入的 `tmux` 定义, 在本机存在 tmux 时 (`go test` 以环境探测决定是否运行) 用隔离的 tmux 服务器 (独立 `-L`/`-S` socket 或 `TMUX_TMPDIR` 指向临时目录) 真实跑通全部步骤, 不触碰用户已有的 tmux 会话; 切焦点一步 (`switch-client`, 对应 `internal/focus/focus.go:84` 与 `:115`) 需要附着客户端, 测试在能分配 PTY 的环境下用隔离 socket 附着一个客户端跑完整聚焦步骤, 否则验证该步的 skip 路径; herdr 定义只在显式设置 `KANDER_E2E_HERDR=1` 且 `HERDR_SOCKET_PATH` 可用时运行, 否则跳过并说明. 本卡在 herdr 迁移卡之后执行, 对两份嵌入定义的最终版本都出具检查输出并附在 IMPLEMENTATION.
- [ ] `terminal list` 与 doctor 对同一份损坏定义给出相同的校验结论.
- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `docs/terminal-definitions.md` 新增 "提交新终端定义" 一节: 要求附 `kander terminal test <name>` 的完整输出; `AGENTS.md` 命令接线说明与 `rules/KANDER-KANBAN-RULES.md` 命令表新增 `terminal` 子命令.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 已有问题: herdr 与 tmux 后端既有语义不改, 本卡只检查不修; 排除.
- 并发与跨平台加固: 不新增 Windows 原生验证; Windows 上 `terminal test` 只要求能加载定义并报告 "无可用终端" 而不崩溃; 排除.
- 共享契约与文档: `docs/terminal-definitions.md`, `AGENTS.md`, `rules/KANDER-KANBAN-RULES.md` 命令表纳入; 纳入.
- 相邻功能与后续阶段: agent 定义的一致性检查 (对 `agents.*` 跑 start/review 模板) 属后续阶段; 选项面板集成属后续阶段; 排除.

## DISCUSSION

```text
PREREQUISITES: 20260908-terminal-definition-format-task,20260908-terminal-herdr-hooks-task
```

- 设计结论: 一致性检查与搜索路径/优先级是定义文件方案的两项必要配套, 后者已在 `20260908-terminal-definition-format-task`.
- 设计结论: 检查步骤与接口方法一一对应并由测试锁定, 使 "加操作必加检查" 成为编译期约束之外的第二道保障.
- 顺序: 按用户决策 "终端侧最后一步做一致性检查", 本卡依赖格式卡与 herdr 迁移卡, 是组内最后一张, 对两份最终嵌入定义 (tmux, herdr) 出具验收输出.
- 注意: `internal/cli/cli.go` 顶部注释写有 "The command table in this file is registered once. Later implementation cards only override their Runner; they must not edit the command name list or main.go just to wire themselves in.", 那是首轮建包时的约束; 本卡新增 `terminal` 子命令属于正当扩展, 应同步更新该注释与 `AGENTS.md` 的命令接线说明, 使二者一致.

- SELF_REVIEW: 补充一条 DISCUSSION: cli 命令表注释与本卡新增子命令的关系, 要求同步更新, 避免执行者误判为禁止. 其余: 检查步骤与接口方法一一对应并由测试锁定, 失败清理与 --keep 语义可判定; 真实 tmux/herdr 检查按环境探测决定运行, 不会让 CI 因缺终端失败.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent). 发现: (1) USER_DECISIONS 中 "两件配套" 是论证, 已改为用户确认的 "最后做一致性检查命令", 论证移入 DISCUSSION; (2) go test 中真实跑 tmux/herdr 与 AGENTS.md 的测试隔离要求有张力, 已改为隔离 tmux socket 与显式环境变量门控 herdr; (3) cli 注释改为英文原文引用.
- 计划: 本卡改为 large (新增命令、通用检查器、真实终端生命周期与隔离设施), 执行者启动时先写 `plan.md`, 建议阶段: (1) 命令与 i18n; (2) 与接口方法一一对应的检查步骤及锁定测试; (3) 假终端定义的通过/失败/keep 路径; (4) 隔离 tmux 服务器与 PTY 客户端; (5) herdr 门控; (6) 文档与规则命令表.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) 用户决策写 "最后一步" 而依赖不含 herdr 迁移卡, 已加前置并删除并行表述; (2) `--keep` 与 "执行关闭及 gone 检查" 冲突, 已定义 keep 跳过的步骤与输出; (3) 只装 tmux 跑不了 `switch-client` 聚焦步骤, 已要求隔离客户端或 skip 路径; (4) 改 large 并补计划. 四项已修正.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: 检查要求所有操作成功, 但格式卡允许定义不支持部分能力, 合法定义会被判失败; 已改为按 `capabilities` 跳过成功检查并校验 "不支持" 契约, 补两个假定义用例. 已修正.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
