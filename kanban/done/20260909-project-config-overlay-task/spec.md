# 项目目录 .kander-config.json 覆盖作用域配置

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-config-layering-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 02:04
- OWNER: cursor
- SESSION: cursor 409f7740-a172-4db9-b1e5-6d027a031821
- WINDOW: herdr:w2R:t7:w2R:p9
- STARTED_AT: 2026-09-09 02:24
- FINISHED_AT: 2026-09-09 04:48
- TASK_BRANCH: 20260909-project-config-overlay
- RESULT: completed

- DISPATCH_ID: wrapup-c2

- EXECUTION_EPOCH: 8

## GOAL

允许项目在其 Git 主工作树根目录放置一个可提交到版本库的 `.kander-config.json`,其中的键覆盖当前作用域(全局 `~/.config/kander/config.json` 或项目安装 `.kander/config.json`)的同名配置,使同一份 `kander` 二进制在不同项目里运行时得到项目专属的执行 Agent、审核者、审核阶段策略、模型档位、模块开关等设置。

当前配置路径只由二进制所在位置决定(`internal/config/paths.go` 的 `InstallPathsFromEntry`),不看当前目录,也没有任何配置分层或合并逻辑。

## USER_DECISIONS

- 覆盖文件名固定为 `.kander-config.json`,位置在当前目录所属 Git 主工作树的根目录(不在 `.kander/` 内);非 Git 目录则从当前目录向上逐级查找第一个命中的文件。
- 合并规则:在校验前对原始 JSON 做对象级深合并——对象按键递归合并,标量和数组整体替换;合并结果再走现有 `Validate`。
- 覆盖文件中禁止出现 `schema_version` 与 `welcome_complete`;出现这两个键或任何未知键时直接报错(不像作用域配置那样静默丢弃)。
- 写入语义不变:`Save`/`Update`/`SaveIfUnchanged`、TUI 选项面板、`kander doctor` 修复、安装向导只写作用域 `config.json`,不修改项目覆盖文件。
- 优先级:`KANDER_CONFIG` 指定的文件仍作为作用域配置(由"覆盖是叠加层"推导);项目覆盖文件叠加在其上。最终优先级为 项目覆盖 > 作用域配置 > 默认值。
- 所有写入路径(`Save`/`Update`/`SaveIfUnchanged`、doctor 修复、TUI 保存、安装向导)以未合并的作用域配置为读-改-写基底,覆盖文件中的值不得反向写入作用域 `config.json`。
- 深合并前先对作用域配置的原始 JSON 调用前置卡导出的 `review_stages` 规范化函数,把旧平铺规范化为两档结构,避免合并出档名与角色同层的混合形态。

## EXPECTED_OUTCOME

- `internal/config` 新增覆盖文件定位与合并:`config.Load` 及其等价读取入口返回合并后的生效配置;`kander config --json` 输出合并结果;`kander config`(非 JSON)在存在覆盖文件时额外打印一行覆盖文件的绝对路径。
- 覆盖文件读取复用现有的 symlink/reparse 防护(`rejectLeafReparse`、`ensureWindowsPathNofollowSafe`、`readConfigBytes`),文件必须是常规文件。
- 覆盖文件不存在时行为与现状完全一致;覆盖文件存在但 JSON 非法、含禁止键或未知键时,读取配置的命令报错并指出文件路径与键名。
- TUI 选项面板加载的是作用域配置(不含覆盖),在检测到覆盖文件时于面板顶部显示一行提示:显示与编辑的是作用域配置,项目 `.kander-config.json` 中的键在运行时优先。
- 规则文档 `rules/KANDER-AGENTS.md` 的路径表新增"项目覆盖配置"一行并说明优先级;`rules/KANDER-KANBAN-RULES.md` 命令契约、`AGENTS.md`、`docs/custom-agents.md` 同步;i18n 三语文案补齐。

## ACCEPTANCE_CRITERIA

