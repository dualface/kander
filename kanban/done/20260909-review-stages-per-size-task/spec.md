# 审核阶段策略按 large/small 任务规格分档

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-config-layering-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 02:04
- OWNER: cursor
- SESSION: cursor ec197269-b129-4485-ac57-313cdf331a95
- WINDOW: herdr:w2R:t6:w2R:p8
- STARTED_AT: 2026-09-09 02:09
- FINISHED_AT: 2026-09-09 04:48
- TASK_BRANCH: N/A
- RESULT: completed

- DISPATCH_ID: wrapup-c1

- EXECUTION_EPOCH: 6

## GOAL

让 `review_stages` 配置可以为 `large` 与 `small` 两种任务规格分别指定四个审核角色(PM、QA、CSA、Hacker)的阶段策略(`auto`/`skip`/`required`),使大任务可以走更严格的审核流程、小任务走更轻的流程。现有平铺结构 `{角色: 模式}` 只能表达一套策略,无法按规格区分。

实现范式照抄现有的 `kanban_agents.large/small`:配置结构、校验、默认回填、访问器、doctor 修复、摘要折叠、TUI 按档循环。

## USER_DECISIONS

- 新结构为 `"review_stages": {"large": {角色: 模式}, "small": {角色: 模式}}`。
- 旧的平铺结构 `{角色: 模式}` 继续合法:读入时同时作用于两档;保存时统一写出新结构。
- 规则文档中"审核阶段"第 3 级优先级改为"配置中该卡 `SIZE` 对应档位的 `review_stages` 值;任务组批次含多种规格时按 `large` 档"。
- 混合形态(档名与角色出现在同一层,如 `{"large": {...}, "PM": "auto"}`)视为非法,报可读错误并指出冲突的键。
- 导出规范化函数(建议名 `config.NormalizeReviewStages(raw any) (map[string]any, error)`),把旧平铺原始 JSON 规范化为两档原始结构,供后置卡在深合并前对作用域配置调用。

## EXPECTED_OUTCOME

- `internal/config` 接受并输出按档的 `review_stages`;旧平铺配置加载后 `kander config --json` 输出两档内容相同的新结构;缺失的档或角色回填为 `auto`;未知档名、未知角色、非法模式值报错。
- 新增访问器 `config.ReviewStageFor(cfg, scale, role)`,现有读取 `cfg.ReviewStages[role]` 的调用点(`internal/menu`、`internal/tui`、`internal/flow`、`internal/config/format.go`)全部改为按档读取。
- `kander config`(非 JSON)摘要按档展示阶段策略,两档完全相同时折叠为一行。
- `kander doctor` 修复:缺失一档时从另一档回填,两档都缺时回填默认值。
- TUI 选项面板"审核与模型"分区:每个角色下按 large/small 各有一个阶段选择,保存后写入新结构;`internal/flow` 的角色列表按档列出。
- 规则文档 `rules/KANDER-REVIEW-RULES.md` "Review Stages" 一节、`AGENTS.md` 中 `review_stages` 的说明、`docs/custom-agents.md` 同步更新;i18n 三语(en、zh-CN、ja)文案补齐。

## ACCEPTANCE_CRITERIA

- [ ] `config.Validate` 接受 `{"large": {...}, "small": {...}}`,接受旧平铺 `{角色: 模式}` 并映射到两档,拒绝未知档名、未知角色、非法模式以及档名与角色同层的混合形态(错误信息含冲突键名);每种情形都有单元测试。
- [ ] 加载旧平铺配置后执行 `Save`,文件内 `review_stages` 为两档结构,且两档内容相同;有往返测试。
- [ ] 导出的规范化函数对旧平铺、两档、混合形态三种原始输入的行为有单元测试。
- [ ] 缺失整个 `review_stages` 节、缺失某一档、缺失某一角色时,均回填 `auto`,并有测试覆盖。
- [ ] `config.ReviewStageFor(cfg, scale, role)` 存在并有测试;仓库中不再有按 `cfg.ReviewStages[role]` 平铺读取的调用。
- [ ] `kander config --json` 输出新结构;`kander config` 摘要在两档相同时折叠为一行、不同时分两行展示,有 `format.go` 测试覆盖。
- [ ] `internal/config/repair.go` 的修复逻辑覆盖缺档回填,有测试。
- [ ] TUI 选项面板每个角色按 large/small 提供阶段选择并能保存,`internal/menu`、`internal/tui`、`internal/flow` 现有测试更新并通过。
- [ ] `rules/KANDER-REVIEW-RULES.md` "Review Stages" 第 3 级明确写出"按该卡 `SIZE` 对应档位取值;任务组批次含多种规格时按 `large` 档";`AGENTS.md` 与 `docs/custom-agents.md` 的配置说明同步;`rules/embed_test.go` 与 `internal/install` 的规则摘要相关测试通过。
- [ ] en、zh-CN、ja 三个 i18n 文件对新增文案键齐全,`go test ./...` 与 `go vet ./...` 通过。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 现有问题:不修改审核流程本身(阶段顺序、触发条件、批次机制)以及 `kander review` 的参数;原因是本卡只改配置的表达粒度。
- 加固:不在 `internal/board` 或 `internal/review` 中按 SIZE 强制校验 `--requirements-file`;原因是现状即为 Agent 侧解析,强制执行是另一项设计决策。
- 共享契约与文档:不改 `kanban_agents`、`models.*` 等其他配置键的结构;README 三语不改,因为它们目前不描述 `config.json` 细节。
- 相邻功能:不实现项目目录 `.kander-config.json` 覆盖(由后续卡 `20260909-project-config-overlay-task` 负责)。

