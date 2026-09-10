# Move herdr to a definition file and fix the Go hook list

- TYPE: Feature
- SIZE: small
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

把内置 herdr 后端迁为嵌入的定义文件, 并把无法用 argv 模板表达的部分固定为有名字的 Go 钩子清单. herdr 的绝大多数操作 (tab create, pane run, pane read, agent prompt, pane wait-output, tab close) 都是命令行, 可以由定义文件表达; 例外是经 `HERDR_SOCKET_PATH` Unix socket 的两处协议: 上报 agent 会话的握手, 以及 pane 级聚焦 (原 `internal/focus`, 已由 `20260908-terminal-backend-dispatch-task` 归入 herdr 后端的 `focus` 操作). 本卡把它们保留为钩子 `herdr-socket-session` 与 `herdr-socket-focus`, 定义文件用 `hooks.report_session` 与 `hooks.focus_pane` 引用. 完成后两个内置终端都是定义文件, "内置与自定义共用一套机制" 在终端侧成立.

## USER_DECISIONS

- 与 `20260908-terminal-definition-format-task` 相同的整体决策: 声明式为主, 不可声明化的部分收敛为有名字的钩子清单并写入文档.
- 本轮只建卡, 不启动.

## EXPECTED_OUTCOME

- 定义格式的 `hooks` 对象、钩子注册表与三态结果类型由前置卡 `20260908-terminal-definition-format-task` 交付; 本卡只定义两个挂载点 `report_session` 与 `focus_pane` 的语义 (定义了 `focus_pane` 时 `focus` 操作先走 argv 模板的容器级聚焦再调用钩子), 并注册 `herdr-socket-session` 与 `herdr-socket-focus` 两个钩子实现. 钩子返回三类结果: 成功, 降级 (操作已部分完成, 附说明, 对应 `internal/focus/focus.go:100` 现在 tab 聚焦成功而 pane socket 失败时返回成功并附提示的行为), 失败; `focus` 操作把降级结果作为成功加警告返回给调用方, 与现有行为一致.
- 嵌入的 `herdr.json` 提供 `herdr` launcher, 用 argv 模板表达 tab 创建 (含 `--workspace`, `--cwd`, `--label`, `--no-focus`), pane 就绪等待, pane run, 字面文本投递 (`agent prompt`), 标记等待 (`pane wait-output --match --source recent`), pane 事实查询 (`pane get <pane-id>`, 解析 `result.pane` 下的身份、`agent_status`、会话引用等既有字段, 对应 `internal/probe/herdr.go:59`, 并保留返回 pane ID 与请求 ID 一致性校验), 失败输出读取 (`pane read`, 对应 `internal/launch/agent.go:330`, 只用于错误诊断输出), tab 关闭, 以及 herdr 特有的错误码分类 (用格式卡的 `stdout_json`/`stderr_json` 规则从 stderr 或 stdout 的 JSON 中取 `error.code`, 对应 `internal/probe/herdr.go:17`, 例如 `pane_not_found` 归 gone); 会话上报走 `hooks.report_session`.
- `internal/terminal/herdr` 的 Go 后端删除; 钩子实现文件只保留两处 socket 协议.
- `docs/terminal-definitions.md` 新增 "钩子清单" 一节, 列出每个钩子的名字, 挂载点, 用途, 使用它的内置定义, 并说明 "以下情形仍需提交 Go 代码": socket 或非命令行协议, 平台特定进程树处理.

## ACCEPTANCE_CRITERIA