- [ ] 在 Git 主工作树根放置 `.kander-config.json` 后,从主工作树、子目录和任务 worktree 中运行 `kander config --json`,均输出合并后的值;删除该文件后输出回到作用域配置。有测试覆盖 Git 与非 Git(向上查找)两种定位方式。
- [ ] 深合并语义有单元测试:嵌套对象按键合并(如只覆盖 `review_stages.large.PM`),标量与数组整体替换,合并后仍通过 `Validate`。
- [ ] 覆盖文件含 `schema_version`、`welcome_complete` 或未知键时报错,错误信息含文件路径与键名;JSON 非法、非常规文件、symlink 时报错;每种情形有测试。
- [ ] `Save`/`Update`/`SaveIfUnchanged`、`kander doctor` 修复、TUI 选项面板保存只写作用域 `config.json`,覆盖文件字节不变;执行 doctor 修复与 TUI 保存后,作用域 `config.json` 中不含覆盖文件独有的值;两点均有测试。
- [ ] 作用域配置为旧平铺 `review_stages`、覆盖文件为两档 `review_stages` 时,合并结果通过 `Validate` 且覆盖档位生效;有测试。
- [ ] `kander config` 非 JSON 输出在有覆盖文件时多打印一行其绝对路径;TUI 选项面板在有覆盖文件时显示提示行;各有测试。
- [ ] `rules/KANDER-AGENTS.md` 路径表新增覆盖文件行并写明优先级,并明确写出"覆盖文件可指定 `agents` 自定义 Agent 的可执行路径与 argv 模板,信任层级等同于检出并运行该仓库代码"这一已接受风险;`AGENTS.md`、`docs/custom-agents.md`、`rules/KANDER-KANBAN-RULES.md` 同步;`rules/embed_test.go` 与 `internal/install` 规则摘要相关测试通过。
- [ ] en、zh-CN、ja 三个 i18n 文件对新增文案键齐全,`go test ./...` 与 `go vet ./...` 通过。

## THREAT_MODEL

- 资产:用户作用域配置的完整性;Agent/审核者/模型选择不被仓库中的文件静默篡改到未知值。
- 可信主体:当前用户,以及当前用户选择检出的项目仓库的维护者。
- 攻击者能力:向仓库提交一个 `.kander-config.json`。它只能改变已有配置键的取值(仍受 `Validate` 的取值域约束,如 Agent 名必须是已配置的 Agent),不能引入新的可执行路径以外的行为;`agents` 自定义 Agent 定义中的可执行路径与 argv 模板属于已有配置键,因此覆盖文件能够指定它们——这是接受的风险,与用户检出并运行仓库代码本身的信任层级一致,须在文档中明确写出。
- 不在范围:通过 symlink/reparse 指向仓库外文件读取——由现有防护拒绝。

## OUT_OF_SCOPE

- 现有问题:不改变 `KANDER_CONFIG` 与二进制位置决定作用域配置的现有逻辑;原因是覆盖是叠加层,不替换作用域解析。
- 加固:不为覆盖文件提供签名、白名单键集或交互式信任确认;原因是已在 THREAT_MODEL 中接受该风险并要求写入文档,进一步加固另立卡。
- 共享契约与文档:不提供 `kander config --set`、`--project` 等写入覆盖文件的命令;不改 `kanban/` 与 `KANBAN_DIR` 的定位;README 三语不改。
- 相邻功能:`review_stages` 的按档结构由前置卡 `20260909-review-stages-per-size-task` 负责,本卡只需其合并后能通过校验。

## DISCUSSION

```text
PREREQUISITES: 20260909-review-stages-per-size-task
```

- 任务组 `20260909-config-layering-group` 共 2 张卡,本卡依赖前置卡;原因见前置卡 DISCUSSION(两卡共同修改 `config.go`、`format.go`、`kander config` 输出与规则文档)。
- 现状:`config.ConfigPath()` 先看 `KANDER_CONFIG`,再由 `CurrentInstallPaths()` 按二进制位置决定;`Load` 直接 `loadValidated(path)`;`Effective` 只重新校验。`internal/board/locate.go` 已有按当前目录定位 Git 主工作树并向上查找的范式(`BoardRootAt`),定位覆盖文件可复用相同思路。
- 建议的最小实现:新增 `config.OverlayPath(cwd)` 与 `config.LoadEffective()`(或改造 `Load`),在 `decodeJSON` 后、`Validate` 前对两份 `map[string]any` 做深合并;`Save` 系列保持读写作用域文件。
- 本仓库 `AGENTS.md` 规定 CSA/Hacker 恒为 N/A,PM/QA 适用。
- CARD_REVIEW: 独立评审(新会话 Claude 子代理,只读两张卡与用户原始需求)指出问题并已修正:标注 `KANDER_CONFIG` 一条为推导;新增"写入以未合并作用域配置为基底、禁止反向污染"的决策与验收;把 THREAT_MODEL 的风险说明列为可判定的文档验收;为跨卡缺口新增"合并前规范化旧平铺 `review_stages`"的决策与验收。"非 Git 向上查找"与"禁止 `schema_version`/`welcome_complete`"两条在用户确认的方案原文中已有,保留为用户决策。评审结论:修正后通过。
- SELF_REVIEW: 已对照用户需求与确认方案复核。目标与产出一致;文件位置、深合并、禁止键报错、只写作用域配置四项决策均为用户确认的方案;补充了 THREAT_MODEL 说明覆盖文件能改 `agents` 可执行路径的已接受风险并要求写入文档;验收条目可判定,覆盖定位、合并、报错、写入隔离、输出、文档与 i18n。未发现需要新用户决策的歧义。