## DISCUSSION

```text
PREREQUISITES: N/A
```

- 任务组 `20260909-config-layering-group` 共 2 张卡,线性依赖:本卡先行,`20260909-project-config-overlay-task` 依赖本卡。原因:两卡都改 `internal/config/config.go`、`format.go`、`kander config` 输出与同一批规则文档,并行会冲突。
- 现状:`review_stages` 在代码中没有强制消费方,只经 `kander config --json` 和 `rules/KANDER-REVIEW-RULES.md` "Review Stages" 四级优先级传给 Agent;Go 侧消费者是 `internal/menu/options.go`、`internal/tui/options_form.go`、`internal/flow/flow.go`、`internal/config/format.go`。
- 参照实现:`kanban_agents` 的 `validateKanbanAgents`、`KanbanAgentFor`、`repair.go` 回填、`FormatKanbanAgentsSummary` 折叠、TUI 按 `config.TaskScales` 循环。
- 本仓库 `AGENTS.md` 规定 CSA/Hacker 恒为 N/A;这不影响配置结构,只影响本卡的审核角色。
- 说明(非用户决策):本卡不在 Go 代码中强制执行阶段策略,与现状一致,策略经 `kander config --json` 与规则文本传给 Agent;见 OUT_OF_SCOPE。
- 规范化函数的动机:后置卡在校验前深合并原始 JSON,若作用域配置仍是旧平铺而覆盖文件写两档,合并会产生混合形态;后置卡在合并前调用本卡导出的规范化函数消除该情形。
- CARD_REVIEW: 独立评审(新会话 Claude 子代理,只读两张卡与用户原始需求)指出三处问题并已修正:把"不在 Go 中强制执行"从 USER_DECISIONS 移到 DISCUSSION 说明;补充混合形态报错的决策与验收;补充旧平铺加载后 Save 写出两档的往返验收;为消除与后置卡的跨卡缺口,新增导出规范化函数及其验收。评审结论:修正后通过。
- SELF_REVIEW: 已对照用户需求与确认方案复核。目标与产出一致;新结构、旧结构兼容、规则第 3 级改法均来自用户确认的方案而非建议;边界排除了强制执行与项目覆盖两项并给出原因;验收条目逐条可判定且覆盖代码、TUI、文档、i18n。未发现需要新用户决策的歧义。

## IMPLEMENTATION

- 2026-09-09 首轮交付 `e09651152ea335ded58a504239d479a1ae0ba621`;修复轮交付 `9a2badc1bdb23dd946020aaaa950501081dbbd5c`。
- 2026-09-09 审核处置:门禁项已修,QA-104/PM-08 各轮 deferred。
- 2026-09-09 wrap-up:组 HEAD/`develop` `4832ee9db7912db15d297a32ee8888fff757bf4a`;已删本卡 worktree 与任务分支;完成报告 `report.md`。
## SUMMARY

- 验收:review_stages 按 large/small 分档已交付并进入 `develop`(`4832ee9db7912db15d297a32ee8888fff757bf4a`);本卡最终 SHA `9a2badc1bdb23dd946020aaaa950501081dbbd5c`。
- 审核:PM/QA 通过于 `4832ee9`(run `pm-batch-one-r4`/`qa-batch-one-r4`);CSA/Hacker N/A;批次 `batch-one` 已关闭。
- 未解决项:
  - QA suggest deferred:reviews/qa-batch-one-r4/dispositions/qa-104-deferred-r4.json
  - PM low deferred:reviews/pm-batch-one-r4/dispositions/pm-08-deferred-r4.json
## REVIEWS

- {"run_id":"qa-batch-one-r1","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/qa-batch-one-r1/report.md"}
- {"run_id":"pm-batch-one-r1","batch_id":"batch-one","role":"PM","execution_status":"failed","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/pm-batch-one-r1/report.md"}
- {"run_id":"pm-batch-one-r2","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/pm-batch-one-r2/report.md"}
- {"run_id":"pm-batch-one-r3","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5d632af67ee28a19769972298a11984ced5e76e6","previous_run_id":"pm-batch-one-r2","report":"reviews/pm-batch-one-r3/report.md"}
- {"run_id":"qa-batch-one-r2","batch_id":"batch-one","role":"QA","execution_status":"failed","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"777b1f088985e3d11cec3bd9b4dde9271822119e","previous_run_id":"qa-batch-one-r1","report":"reviews/qa-batch-one-r2/report.md"}
- {"run_id":"qa-batch-one-r3","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"777b1f088985e3d11cec3bd9b4dde9271822119e","previous_run_id":"qa-batch-one-r1","report":"reviews/qa-batch-one-r3/report.md"}
- {"run_id":"qa-batch-one-r4","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"4832ee9db7912db15d297a32ee8888fff757bf4a","previous_run_id":"qa-batch-one-r3","report":"reviews/qa-batch-one-r4/report.md"}
- {"run_id":"pm-batch-one-r4","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"4832ee9db7912db15d297a32ee8888fff757bf4a","previous_run_id":"pm-batch-one-r3","report":"reviews/pm-batch-one-r4/report.md"}