- [ ] 两个挂载点在定义文档中有语义说明; 引用未注册钩子名的定义加载失败 (机制由格式卡交付, 本卡以 herdr 定义的用例验证错误信息含文件路径, 挂载点与钩子名); `focus` 对钩子的降级结果返回成功加警告, 对失败返回错误; 三种路径各有测试.
- [ ] 嵌入 `herdr.json` 存在, pane 事实查询用 `pane get` 并校验返回 pane ID, `pane read` 只出现在失败输出读取; herdr 错误码分类通过格式卡的 `stdout_json`/`stderr_json` 规则表达, `pane_not_found` 等既有分类结果与改动前一致 (表驱动测试对比); `internal/terminal/herdr` 中除钩子外的 Go 后端代码删除; 改动前针对 herdr 后端的全部测试 (含从 launch, probe, takeover, notify, focus 迁来的用例, 以及 `herdr_pane_command_test`, socket 握手测试, pane 级聚焦测试) 在声明式实现加钩子上保持通过, 未删改期望值.
- [ ] herdr 的 `agent prompt` 投递语义保持: 不使用 `pane run` 投递正文, 不使用 `pane send-keys` 投递文本; 由测试断言 notify 与 dismiss 路径构造的 argv.
- [ ] 钩子注册表在单一 Go 文件维护, 测试断言 "每个注册名都出现在 `docs/terminal-definitions.md` 钩子清单中".
- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `docs/terminal-definitions.md` 钩子清单一节新增; `docs/terminal-backend.md` 与 `AGENTS.md` 包表注明 herdr 已为声明式实现.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 已有问题: herdr 对 agent 类型的识别由 herdr 自身完成, 定义文件不为其安装识别器; socket 握手的超时与错误处理只搬运不改; 排除.
- 并发与跨平台加固: herdr 目前只在 Unix socket 可用的平台上报会话, 不新增平台支持; 排除.
- 共享契约与文档: `docs/terminal-definitions.md`, `docs/terminal-backend.md`, `AGENTS.md` 纳入; 发布规则 `rules/KANDER-KANBAN-RULES.md` 中对 herdr 投递方式的描述保持有效, 不改; 部分纳入.
- 相邻功能与后续阶段: 一致性检查命令属 `20260908-terminal-conformance-test-task`; 把终端钩子与 agent 钩子 (`20260908-agent-exit-session-hook-task`) 合并为同一注册表属后续阶段; 排除.

## DISCUSSION

```text
PREREQUISITES: 20260908-terminal-definition-format-task
```

- 设计结论: 钩子挂载点是格式的一部分, 名字集合是实现的一部分. 新增挂载点需要递增格式讨论, 新增钩子名只需注册与写文档.
- 本卡完成后, `internal/terminal` 下应只剩: 接口, 注册表, 声明式后端, 无容器后端, 钩子实现与嵌入定义.

- SELF_REVIEW: 通过. hooks 挂载点与钩子名的划分明确; 验收要求 herdr 投递语义 (agent prompt, 不用 pane run/send-keys) 由测试断言, 与发布规则一致; 文档漂移测试可判定. 依赖 B2 已写入 PREREQUISITES.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent). 发现: GOAL 称 socket 握手是唯一例外, 但 focus 的 pane 级聚焦同样走 socket, 已增加第二个挂载点 hooks.focus_pane 与钩子 herdr-socket-focus. notify 与 dismiss 的 herdr 投递事实 (agent prompt, pane wait-output) 核对无误.
- 前置能力: 格式卡负责交付 `stdout_json`/`stderr_json` 错误分类、`on_error` 回退、`hooks` schema、钩子注册表与三态结果类型; 本卡只做 herdr 迁移并提供两个 socket 钩子实现, 不扩展格式. 若启动本卡时发现格式卡漏交付任一能力, 按任务组规则报告并退回格式卡, 不在本卡补. SIZE 保持 small 的前提即此.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) pane 事实读取命令写错 (现用 `pane get`, `pane read` 只读失败输出), 已改准并要求校验 pane ID; (2) 现有错误码从 stderr 或 stdout 解 JSON, 前置格式的 "退出码或 stderr 正则" 表达不了, 已在格式卡增加 `stdout_json`/`stderr_json` 规则并在本卡验收对比; (3) 焦点钩子缺部分成功/降级契约, 已定义三态结果; (4) SIZE 取决于前置能力, 已写明前提. 四项已修正.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: 本卡既要新增 hooks schema、注册与三态类型, 又要求前置格式卡已交付并禁止本卡扩展格式, 生产者冲突; 已把通用承载全部归格式卡, 本卡只保留挂载点语义、两个钩子实现与迁移. 已修正.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