## IMPLEMENTATION

- 2026-09-09 首轮：交付 `5631b184af859cd726f07bfa119e5378a5e3ded3`。
- 2026-09-09 修复轮 `fix-batch-one-r1-c2`：fixed=5、deferred=5、rejected=1，交付 `f7b31bc529958ac268d2709ffd7fd99000c92bfb`。
- 2026-09-09 同步轮 `sync-batch-one-r1-c2`：rebase 到 `9a2badc1bdb23dd946020aaaa950501081dbbd5c`，交付 `5d632af67ee28a19769972298a11984ced5e76e6`。
- 2026-09-09 第二修复轮 `fix-batch-one-r2-c2` epoch 3：作用域语言写回隔离；`savePrefs` 注释与实现对齐。处置 fixed=2、deferred=3。最新交付 `777b1f088985e3d11cec3bd9b4dde9271822119e`。
- 2026-09-09 第三修复轮 `fix-batch-one-r3-c2` epoch 4：缺 language 键时选项面板不再把覆盖语言写回作用域。处置 fixed=1、deferred=4。最新交付 `4832ee9db7912db15d297a32ee8888fff757bf4a`。
- 2026-09-09 同步轮 `sync-batch-one-r4-c2` epoch 5：QA 第四轮延续项仅处置。处置 deferred=4。交付仍为 `4832ee9db7912db15d297a32ee8888fff757bf4a`。
- 2026-09-09 同步轮 `sync-batch-one-r5-c2` epoch 6：PM 第四轮延续项仅处置。处置 deferred=3。交付仍为 `4832ee9db7912db15d297a32ee8888fff757bf4a`。
- 2026-09-09 同步轮 `sync-batch-one-r6-c2` epoch 7：将第一修复轮 5 条 `fix_commit` 改为 rebase 后 SHA。交付仍为 `4832ee9db7912db15d297a32ee8888fff757bf4a`。
- 2026-09-09 wrap-up `wrapup-c2` epoch 8：`4832ee9` 已在 `origin/develop`；已清理本卡 worktree 与任务分支。报告 `report.md`，收尾记录 `wrap-up/wrapup-c2-8.md`。

## REVIEWS

- {"run_id":"qa-batch-one-r1","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/qa-batch-one-r1/report.md"}
- {"run_id":"pm-batch-one-r1","batch_id":"batch-one","role":"PM","execution_status":"failed","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/pm-batch-one-r1/report.md"}
- {"run_id":"pm-batch-one-r2","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5631b184af859cd726f07bfa119e5378a5e3ded3","report":"reviews/pm-batch-one-r2/report.md"}
- {"run_id":"pm-batch-one-r3","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"5d632af67ee28a19769972298a11984ced5e76e6","previous_run_id":"pm-batch-one-r2","report":"reviews/pm-batch-one-r3/report.md"}
- {"run_id":"qa-batch-one-r2","batch_id":"batch-one","role":"QA","execution_status":"failed","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"777b1f088985e3d11cec3bd9b4dde9271822119e","previous_run_id":"qa-batch-one-r1","report":"reviews/qa-batch-one-r2/report.md"}
- {"run_id":"qa-batch-one-r3","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"777b1f088985e3d11cec3bd9b4dde9271822119e","previous_run_id":"qa-batch-one-r1","report":"reviews/qa-batch-one-r3/report.md"}
- {"run_id":"qa-batch-one-r4","batch_id":"batch-one","role":"QA","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"4832ee9db7912db15d297a32ee8888fff757bf4a","previous_run_id":"qa-batch-one-r3","report":"reviews/qa-batch-one-r4/report.md"}
- {"run_id":"pm-batch-one-r4","batch_id":"batch-one","role":"PM","execution_status":"ok","base":"7466cd6acfd3077bdf311c0bb0ca163566d37ba6","commit":"4832ee9db7912db15d297a32ee8888fff757bf4a","previous_run_id":"pm-batch-one-r3","report":"reviews/pm-batch-one-r4/report.md"}
