KANDER_AUTOMATIC_CONTEXT_BYTES: 160115
PREVIOUS_RUN_ID: pm-batch-one-r3
Prior report (verbatim):
已完成核实。下面是审核报告。

---

# PM 审核报告(增量轮)

**Role**: PM(规格验收)
**Commit**: `5d632af67ee28a19769972298a11984ced5e76e6`(增量范围 `5631b184af859cd726f07bfa119e5378a5e3ded3..5d632af`,共 5 个修复提交)
**Task Context**: 任务组 `20260909-config-layering-group` batch-one 两张卡:`20260909-review-stages-per-size-task`、`20260909-project-config-overlay-task`
**Reviewed Scope**: 仅修复范围内改动文件及其可达影响面 —— `internal/config/{language.go,overlay.go,repair.go}`、`internal/tui/{prefs.go,options_panel.go,options_form.go}`、对应测试,以及为判定影响而只读追踪的 `internal/menu/options.go`(`Session.prepare`/`Save`)、`internal/config/loadsave.go`(`SaveIfUnchanged`)、`internal/config/review_stages.go`、`internal/tui/app.go`。未改动代码按上一轮结论接受,不重新审计。仅只读检查。

## 一、上一轮 findings 逐条核实

| ID | 处置记录 | 本轮核实结论 | 证据 |
|---|---|---|---|
| PM-01 | fixed (`fa8ab21`) | **已关闭** | `internal/tui/options_panel.go:537-546` `persistUI` 改为写 `prefsFromConfig(p.session.Config.TUI)`;会话 `Config.TUI` 来自 `options_panel.go:125` `LoadScope(true)` → `internal/menu/options.go:150` `cfg.TUI = s.existing.TUI`,即未合并的作用域值;`options_form.go:684-723` `applyInterface` 以作用域 TUI 为基底只写用户实际改动的字段;`prefs.go:81-91` `saveColumns` 改为只 patch `Columns`。`app.go:527-533` 不再用返回值回写显示字段,合并值显示不受影响。新增 `internal/tui/options_test.go:683-726`(覆盖文件含 `tui`)与 `:728-762`(`saveColumns`)锁住该路径。**但同一契约在本轮被 `language` 键重新违反,见新增的 PM-09** |
| PM-02 | fixed (`c1e3851`) | **已关闭** | `internal/config/overlay.go:205-214` `mergeOverlayRaw` 现对覆盖侧克隆后同样调用 `normalizeReviewStagesField`;`review_stages.go:74-101` 的规范化不回填缺失角色,因此平铺覆盖只覆盖它写到的角色,`deepMerge` 保留作用域其余角色。`internal/config/overlay_test.go:191-219` 断言两档作用域 + 平铺覆盖合并成功、`QA` 保持作用域值、作用域文件未被改写 |
| PM-03 | fixed (`baa7b35`) | **已关闭** | `internal/config/repair.go:100-105` 在 `asObject(raw)` 后 `cloneRawObjectDeep`,`fillMissingReviewStageScales`(`repair.go:149-171`)不再就地污染 `raw`,`repair.go:74` 的 `reflect.DeepEqual(raw, normalized)` 基准恢复。`internal/config/repair_test.go:174-229` 用 `Save` 写出的规范配置仅删 `small`,断言 `result.Changed` 为真且磁盘 `small.PM=required` |
| PM-04 | fixed (`9a2badc`) [mechanical] | **已关闭** | `internal/config/config_test.go:649-676` 中三段重复断言已删除,仅保留 `DefaultReviewStages` 形状、`nil`/`"auto"`/`[]any` 三类型错误与 `language:"fr"`;按档语义仍由 `review_stages_test.go:12-67`(含 `{"PM":"always"}` 与平铺映射)完整覆盖,无覆盖损失 |
| PM-05 | rejected | **接受该处置**。作者依据(写入值仍取自作用域对象、条目原文即标 `[out-of-contract]`)与我上一轮的定性一致,本轮无新事实,不再复述 |
| PM-06 | deferred | **仍存在**,`review_stages.go:36-46` `cloneRawObject` 与 `overlay.go:33-53` `cloneRawObjectDeep` 并存;本轮 `repair.go:104` 又新增一处 `cloneRawObjectDeep` 调用,与同一函数链上 `repair.go:158-166` 使用的 `cloneRawObject` 相邻,误用风险略增。仍为 suggest,见 NON-BLOCKING |
| PM-07 | deferred | **仍存在且被本轮加重**:`language.go:106` 使 `ConfiguredLanguage` 每次也调用 `readOverlay("")`,而 `internal/menu/commands.go:12-14` 在每条命令开头经 `BindEffectiveLanguage`(`language.go:159-166`)调用它,`format.go:146` 再调一次;单条 `kander config` 的覆盖定位次数由 2 次升到 4 次 `git worktree list --porcelain`。仍为 suggest,见 NON-BLOCKING |
| PM-08 | deferred | **仍存在**,`review_stages.go:49-63,86-99` 未改动。仍为 low,见 NON-BLOCKING |

另注(不构成条目):`options_panel.go:537-539` 新增的 `p.session == nil` 提前返回,使 `save()`(`options_panel.go:511-514`)的 `tui.environment_not_ready_interface_preferences_saved` 状态文案在该分支上不再成立;但会话构造失败时 `options_panel.go:167-171` 只置 `loadErr`、`form` 保持为 `nil`(`options_view.go:122`、`options_panel.go:261-286`),面板此时只接受 esc/q,`save()` 不可达,故不作为条目。

## 二、需求追踪表(仅列本轮状态或成因变化的行)

| # | 需求(原子) | 期望行为 | 代码证据 | 状态 |
|---|---|---|---|---|
| R7 | `repair.go` 缺档回填并有测试 | doctor 缺档回填必须落盘 | `repair.go:100-105,149-171`;`repair_test.go:174-229` | Partial → **Complete** |
| R14 | 所有写入路径以未合并作用域为读-改-写基底,覆盖值不得反向写入 | 写入隔离 | `tui/prefs.go:81-91`、`options_panel.go:542`、`options_form.go:684-723` 已隔离 `tui` 键;但 `menu/options.go:153-155` + `language.go:106-119` 使 `language` 反向写入 | **Partial**(成因由 PM-01 更替为 PM-09) |
| R20 | TUI 选项面板加载的是作用域配置(不含覆盖) | 面板会话基线=作用域 | `options_panel.go:125` `LoadScope(true)`;但会话 `Config.Language` 取自合并后的 `ConfiguredLanguage()`(`menu/options.go:154`) | **Partial**(并入 PM-09) |

**完成度统计**:Complete 18、Partial 2、Missing 0、Contradicted 0、Unverifiable 0(上一轮为 17/3/0/0/0)。

## findings

### PM-09 — high — `ConfiguredLanguage` 改为合并覆盖后,TUI 选项面板保存会把覆盖文件的 `language` 写进作用域 `config.json`

- **claim**: Observed
- **证据**:`internal/config/language.go:106-119`(本轮 `5d632af` 新增)让 `ConfiguredLanguage()` 返回合并覆盖后的语言;`internal/menu/options.go:153-155` 在 `Session.prepare` 中 `if configValid { if stored := config.ConfiguredLanguage(); stored != "" { cfg.Language = stored } }`,把该合并值放进会话配置 `s.Config`(`options.go:163`);`internal/menu/options.go:501-507` `Session.Save()` → `config.SaveIfUnchanged(s.Config, s.existing)`;`internal/config/loadsave.go:352-377` 该函数直接 `saveConfigAt(path, cfg)` 整份写出作用域文件,`language` 字段一并落盘。会话由 `internal/tui/options_panel.go:125,130` 以 `config.LoadScope(true)` + `menu.NewSession(existing, valid=true)` 构造,保存入口为 `options_panel.go:464`(`persistNow`,interface/execution/review/rules 任一分区提交)与 `options_panel.go:522`(`save`)。
- **违反的契约**:卡 2 USER_DECISIONS「所有写入路径(`Save`/`Update`/`SaveIfUnchanged`、doctor 修复、TUI 保存、安装向导)以未合并的作用域配置为读-改-写基底,覆盖文件中的值不得反向写入作用域 `config.json`」;ACCEPTANCE「执行 doctor 修复与 TUI 保存后,作用域 `config.json` 中不含覆盖文件独有的值」;EXPECTED_OUTCOME「TUI 选项面板加载的是作用域配置(不含覆盖)」;以及 `rules/KANDER-AGENTS.md:19`「Writes … only update the scope `config.json`. Overlay values are never written back.」
- **失败场景**:作用域 `config.json` 的 `language` 为 `en`,项目根提交 `.kander-config.json` = `{"language":"ja"}`(`language` 在 `internal/config/overlay.go:27` 允许键内)。用户在该项目内打开 TUI 选项面板改任一项并保存(或在任一分区按 Enter 提交),作用域 `config.json` 的 `language` 即被永久改成 `ja`;此后所有其他项目与全局界面都变成日文,删除覆盖文件也不恢复,且过程无任何提示。这与 PM-01 是同一条契约,只是键从 `tui` 换成 `language`,并且是本轮修复引入的。
- **为什么现有测试没发现**:本轮新增的 `internal/config/overlay_test.go:387-414` 只断言 `ConfiguredLanguage()` 自身不写盘;`internal/tui/options_test.go` 的面板用例经 `newTestSession`(`options_test.go:59-70`)走 `menu.NewSessionForTest`,而该构造器用 `internal/menu/options.go:572` `cfg.Language = existing.Language`,根本不经过 `ConfiguredLanguage()`,因此 `TestOptionsSaveLeavesOverlayIsolated` 覆盖不到这条路径。
- **最小修复**:把「显示用的语言」与「持久化的语言」分开——`Session.prepare` 仍用作用域自身的显式语言(新增一个不合并覆盖的 `configuredScopeLanguage()`,或直接取 `s.existing` 的显式值)赋给 `cfg.Language`,面板文案的绑定再单独用合并值(`config.BindEffectiveLanguage()`);并补一条「覆盖文件含 `language`、TUI 保存后作用域 `config.json` 的 `language` 不变」的测试。

### PM-10 — medium [mechanical] — `savePrefs` 的注释声称按字段差异写入,实际五个判断全是空操作,仍整节覆盖

- **claim**: Observed
- **证据**:`internal/tui/prefs.go:28-29` 的注释写「writes only the UI fields that differ from the unmerged scope config. Overlay-only TUI values must not be copied back into config.json」;但 `prefs.go:34-51` 的实现是 `next := cfg.TUI` 之后对五个字段一律执行 `if desired.X != next.X { next.X = desired.X }`,每个分支的结果都等价于无条件 `next.X = desired.X`,函数结束时 `next == desired`,`cfg.TUI = next` 与修复前的 `cfg.TUI = value` 完全等价。真正实现隔离的是调用方:`options_panel.go:542` 传入的已是作用域 TUI,`prefs.go:83-90` 的 `saveColumns` 干脆绕开了本函数;`savePrefs` 在非测试代码中只有 `options_panel.go:542` 一个调用点。
- **影响**:注释描述的语义与实现不符,并且这十三行条件判断没有任何可观察效果(等价于死代码)。后续维护者若据注释认为「传合并值也安全」,PM-01 那类覆盖值泄漏会以静默方式回归。
- **最小修复**:二选一 —— 要么删掉五个空判断、把注释改成「写入调用方给出的作用域 UI 偏好整节」;要么让函数真正做差异写入(需要额外传入基线,如 `savePrefs(prefs, baseline uiPrefs)`,仅写与基线不同的字段)。

## NON-BLOCKING

- **PM-11 — low(新增)** — `ConfiguredLanguage` 的两处提前返回让界面语言与 `kander config --json` 不一致。其一:覆盖文件非法(JSON 错误、禁止键、未知键、非常规文件)时 `internal/config/language.go:106-109` 返回 `""`,界面语言退回环境语言(`language.go:159-166` → `ResolveLanguage`),于是"覆盖文件有问题"这条错误本身以非用户配置的语言显示。其二:作用域 `config.json` 不存在时 `language.go:91-94` 提前返回 `""`,覆盖文件里的 `language` 对界面无效,而同一目录下 `config.Load(true)` 经 `loadsave.go:63-79,95-115` 以 `DefaultConfig` 为底合并覆盖,`kander config --json` 仍输出覆盖语言,`format.go:146-151` 打印的"语言"行却是环境语言。两种情形触发都少见(配置尚未创建 / 覆盖文件本就报错),后果仅为提示语言不一致。若要收紧:这两个分支改为在返回前仍尝试合并覆盖,或至少在作用域文件缺失时以默认配置为底做同样的合并。
- **PM-06 — suggest(carried,lineage `pm-batch-one-r2`/`PM-06`)** — 两个语义相近的原始 JSON 克隆实现仍并存:`internal/config/review_stages.go:36-46` `cloneRawObject`(只递归 object)与 `internal/config/overlay.go:33-53` `cloneRawObjectDeep`(object + array)。本轮 `repair.go:104` 新增一处 `cloneRawObjectDeep`,与同一调用链上 `repair.go:158-166` 的 `cloneRawObject` 相邻,误用其一的风险比上一轮更高。合并为单一深拷贝实现即可。
- **PM-07 — suggest(carried,lineage `pm-batch-one-r2`/`PM-07`)** — 覆盖文件定位仍每次新起 `git worktree list --porcelain` 子进程(`internal/config/paths.go:125`),且本轮被加重:`internal/config/language.go:106` 让 `ConfiguredLanguage` 也调用 `readOverlay("")`,而它在 `internal/menu/commands.go:12-14`(每条命令开头)与 `internal/config/format.go:146` 各被调用一次;单条 `kander config` 的定位次数由 2 次升至 4 次。可在进程内按 cwd 缓存一次定位结果。
- **PM-08 — low(carried,lineage `pm-batch-one-r2`/`PM-08`)** — `review_stages` 中未知键取值为 object 时,错误信息把它当作档名报出(`internal/config/review_stages.go:49-63,86-99`),例如 `{"Owner": {"PM":"auto"}}` 报「含未知档名: Owner; 只允许 large, small」。本轮未改动,处置为 deferred,现象仍在。

候选共 6 条,均已列出,未丢弃。

```kander-findings
{"FINDINGS":[{"id":"PM-09","tier":"high","text":"本轮 5d632af 让 ConfiguredLanguage 返回合并覆盖后的语言,于是 TUI 选项面板保存会把覆盖文件独有的 language 写进作用域 config.json,重新违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。链路:internal/config/language.go:106-119 合并覆盖 → internal/menu/options.go:153-155 在 Session.prepare 中 cfg.Language = config.ConfiguredLanguage() → options.go:163 s.Config = cfg → options.go:501-507 Session.Save() 调 config.SaveIfUnchanged(s.Config, s.existing) → internal/config/loadsave.go:352-377 直接 saveConfigAt 整份写出作用域文件。会话由 internal/tui/options_panel.go:125,130 以 LoadScope(true)+menu.NewSession 构造,保存入口为 options_panel.go:464(persistNow,任一分区提交)与 options_panel.go:522(save)。失败场景:作用域 language=en,项目 .kander-config.json={\"language\":\"ja\"}(language 在 internal/config/overlay.go:27 允许键内),用户在该项目内保存一次选项面板,作用域 config.json 的 language 即被永久改成 ja 并对所有其他项目生效,删除覆盖文件也不恢复。与 PM-01 是同一条契约,键由 tui 换成 language,且由本轮修复引入。最小修复:Session.prepare 用不合并覆盖的作用域显式语言赋给 cfg.Language(新增 configuredScopeLanguage(),或取 s.existing 的显式值),面板文案单独用 config.BindEffectiveLanguage() 绑定合并值;并补「覆盖含 language、TUI 保存后作用域 language 不变」的测试。","evidence":"internal/config/language.go:106-119(ConfiguredLanguage 合并覆盖,5d632af 新增);internal/menu/options.go:153-155,163(prepare 用该合并值设 cfg.Language 并存入 s.Config);internal/menu/options.go:501-507(Session.Save → SaveIfUnchanged);internal/config/loadsave.go:352-377(SaveIfUnchanged → saveConfigAt 整份写作用域文件);internal/tui/options_panel.go:125,130(LoadScope+NewSession)、:464、:522(两条保存入口);internal/config/overlay.go:27(language 在允许键内)。测试盲区:internal/config/overlay_test.go:387-414 只断言 ConfiguredLanguage 自身不写盘;internal/tui/options_test.go:59-70 的 newTestSession 走 menu.NewSessionForTest(internal/menu/options.go:572 cfg.Language = existing.Language),不经过 ConfiguredLanguage。"},{"id":"PM-10","tier":"medium","mechanical":"documentation","text":"savePrefs 的注释与实现不符,且新增的五个条件判断是空操作。internal/tui/prefs.go:28-29 声称「writes only the UI fields that differ from the unmerged scope config」,但 prefs.go:34-51 对 Columns/MinColumnWidth/Theme/Refresh/Single 一律执行 if desired.X != next.X { next.X = desired.X },每个分支都等价于无条件赋值,函数结束时 next == desired,cfg.TUI = next 与修复前的整节覆盖完全等价;真正实现覆盖隔离的是调用方(options_panel.go:542 传入作用域 TUI,prefs.go:83-90 的 saveColumns 绕开本函数)。注释描述的保护并不存在,后续维护者据此认为「传合并值也安全」时,PM-01 那类泄漏会静默回归。最小修复:要么删掉五个空判断并把注释改为「写入调用方给出的作用域 UI 偏好整节」,要么改为真正的差异写入(额外传入基线,如 savePrefs(prefs, baseline uiPrefs),只写与基线不同的字段)。","evidence":"internal/tui/prefs.go:28-29(注释);internal/tui/prefs.go:31-51(desired := prefsConfig(prefs) 后五个 if 均为空操作,cfg.TUI = next == desired);internal/tui/options_panel.go:542(唯一非测试调用点,传入 prefsFromConfig(p.session.Config.TUI));internal/tui/prefs.go:83-90(saveColumns 改为直接 config.Update,不再经 savePrefs)。"}],"NON_BLOCKING":[{"id":"PM-11","tier":"low","text":"ConfiguredLanguage 的两处提前返回让界面语言与 kander config --json 不一致。其一:覆盖文件非法(JSON 错误、禁止键、未知键、非常规文件)时 internal/config/language.go:106-109 返回 \"\",界面语言退回环境语言,于是「覆盖文件有问题」这条错误本身以非用户配置的语言显示。其二:作用域 config.json 不存在时 language.go:91-94 提前返回 \"\",覆盖文件的 language 对界面无效,而 config.Load(true) 以 DefaultConfig 为底合并覆盖,kander config --json 仍输出覆盖语言、同一条命令的「语言」行却是环境语言。两种触发都少见,后果仅为提示语言不一致。若要收紧:这两个分支在返回前仍尝试合并覆盖,或至少在作用域文件缺失时以默认配置为底做同样的合并。","evidence":"internal/config/language.go:91-94(作用域文件缺失即返回)、:106-109(覆盖读取出错即返回)、:159-166(BindEffectiveLanguage 据此退回 ResolveLanguage);internal/config/loadsave.go:63-79,95-115(Load 在作用域缺失时以 DefaultConfig 为底合并覆盖);internal/config/format.go:146-151(kander config 的语言行取 ConfiguredLanguage)。"},{"id":"PM-06","tier":"suggest","lineage":{"run_id":"pm-batch-one-r2","finding_id":"PM-06"},"text":"两个语义相近的原始 JSON 克隆实现仍并存,差别只在是否复制数组;本轮 repair.go:104 新增一处 cloneRawObjectDeep 调用,与同一调用链上 repair.go:158-166 使用的 cloneRawObject 相邻,误用其一的风险比上一轮更高。合并为单一深拷贝实现即可。","evidence":"internal/config/review_stages.go:36-46(cloneRawObject,只递归 object);internal/config/overlay.go:33-53(cloneRawValue/cloneRawObjectDeep,object + array);internal/config/repair.go:104(本轮新增的 cloneRawObjectDeep)与 internal/config/repair.go:158-166(fillMissingReviewStageScales 用 cloneRawObject)。"},{"id":"PM-07","tier":"suggest","lineage":{"run_id":"pm-batch-one-r2","finding_id":"PM-07"},"text":"覆盖文件定位仍每次新起 git worktree list --porcelain 子进程,且本轮被加重:ConfiguredLanguage 现在也调用 readOverlay(\"\"),而它在每条命令开头(BindEffectiveLanguage)与 kander config 的摘要处各被调用一次,单条 kander config 的定位次数由 2 次升至 4 次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。","evidence":"internal/config/language.go:106(本轮新增的 readOverlay 调用);internal/menu/commands.go:12-14(每条命令开头 prepareLanguage → BindEffectiveLanguage → ConfiguredLanguage);internal/config/format.go:146(FormatConfigLines 再次调用)、format.go:112(同一函数内另有 OverlayPath(\"\"));internal/config/loadsave.go:104-106(Load 的覆盖读取);internal/config/paths.go:125(exec.Command(\"git\", …))。"},{"id":"PM-08","tier":"low","lineage":{"run_id":"pm-batch-one-r2","finding_id":"PM-08"},"text":"review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\":{\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。本轮未改动该代码,处置为 deferred,现象仍在。如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。","evidence":"internal/config/review_stages.go:49-63(classifyReviewStages);internal/config/review_stages.go:86-99(reviewStagesLooksFlat 为假时统一走 config.review_stages_has_unknown_scales)。"}]}
```

任务文件清理:本次审核受"只读检查、不得创建/修改/删除任何文件"的约束,故未删除 `/tmp/claude-review.c9b7d5d3c1d6e4204389c315098e8f06/prompt.txt`;文件遗留不影响结果。

---

**结论摘要**:上一轮 4 条门禁项(PM-01~PM-04)在 `5d632af` 上均已真实关闭,证据逐条可复核;PM-05 的 rejected 处置予以接受,PM-06/07/08 的 deferred 现象仍在。但修复范围本身引入两条新问题:`ConfiguredLanguage` 合并覆盖后,TUI 选项面板保存会把覆盖文件的 `language` 写进作用域 `config.json`(PM-09,high,与 PM-01 同一条契约),以及 `savePrefs` 的注释与空操作判断不符(PM-10,medium [mechanical])。因此本轮不通过。
Author records (verbatim JSON):
{
  "assignment": {
    "run_id": "pm-batch-one-r3",
    "batch_id": "batch-one",
    "author": "claude-orchestrator",
    "basis": "assigned by each card's GOAL/OUT_OF_SCOPE and the fix commits that introduced them: PM-09 (language write-back via ConfiguredLanguage, introduced by card 2 commit 5d632af), PM-10 (savePrefs comment vs implementation, card 2 commit fa8ab21), PM-11 (ConfiguredLanguage early returns, card 2) and carried PM-06/PM-07 hit card 2; carried PM-08 hits card 1's review_stages error text",
    "items": {
      "PM-06": [
        "20260909-project-config-overlay-task"
      ],
      "PM-07": [
        "20260909-project-config-overlay-task"
      ],
      "PM-08": [
        "20260909-review-stages-per-size-task"
      ],
      "PM-09": [
        "20260909-project-config-overlay-task"
      ],
      "PM-10": [
        "20260909-project-config-overlay-task"
      ],
      "PM-11": [
        "20260909-project-config-overlay-task"
      ]
    },
    "owners": {
      "20260909-project-config-overlay-task": "cursor",
      "20260909-review-stages-per-size-task": "cursor"
    },
    "recorded_at": "2026-09-08T19:38:55.552411666Z"
  },
  "records": [
    {
      "authorization": {
        "dispatch_id": "sync-batch-one-r2-c1",
        "epoch": 2
      },
      "submitted_revision": 30,
      "record_id": "pm-08-deferred-r3",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-08",
      "batch_id": "batch-one",
      "task_id": "20260909-review-stages-per-size-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:42:08.529885951Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\":{\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。本轮未改动该代码,处置为 deferred,现象仍在。如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。",
      "status": "deferred",
      "basis": "已在未改交付 9a2badc1bdb23dd946020aaaa950501081dbbd5c 上独立复现: Validate(review_stages={\"Owner\":{\"PM\":\"auto\"}}) 报「review_stages 含未知档名: Owner; 只允许 large, small」。classifyReviewStages 将 Owner 列入 unknown;reviewStagesLooksFlat 因取值为 object 为假,NormalizeReviewStages 走 config.review_stages_has_unknown_scales。与上一轮 reviews/pm-batch-one-r2/dispositions/pm-08-deferred-r1.json 同一现象,本 sync 轮不改代码,维持 deferred。"
    },
    {
      "authorization": {
        "dispatch_id": "fix-batch-one-r2-c2",
        "epoch": 3
      },
      "submitted_revision": 44,
      "record_id": "pm-09-fix-r2",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-09",
      "batch_id": "batch-one",
      "task_id": "20260909-project-config-overlay-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:45:12.165413Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "本轮 5d632af 让 ConfiguredLanguage 返回合并覆盖后的语言,于是 TUI 选项面板保存会把覆盖文件独有的 language 写进作用域 config.json,重新违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。链路:internal/config/language.go:106-119 合并覆盖 → internal/menu/options.go:153-155 在 Session.prepare 中 cfg.Language = config.ConfiguredLanguage() → options.go:163 s.Config = cfg → options.go:501-507 Session.Save() 调 config.SaveIfUnchanged(s.Config, s.existing) → internal/config/loadsave.go:352-377 直接 saveConfigAt 整份写出作用域文件。会话由 internal/tui/options_panel.go:125,130 以 LoadScope(true)+menu.NewSession 构造,保存入口为 options_panel.go:464(persistNow,任一分区提交)与 options_panel.go:522(save)。失败场景:作用域 language=en,项目 .kander-config.json={\"language\":\"ja\"}(language 在 internal/config/overlay.go:27 允许键内),用户在该项目内保存一次选项面板,作用域 config.json 的 language 即被永久改成 ja 并对所有其他项目生效,删除覆盖文件也不恢复。与 PM-01 是同一条契约,键由 tui 换成 language,且由本轮修复引入。最小修复:Session.prepare 用不合并覆盖的作用域显式语言赋给 cfg.Language(新增 configuredScopeLanguage(),或取 s.existing 的显式值),面板文案单独用 config.BindEffectiveLanguage() 绑定合并值;并补「覆盖含 language、TUI 保存后作用域 language 不变」的测试。",
      "status": "fixed",
      "basis": "独立核实目标 5d632af67ee28a19769972298a11984ced5e76e6：ConfiguredLanguage 合并覆盖后，menu.Session.prepare 用其赋值 cfg.Language，SaveIfUnchanged 整份写作用域。已新增 ConfiguredScopeLanguage() 只读未合并作用域；prepare 用该值填会话，面板文案改 BindEffectiveLanguage()。",
      "fix_commit": "32ba6ee3bb197acc504d6208fe521cb1d68d0bfc",
      "verification": "go test -count=1 ./internal/config -run 'TestConfiguredLanguage|TestConfiguredLanguageUsesOverlay' 与 go test -count=1 ./internal/menu -run TestSessionSaveLeavesOverlayLanguageIsolated 在 32ba6ee3bb197acc504d6208fe521cb1d68d0bfc 通过；最终交付 777b1f088985e3d11cec3bd9b4dde9271822119e 上 go test -count=1 ./... 1267 passed。"
    },
    {
      "authorization": {
        "dispatch_id": "fix-batch-one-r2-c2",
        "epoch": 3
      },
      "submitted_revision": 45,
      "record_id": "pm-10-fix-r2",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-10",
      "batch_id": "batch-one",
      "task_id": "20260909-project-config-overlay-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:45:12.538559895Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "savePrefs 的注释与实现不符,且新增的五个条件判断是空操作。internal/tui/prefs.go:28-29 声称「writes only the UI fields that differ from the unmerged scope config」,但 prefs.go:34-51 对 Columns/MinColumnWidth/Theme/Refresh/Single 一律执行 if desired.X != next.X { next.X = desired.X },每个分支都等价于无条件赋值,函数结束时 next == desired,cfg.TUI = next 与修复前的整节覆盖完全等价;真正实现覆盖隔离的是调用方(options_panel.go:542 传入作用域 TUI,prefs.go:83-90 的 saveColumns 绕开本函数)。注释描述的保护并不存在,后续维护者据此认为「传合并值也安全」时,PM-01 那类泄漏会静默回归。最小修复:要么删掉五个空判断并把注释改为「写入调用方给出的作用域 UI 偏好整节」,要么改为真正的差异写入(额外传入基线,如 savePrefs(prefs, baseline uiPrefs),只写与基线不同的字段)。",
      "status": "fixed",
      "basis": "独立核实 5d632af 上 prefs.go:28-51：注释声称按差异写入，五个 if desired.X != next.X 均为空操作，结束时 next==desired。已删除空判断，注释改为整节写入调用方快照、隔离由调用方保证。",
      "fix_commit": "777b1f088985e3d11cec3bd9b4dde9271822119e",
      "mechanical": "documentation",
      "verification": "逐句比对 777b1f088985e3d11cec3bd9b4dde9271822119e 的 internal/tui/prefs.go：1) 旧注释「writes only the UI fields that differ from the unmerged scope config」已删除；2) 五个 if desired.Columns/MinColumnWidth/Theme/Refresh/Single 空操作已删除；3) 新注释写明「writes the caller-provided UI preferences into the scope config.json as a whole TUI section」且「does not filter overlay-only fields」；4) 实现现为 cfg.TUI = prefsConfig(prefs) 整节赋值，与注释一致；5) persistUI 仍传入 prefsFromConfig(session.Config.TUI)，saveColumns 仍只 patch Columns。go test -count=1 ./internal/tui -run 'TestColumnCountPrefs|TestOptionsSaveLeavesOverlayIsolated|TestThemeChangeKeepsInterfaceState' 通过；最终提交 go test -count=1 ./... 1267 passed。"
    },
    {
      "authorization": {
        "dispatch_id": "fix-batch-one-r2-c2",
        "epoch": 3
      },
      "submitted_revision": 46,
      "record_id": "pm-11-deferred-r2",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-11",
      "batch_id": "batch-one",
      "task_id": "20260909-project-config-overlay-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:45:12.909046585Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "ConfiguredLanguage 的两处提前返回让界面语言与 kander config --json 不一致。其一:覆盖文件非法(JSON 错误、禁止键、未知键、非常规文件)时 internal/config/language.go:106-109 返回 \"\",界面语言退回环境语言,于是「覆盖文件有问题」这条错误本身以非用户配置的语言显示。其二:作用域 config.json 不存在时 language.go:91-94 提前返回 \"\",覆盖文件的 language 对界面无效,而 config.Load(true) 以 DefaultConfig 为底合并覆盖,kander config --json 仍输出覆盖语言、同一条命令的「语言」行却是环境语言。两种触发都少见,后果仅为提示语言不一致。若要收紧:这两个分支在返回前仍尝试合并覆盖,或至少在作用域文件缺失时以默认配置为底做同样的合并。",
      "status": "deferred",
      "basis": "独立核实 5d632af 与本轮 language.go：configuredScopeObject 在作用域文件缺失或 Validate 失败时返回 nil，ConfiguredLanguage 随之返回 \"\"；readOverlay 出错仍提前返回，不回退到作用域语言。与 Load(true) 缺文件仍合并覆盖不一致。属低等级提示语言不一致，本轮 disposition-only 不改。"
    },
    {
      "authorization": {
        "dispatch_id": "fix-batch-one-r2-c2",
        "epoch": 3
      },
      "submitted_revision": 47,
      "record_id": "pm-06-deferred-r2",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-06",
      "batch_id": "batch-one",
      "task_id": "20260909-project-config-overlay-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:45:13.059949569Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "两个语义相近的原始 JSON 克隆实现仍并存,差别只在是否复制数组;本轮 repair.go:104 新增一处 cloneRawObjectDeep 调用,与同一调用链上 repair.go:158-166 使用的 cloneRawObject 相邻,误用其一的风险比上一轮更高。合并为单一深拷贝实现即可。",
      "status": "deferred",
      "basis": "延续 pm-batch-one-r2/PM-06（记录 reviews/pm-batch-one-r2/dispositions/pm-06-deferred-r1.json）。独立核实 5d632af：cloneRawObject（review_stages.go:36-46）与 cloneRawObjectDeep（overlay.go:33-54）仍并存；repair.go:104 用后者、fillMissingReviewStageScales 用前者。建议项，本轮不开修复。"
    },
    {
      "authorization": {
        "dispatch_id": "fix-batch-one-r2-c2",
        "epoch": 3
      },
      "submitted_revision": 48,
      "record_id": "pm-07-deferred-r2",
      "run_id": "pm-batch-one-r3",
      "finding_id": "PM-07",
      "batch_id": "batch-one",
      "task_id": "20260909-project-config-overlay-task",
      "author": "cursor",
      "recorded_at": "2026-09-08T19:45:13.187112313Z",
      "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
      "original": "覆盖文件定位仍每次新起 git worktree list --porcelain 子进程,且本轮被加重:ConfiguredLanguage 现在也调用 readOverlay(\"\"),而它在每条命令开头(BindEffectiveLanguage)与 kander config 的摘要处各被调用一次,单条 kander config 的定位次数由 2 次升至 4 次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。",
      "status": "deferred",
      "basis": "延续 pm-batch-one-r2/PM-07（记录 reviews/pm-batch-one-r2/dispositions/pm-07-deferred-r1.json）。独立核实：ConfiguredLanguage 仍调用 readOverlay(\"\")，BindEffectiveLanguage 与 FormatConfigLines/Load/OverlayPath 叠加，单条 kander config 仍多次 git worktree list。性能建议，本轮不开修复。"
    }
  ]
}

Current batch disposition (tool generated):
{
  "schema": 1,
  "batch": {
    "plan_id": "config-layering-cycle",
    "task_context_hash": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493",
    "schema": 1,
    "batch_id": "batch-one",
    "task_ids": [
      "20260909-project-config-overlay-task",
      "20260909-review-stages-per-size-task"
    ],
    "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
    "target_commit": "4832ee9db7912db15d297a32ee8888fff757bf4a",
    "report_language": "zh-CN",
    "requirements": {
      "CSA": "N/A: repository AGENTS.md marks the second-stage security roles CSA and Hacker as always N/A in this repository",
      "Hacker": "N/A: repository AGENTS.md marks the second-stage security roles CSA and Hacker as always N/A in this repository",
      "PM": "required",
      "QA": "required"
    },
    "advances": [
      {
        "previous_target": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "target": "9a2badc1bdb23dd946020aaaa950501081dbbd5c",
        "reason": "fix delivery of card 20260909-review-stages-per-size-task for batch-one (QA-004/PM-03 fixed, PM-04 mechanical redundant-test fixed), received onto the group branch",
        "deliveries": {
          "9a2badc1bdb23dd946020aaaa950501081dbbd5c": "20260909-review-stages-per-size-task",
          "baa7b350eb3bf2f004864875e9b81d1d3e6c6eb4": "20260909-review-stages-per-size-task"
        }
      },
      {
        "previous_target": "9a2badc1bdb23dd946020aaaa950501081dbbd5c",
        "target": "5d632af67ee28a19769972298a11984ced5e76e6",
        "reason": "fix delivery of card 20260909-project-config-overlay-task for batch-one (QA-001/PM-02, QA-002/PM-01, QA-003 fixed), rebased onto the group head via sync round and received onto the group branch",
        "deliveries": {
          "5d632af67ee28a19769972298a11984ced5e76e6": "20260909-project-config-overlay-task",
          "c1e3851fb4008854d2f58c418411caef79216665": "20260909-project-config-overlay-task",
          "fa8ab21a402b522e5820c631b0e97712f8ceb309": "20260909-project-config-overlay-task"
        }
      },
      {
        "previous_target": "5d632af67ee28a19769972298a11984ced5e76e6",
        "target": "777b1f088985e3d11cec3bd9b4dde9271822119e",
        "reason": "second fix delivery of card 20260909-project-config-overlay-task for batch-one (PM-09 fixed, PM-10 mechanical documentation fixed), received onto the group branch",
        "deliveries": {
          "32ba6ee3bb197acc504d6208fe521cb1d68d0bfc": "20260909-project-config-overlay-task",
          "777b1f088985e3d11cec3bd9b4dde9271822119e": "20260909-project-config-overlay-task"
        }
      },
      {
        "previous_target": "777b1f088985e3d11cec3bd9b4dde9271822119e",
        "target": "4832ee9db7912db15d297a32ee8888fff757bf4a",
        "reason": "third fix delivery of card 20260909-project-config-overlay-task for batch-one (QA-005 fixed), received onto the group branch",
        "deliveries": {
          "4832ee9db7912db15d297a32ee8888fff757bf4a": "20260909-project-config-overlay-task"
        }
      }
    ],
    "revision": 5
  },
  "runs": [
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "pm-batch-one-r1",
        "batch_id": "batch-one",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "PM",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "363d52e6e53385f7ad1ccfb81b60a3d4838d0d7eb1b1a0c3fe2c99087bea7493",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "failed",
        "semantic_status": "unassessed",
        "failure_reason": "invalid structured review report: 审核证据无效：structured findings required; legacy mapping required for old reports",
        "exit_code": 1,
        "created_at": "2026-09-08T18:44:12.029321491Z",
        "finished_at": "2026-09-08T18:56:30.607012361Z",
        "duration_ms": 738577,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "100b658a6d867f9fe3e41e4d9b8755dfab44cb6b8216911974b3f3f445e8c528",
          "output.raw": "3a85fd73a2b495da489a0110fe341149c897794a851de515f239df886a690495",
          "prompt.txt": "f4021e05087a2a8feb4a19c0a3da849de40db55b5c70bed107008511533af8d3",
          "report.md": "f84d2c12f593825251a323a1edd7e4234fb4fbad21acf2eb95c264e66438d024",
          "review-context.md": "363d52e6e53385f7ad1ccfb81b60a3d4838d0d7eb1b1a0c3fe2c99087bea7493",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "records": []
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "pm-batch-one-r2",
        "batch_id": "batch-one",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "PM",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "f96fa75e7199206d60f60b1913cf113efca19f85d67f1f5f4e976c234a3c1792",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T18:57:15.125380194Z",
        "finished_at": "2026-09-08T19:09:08.856153967Z",
        "duration_ms": 713730,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "100b658a6d867f9fe3e41e4d9b8755dfab44cb6b8216911974b3f3f445e8c528",
          "output.raw": "ff02dae84e2579a65652ac95f24a90b74819bfa6816ef913130854b7db5022a7",
          "prompt.txt": "b702423fd6b175f96624368ab370e1273d4e8d9011388a678ea8eb84fbe80750",
          "report.md": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "review-context.md": "f96fa75e7199206d60f60b1913cf113efca19f85d67f1f5f4e976c234a3c1792",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-01",
            "tier": "high",
            "text": "TUI 偏好写回把项目覆盖的 `tui` 值写进作用域 config.json,违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与本次新增的 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。internal/tui/prefs.go:20-26 的 loadPrefs 读的是合并后配置 config.Load(true),App 的 Theme/Columns/MinColumnWidth/RefreshSecs/Single 由 internal/tui/cmd.go:69 从该合并值初始化;internal/tui/options_panel.go:537-546 的 persistUI 用这些 App 字段组装 uiPrefs,internal/tui/prefs.go:29-36 的 savePrefs 在 config.Update 回调里无条件 cfg.TUI = value 并落盘作用域文件。persistUI 由 options_panel.go:457-460(persistNow,任意分区提交)、options_panel.go:511(save)、options_form.go:711-714(applyInterface)以及 app.go:150 → prefs.go:63-67(saveColumns)四条路径触发。最小修复:savePrefs 以 config.Update 回调里收到的作用域 cfg.TUI 为基底,只写入本次实际变更的字段(显示仍用合并值),并在 TestOptionsSaveLeavesOverlayIsolated 的覆盖文件中加入 tui 键。",
            "evidence": "internal/tui/prefs.go:20-26(loadPrefs → config.Load(true));internal/tui/prefs.go:29-36(savePrefs → config.Update 中 cfg.TUI = value);internal/tui/cmd.go:69,104(App 初值来自合并 prefs);internal/tui/options_panel.go:537-546(persistUI 取 p.app.* );internal/tui/options_panel.go:452-460,511(persistNow/save 在任意分区提交时调用 persistUI);internal/tui/options_form.go:711-714;internal/tui/app.go:150。失败场景:项目 .kander-config.json = {\"tui\":{\"theme\":\"dark\",\"columns\":5}}(tui 在 internal/config/overlay.go:22-33 允许键内),用户在该项目内改一次列数或在选项面板按 Enter 保存,dark/5 即被写入作用域 config.json 并对其他项目生效。现有测试 internal/tui/options_test.go:683-716 的覆盖文件只含 kanban_agent,未覆盖 tui 键。"
          },
          {
            "id": "PM-02",
            "tier": "medium",
            "text": "覆盖文件里使用文档声明仍然合法的旧平铺 review_stages 时,合并结果变成档名与角色同层的混合形态,该项目下所有读配置的命令直接报错。internal/config/overlay.go:205-210 的 mergeOverlayRaw 只对作用域原始 JSON 调用 NormalizeReviewStages,覆盖侧原样进入 deepMerge(overlay.go:44-58 对同键 object 递归合并),而 internal/config/review_stages.go:79-85 对混合形态直接报错。作用域 config.json 一经 kander 写出必为两档(review_stages_test.go:174-211),因此这是常见组合。最小修复:mergeOverlayRaw 在 deepMerge 前对 overlay[\"review_stages\"] 同样调用 NormalizeReviewStages,并补一条「覆盖平铺 + 作用域两档」的合并测试。",
            "evidence": "internal/config/overlay.go:205-210(仅规范化 scope);internal/config/overlay.go:44-58(deepMerge 递归 object);internal/config/review_stages.go:79-85(mixed 报错);文档声明旧平铺合法:docs/custom-agents.md:84、AGENTS.md:51、rules/KANDER-REVIEW-RULES.md:330。失败场景:作用域为两档 + .kander-config.json = {\"review_stages\":{\"PM\":\"required\"}} → 合并得 {large,small,PM} → 报「review_stages 同时含档名与角色: PM, large」,kander config/start/review/TUI 全部失败。internal/config/overlay_test.go:154-182 只测了反方向组合。"
          },
          {
            "id": "PM-03",
            "tier": "medium",
            "text": "doctor 的 review_stages 缺档回填在「其余字段已规范」的配置上不落盘,并报告为「无变化」。internal/config/repair.go:141 的 fillMissingReviewStageScales(provided) 中,provided 来自 repair.go:100 的 asObject(raw),而 internal/config/config.go:638-641 的 asObject 只是类型断言、返回同一个 map,于是 repair.go:166 的 provided[\"review_stages\"] = normalized 就地改写了 raw;repair.go:74 随后用 reflect.DeepEqual(raw, normalized) 决定是否备份并写盘,基准已被污染。对一份规范 config.json(该等值判断在规范文件上必为真,见 repair_test.go:43-46)手工删掉 review_stages.small 后运行 doctor,会跳过写盘、result.Changed=false 并打印「config.json is unchanged」,磁盘上仍缺该档,下次 Load 时 small 全部退化为 auto,与 doctor 当场返回的 cfg 不一致,重复运行也修不好。最小修复:fillMissingReviewStageScales 先深拷贝再规范化(或只把结果写进 root),并补一条「除缺一档外其余均规范」的 doctor 测试断言 result.Changed 为真。",
            "evidence": "internal/config/repair.go:141,149-167(就地改写 provided);internal/config/config.go:638-641(asObject 返回同一 map);internal/config/repair.go:74(reflect.DeepEqual(raw, normalized) 决定是否写盘);internal/config/repair_test.go:43-46(规范文件上二次 repair 不重写,证明该判断在规范文件上为真);internal/config/repair_test.go:125-150(现有缺档测试用的是缺大量键的最小配置,DeepEqual 必为假,故漏检);internal/menu/doctor_repair.go:31-33(据 result.Changed 打印「unchanged」)。"
          },
          {
            "id": "PM-04",
            "tier": "medium",
            "text": "新增的 internal/config/review_stages_test.go 与保留在 config_test.go 的 TestReviewStagesDefaultsAndValidation 重复覆盖同一行为:缺 review_stages 节 → 两档全 auto、{\"PM\":\"always\"} 必须报错、平铺映射到两档,三处断言在两个文件中各有一份(其中「invalid mode」用例输入完全相同)。同一行为在两处维护会造成改一处另一处仍绿的假信心。最小修复:删除 config_test.go:665-687 的这三段,仅保留该测试独有的 DefaultReviewStages 形状检查、nil/\"auto\"/[]any 类型错误与 language 用例,按档语义断言统一留在 review_stages_test.go。",
            "evidence": "internal/config/config_test.go:665-675 ↔ internal/config/review_stages_test.go:69-82(missingSection);internal/config/config_test.go:684-687 ↔ internal/config/review_stages_test.go:54({\"PM\":\"always\"} 同一输入);internal/config/config_test.go:676-683 ↔ internal/config/review_stages_test.go:29-42(平铺作用于两档)。",
            "mechanical": "redundant-test"
          }
        ],
        "NON_BLOCKING": [
          {
            "id": "PM-05",
            "tier": "suggest",
            "text": "[out-of-contract] kander doctor 用合并后的 agent 定义判定可用性,再据此改写并落盘作用域 config.json 的 kanban_agent/kanban_agents/reviewers。项目覆盖把 agents.\u003cname\u003e.path 指到不存在的路径时,用户在该项目里跑一次 doctor 就会永久改掉全局的执行 Agent/审核者选择。写入值本身仍取自作用域配置,未违反「覆盖值不得反向写入」的字面契约,故不作为门禁项;若要收紧,修复阶段的可用性判定改用 config.LoadScope 的 agent 定义。",
            "evidence": "internal/menu/doctor.go:67(config.Load(true) 合并配置)→ doctor.go:71 findAgents(agentConfig) → doctor.go:73 repairDoctorConfig(agents, tools);internal/menu/doctor_repair.go:58-77(依据 agentUsable(agents[…]) 改写 kanban_agent/kanban_agents/reviewers,经 config.Repair 落盘作用域文件)。"
          },
          {
            "id": "PM-06",
            "tier": "suggest",
            "text": "存在两个语义相近的原始 JSON 克隆实现,差别只在是否复制数组,后续改动容易误用其中之一。可合并为单一深拷贝实现。",
            "evidence": "internal/config/review_stages.go:36-46(cloneRawObject,只递归 object);internal/config/overlay.go:24-51(cloneRawValue / cloneRawObjectDeep,object + array);二者分别被 repair.go:162-164 与 loadsave.go:79,93 使用。"
          },
          {
            "id": "PM-07",
            "tier": "suggest",
            "text": "每次 config.Load 都会新起一个 `git worktree list --porcelain` 子进程用于定位覆盖文件,kander config 因摘要处再次调用 OverlayPath 而在单条命令内跑两次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。",
            "evidence": "internal/config/loadsave.go:105 → internal/config/overlay.go:174(readOverlay)→ overlay.go:163(OverlayPath)→ internal/config/paths.go:125(exec.Command(\"git\", …));internal/config/format.go:112 再次调用 OverlayPath(\"\")。"
          },
          {
            "id": "PM-08",
            "tier": "low",
            "text": "review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\": {\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。触发少见、后果仅为提示不精确;如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。",
            "evidence": "internal/config/review_stages.go:49-63(classifyReviewStages)、review_stages.go:86-99(reviewStagesLooksFlat 为假时统一走 config.review_stages_has_unknown_scales)。"
          }
        ]
      },
      "assignment": {
        "run_id": "pm-batch-one-r2",
        "batch_id": "batch-one",
        "author": "claude-orchestrator",
        "basis": "assigned by each card's GOAL/OUT_OF_SCOPE and actual changed files: PM-03 (doctor backfill), PM-04 (review_stages tests) and PM-08 (review_stages error text) hit card 1; PM-01 (TUI prefs write-back), PM-02 (overlay merge normalization), PM-05 (doctor agent availability on merged config), PM-06 (second raw clone helper introduced with overlay), PM-07 (OverlayPath git subprocess per Load) hit card 2",
        "items": {
          "PM-01": [
            "20260909-project-config-overlay-task"
          ],
          "PM-02": [
            "20260909-project-config-overlay-task"
          ],
          "PM-03": [
            "20260909-review-stages-per-size-task"
          ],
          "PM-04": [
            "20260909-review-stages-per-size-task"
          ],
          "PM-05": [
            "20260909-project-config-overlay-task"
          ],
          "PM-06": [
            "20260909-project-config-overlay-task"
          ],
          "PM-07": [
            "20260909-project-config-overlay-task"
          ],
          "PM-08": [
            "20260909-review-stages-per-size-task"
          ]
        },
        "owners": {
          "20260909-project-config-overlay-task": "cursor",
          "20260909-review-stages-per-size-task": "cursor"
        },
        "recorded_at": "2026-09-08T19:09:46.93281461Z"
      },
      "records": [
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c1",
            "epoch": 1
          },
          "submitted_revision": 19,
          "record_id": "pm-03-fix-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-03",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:19:38.615777101Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "doctor 的 review_stages 缺档回填在「其余字段已规范」的配置上不落盘,并报告为「无变化」。internal/config/repair.go:141 的 fillMissingReviewStageScales(provided) 中,provided 来自 repair.go:100 的 asObject(raw),而 internal/config/config.go:638-641 的 asObject 只是类型断言、返回同一个 map,于是 repair.go:166 的 provided[\"review_stages\"] = normalized 就地改写了 raw;repair.go:74 随后用 reflect.DeepEqual(raw, normalized) 决定是否备份并写盘,基准已被污染。对一份规范 config.json(该等值判断在规范文件上必为真,见 repair_test.go:43-46)手工删掉 review_stages.small 后运行 doctor,会跳过写盘、result.Changed=false 并打印「config.json is unchanged」,磁盘上仍缺该档,下次 Load 时 small 全部退化为 auto,与 doctor 当场返回的 cfg 不一致,重复运行也修不好。最小修复:fillMissingReviewStageScales 先深拷贝再规范化(或只把结果写进 root),并补一条「除缺一档外其余均规范」的 doctor 测试断言 result.Changed 为真。",
          "status": "fixed",
          "basis": "与 qa-batch-one-r1/QA-004 同一根因:provided 与 raw 共享 map,缺档回填污染 DeepEqual,规范文件上 doctor 报告 unchanged 且不写盘。本条按独立 (run_id, finding_id) 提交。",
          "fix_commit": "baa7b350eb3bf2f004864875e9b81d1d3e6c6eb4",
          "verification": "同一复现与修复见 QA-004。fix_commit baa7b350eb3bf2f004864875e9b81d1d3e6c6eb4 克隆 provided 后补 TestRepairWritesWhenOnlyOneReviewStageScaleIsMissing,断言 result.Changed 与磁盘 small 从 large 回填。最终套件在 9a2badc1bdb23dd946020aaaa950501081dbbd5c 通过。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c1",
            "epoch": 1
          },
          "submitted_revision": 20,
          "record_id": "pm-04-fix-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-04",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:19:38.687402066Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "新增的 internal/config/review_stages_test.go 与保留在 config_test.go 的 TestReviewStagesDefaultsAndValidation 重复覆盖同一行为:缺 review_stages 节 → 两档全 auto、{\"PM\":\"always\"} 必须报错、平铺映射到两档,三处断言在两个文件中各有一份(其中「invalid mode」用例输入完全相同)。同一行为在两处维护会造成改一处另一处仍绿的假信心。最小修复:删除 config_test.go:665-687 的这三段,仅保留该测试独有的 DefaultReviewStages 形状检查、nil/\"auto\"/[]any 类型错误与 language 用例,按档语义断言统一留在 review_stages_test.go。",
          "status": "fixed",
          "basis": "已对照 review_stages_test.go 与 config_test.go:TestReviewStagesDefaultsAndValidation,确认缺节全 auto、平铺映射两档、{\"PM\":\"always\"} 报错三处各有一份。",
          "fix_commit": "9a2badc1bdb23dd946020aaaa950501081dbbd5c",
          "mechanical": "redundant-test",
          "verification": "已删除 config_test.go 中这三段,仅保留 DefaultReviewStages 形状、nil/\"auto\"/[]any 类型错误与 language=fr。按档语义仍由 review_stages_test.go 覆盖。`go test ./internal/config -count=1 -run 'TestReviewStagesDefaultsAndValidation|TestValidateReviewStages'` 通过。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c1",
            "epoch": 1
          },
          "submitted_revision": 22,
          "record_id": "pm-08-deferred-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-08",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:19:38.82208934Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\": {\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。触发少见、后果仅为提示不精确;如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。",
          "status": "deferred",
          "basis": "已核实:{\"Owner\":{\"PM\":\"auto\"}} 在 classifyReviewStages 中 Owner 既非档名也非角色;reviewStagesLooksFlat 因取值为 object 为假,NormalizeReviewStages 走 config.review_stages_has_unknown_scales。提示不精确属实,low 项本轮不改文案。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 27,
          "record_id": "pm-01-fix-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-01",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.307295605Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "TUI 偏好写回把项目覆盖的 `tui` 值写进作用域 config.json,违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与本次新增的 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。internal/tui/prefs.go:20-26 的 loadPrefs 读的是合并后配置 config.Load(true),App 的 Theme/Columns/MinColumnWidth/RefreshSecs/Single 由 internal/tui/cmd.go:69 从该合并值初始化;internal/tui/options_panel.go:537-546 的 persistUI 用这些 App 字段组装 uiPrefs,internal/tui/prefs.go:29-36 的 savePrefs 在 config.Update 回调里无条件 cfg.TUI = value 并落盘作用域文件。persistUI 由 options_panel.go:457-460(persistNow,任意分区提交)、options_panel.go:511(save)、options_form.go:711-714(applyInterface)以及 app.go:150 → prefs.go:63-67(saveColumns)四条路径触发。最小修复:savePrefs 以 config.Update 回调里收到的作用域 cfg.TUI 为基底,只写入本次实际变更的字段(显示仍用合并值),并在 TestOptionsSaveLeavesOverlayIsolated 的覆盖文件中加入 tui 键。",
          "status": "fixed",
          "basis": "与 QA-002 同根因。独立核实 5631b18 persistUI/savePrefs/saveColumns 写回路径后，已按作用域 TUI 基底只写变更字段，并在 TestOptionsSaveLeavesOverlayIsolated 的覆盖文件中加入 tui 键。",
          "fix_commit": "fe34100f9acd41b8a79f024ff4f73134700a670d",
          "verification": "同 QA-002：fe34100f9acd41b8a79f024ff4f73134700a670d 上隔离测试通过；最终交付 f7b31bc529958ac268d2709ffd7fd99000c92bfb，go test -count=1 ./... 1265 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 28,
          "record_id": "pm-02-fix-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-02",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.418904233Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "覆盖文件里使用文档声明仍然合法的旧平铺 review_stages 时,合并结果变成档名与角色同层的混合形态,该项目下所有读配置的命令直接报错。internal/config/overlay.go:205-210 的 mergeOverlayRaw 只对作用域原始 JSON 调用 NormalizeReviewStages,覆盖侧原样进入 deepMerge(overlay.go:44-58 对同键 object 递归合并),而 internal/config/review_stages.go:79-85 对混合形态直接报错。作用域 config.json 一经 kander 写出必为两档(review_stages_test.go:174-211),因此这是常见组合。最小修复:mergeOverlayRaw 在 deepMerge 前对 overlay[\"review_stages\"] 同样调用 NormalizeReviewStages,并补一条「覆盖平铺 + 作用域两档」的合并测试。",
          "status": "fixed",
          "basis": "与 QA-001 同根因。独立核实 5631b18 mergeOverlayRaw 只规范化作用域；已对 overlay[\"review_stages\"] 同样调用 NormalizeReviewStages，并新增作用域两档+覆盖平铺用例。",
          "fix_commit": "2e70155858c9a5240ed21ede0d7758a7e770ade9",
          "verification": "同 QA-001：2e70155858c9a5240ed21ede0d7758a7e770ade9 上 TestLoadMergesReviewStagesAfterNormalizingFlatOverlay 通过；最终交付 f7b31bc529958ac268d2709ffd7fd99000c92bfb，go test -count=1 ./... 1265 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 29,
          "record_id": "pm-05-rejected-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-05",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.524805008Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "[out-of-contract] kander doctor 用合并后的 agent 定义判定可用性,再据此改写并落盘作用域 config.json 的 kanban_agent/kanban_agents/reviewers。项目覆盖把 agents.\u003cname\u003e.path 指到不存在的路径时,用户在该项目里跑一次 doctor 就会永久改掉全局的执行 Agent/审核者选择。写入值本身仍取自作用域配置,未违反「覆盖值不得反向写入」的字面契约,故不作为门禁项;若要收紧,修复阶段的可用性判定改用 config.LoadScope 的 agent 定义。",
          "status": "rejected",
          "basis": "独立核实 5631b18 internal/menu/doctor.go:67 用 config.Load(true) 判 Agent 可用性，repairDoctorConfig 经 Repair 只写作用域文件，写入的 kanban_agent/reviewers 仍取自作用域配置对象，未把覆盖独有键抄进 config.json。超出本卡验收（写入以未合并作用域为 RMW 基底）的字面契约；条目原文已标 [out-of-contract]。收紧可用性判定属相邻加固，见 OUT_OF_SCOPE 加固/相邻功能。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 30,
          "record_id": "pm-06-deferred-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-06",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.628512934Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "存在两个语义相近的原始 JSON 克隆实现,差别只在是否复制数组,后续改动容易误用其中之一。可合并为单一深拷贝实现。",
          "status": "deferred",
          "basis": "独立核实 5631b18：cloneRawObject（review_stages.go:36-46，只递归 object）与 cloneRawObjectDeep（overlay.go:33-54，含数组）并存。建议项，非本轮缺陷；disposition-only 不合并实现。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 31,
          "record_id": "pm-07-deferred-r1",
          "run_id": "pm-batch-one-r2",
          "finding_id": "PM-07",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.725939879Z",
          "report_hash": "692a65d8fb3b259375d82fe4f79f4213216e7b6b7a10cedb16283ee30e3398bd",
          "original": "每次 config.Load 都会新起一个 `git worktree list --porcelain` 子进程用于定位覆盖文件,kander config 因摘要处再次调用 OverlayPath 而在单条命令内跑两次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。",
          "status": "deferred",
          "basis": "独立核实 5631b18：OverlayPath→gitMainWorktree 每次 exec git worktree list --porcelain（paths.go:125）；FormatConfigLines 在 Load 之后再次 OverlayPath(\"\")（format.go:112）。性能建议，验收未要求进程内缓存；本轮不开修复。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "pm-batch-one-r3",
        "batch_id": "batch-one",
        "previous_run_id": "pm-batch-one-r2",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "PM",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "5d632af67ee28a19769972298a11984ced5e76e6",
        "reviewed_commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "f26cc2515a3872bcbf1e048f7af5b1074ece4b8ac7f6b74933df66cb634c97d1",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T19:29:42.247591225Z",
        "finished_at": "2026-09-08T19:38:12.672273857Z",
        "duration_ms": 510424,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "3acff31835c63f4b8f8e6adb8f89a93e3b0106a900817b62242e6822bc1f2de8",
          "output.raw": "205820cfd2b52a7aa2b500f6ffab4eaa9213cf028b6dd51a5313787278d8c9c5",
          "prompt.txt": "145e7237953ac3d126a848fc6958d811e3dd91cdbf00761a32dd2047b14e4446",
          "report.md": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "review-context.md": "f26cc2515a3872bcbf1e048f7af5b1074ece4b8ac7f6b74933df66cb634c97d1",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-09",
            "tier": "high",
            "text": "本轮 5d632af 让 ConfiguredLanguage 返回合并覆盖后的语言,于是 TUI 选项面板保存会把覆盖文件独有的 language 写进作用域 config.json,重新违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。链路:internal/config/language.go:106-119 合并覆盖 → internal/menu/options.go:153-155 在 Session.prepare 中 cfg.Language = config.ConfiguredLanguage() → options.go:163 s.Config = cfg → options.go:501-507 Session.Save() 调 config.SaveIfUnchanged(s.Config, s.existing) → internal/config/loadsave.go:352-377 直接 saveConfigAt 整份写出作用域文件。会话由 internal/tui/options_panel.go:125,130 以 LoadScope(true)+menu.NewSession 构造,保存入口为 options_panel.go:464(persistNow,任一分区提交)与 options_panel.go:522(save)。失败场景:作用域 language=en,项目 .kander-config.json={\"language\":\"ja\"}(language 在 internal/config/overlay.go:27 允许键内),用户在该项目内保存一次选项面板,作用域 config.json 的 language 即被永久改成 ja 并对所有其他项目生效,删除覆盖文件也不恢复。与 PM-01 是同一条契约,键由 tui 换成 language,且由本轮修复引入。最小修复:Session.prepare 用不合并覆盖的作用域显式语言赋给 cfg.Language(新增 configuredScopeLanguage(),或取 s.existing 的显式值),面板文案单独用 config.BindEffectiveLanguage() 绑定合并值;并补「覆盖含 language、TUI 保存后作用域 language 不变」的测试。",
            "evidence": "internal/config/language.go:106-119(ConfiguredLanguage 合并覆盖,5d632af 新增);internal/menu/options.go:153-155,163(prepare 用该合并值设 cfg.Language 并存入 s.Config);internal/menu/options.go:501-507(Session.Save → SaveIfUnchanged);internal/config/loadsave.go:352-377(SaveIfUnchanged → saveConfigAt 整份写作用域文件);internal/tui/options_panel.go:125,130(LoadScope+NewSession)、:464、:522(两条保存入口);internal/config/overlay.go:27(language 在允许键内)。测试盲区:internal/config/overlay_test.go:387-414 只断言 ConfiguredLanguage 自身不写盘;internal/tui/options_test.go:59-70 的 newTestSession 走 menu.NewSessionForTest(internal/menu/options.go:572 cfg.Language = existing.Language),不经过 ConfiguredLanguage。"
          },
          {
            "id": "PM-10",
            "tier": "medium",
            "text": "savePrefs 的注释与实现不符,且新增的五个条件判断是空操作。internal/tui/prefs.go:28-29 声称「writes only the UI fields that differ from the unmerged scope config」,但 prefs.go:34-51 对 Columns/MinColumnWidth/Theme/Refresh/Single 一律执行 if desired.X != next.X { next.X = desired.X },每个分支都等价于无条件赋值,函数结束时 next == desired,cfg.TUI = next 与修复前的整节覆盖完全等价;真正实现覆盖隔离的是调用方(options_panel.go:542 传入作用域 TUI,prefs.go:83-90 的 saveColumns 绕开本函数)。注释描述的保护并不存在,后续维护者据此认为「传合并值也安全」时,PM-01 那类泄漏会静默回归。最小修复:要么删掉五个空判断并把注释改为「写入调用方给出的作用域 UI 偏好整节」,要么改为真正的差异写入(额外传入基线,如 savePrefs(prefs, baseline uiPrefs),只写与基线不同的字段)。",
            "evidence": "internal/tui/prefs.go:28-29(注释);internal/tui/prefs.go:31-51(desired := prefsConfig(prefs) 后五个 if 均为空操作,cfg.TUI = next == desired);internal/tui/options_panel.go:542(唯一非测试调用点,传入 prefsFromConfig(p.session.Config.TUI));internal/tui/prefs.go:83-90(saveColumns 改为直接 config.Update,不再经 savePrefs)。",
            "mechanical": "documentation"
          }
        ],
        "NON_BLOCKING": [
          {
            "id": "PM-11",
            "tier": "low",
            "text": "ConfiguredLanguage 的两处提前返回让界面语言与 kander config --json 不一致。其一:覆盖文件非法(JSON 错误、禁止键、未知键、非常规文件)时 internal/config/language.go:106-109 返回 \"\",界面语言退回环境语言,于是「覆盖文件有问题」这条错误本身以非用户配置的语言显示。其二:作用域 config.json 不存在时 language.go:91-94 提前返回 \"\",覆盖文件的 language 对界面无效,而 config.Load(true) 以 DefaultConfig 为底合并覆盖,kander config --json 仍输出覆盖语言、同一条命令的「语言」行却是环境语言。两种触发都少见,后果仅为提示语言不一致。若要收紧:这两个分支在返回前仍尝试合并覆盖,或至少在作用域文件缺失时以默认配置为底做同样的合并。",
            "evidence": "internal/config/language.go:91-94(作用域文件缺失即返回)、:106-109(覆盖读取出错即返回)、:159-166(BindEffectiveLanguage 据此退回 ResolveLanguage);internal/config/loadsave.go:63-79,95-115(Load 在作用域缺失时以 DefaultConfig 为底合并覆盖);internal/config/format.go:146-151(kander config 的语言行取 ConfiguredLanguage)。"
          },
          {
            "id": "PM-06",
            "tier": "suggest",
            "text": "两个语义相近的原始 JSON 克隆实现仍并存,差别只在是否复制数组;本轮 repair.go:104 新增一处 cloneRawObjectDeep 调用,与同一调用链上 repair.go:158-166 使用的 cloneRawObject 相邻,误用其一的风险比上一轮更高。合并为单一深拷贝实现即可。",
            "evidence": "internal/config/review_stages.go:36-46(cloneRawObject,只递归 object);internal/config/overlay.go:33-53(cloneRawValue/cloneRawObjectDeep,object + array);internal/config/repair.go:104(本轮新增的 cloneRawObjectDeep)与 internal/config/repair.go:158-166(fillMissingReviewStageScales 用 cloneRawObject)。",
            "lineage": {
              "run_id": "pm-batch-one-r2",
              "finding_id": "PM-06"
            }
          },
          {
            "id": "PM-07",
            "tier": "suggest",
            "text": "覆盖文件定位仍每次新起 git worktree list --porcelain 子进程,且本轮被加重:ConfiguredLanguage 现在也调用 readOverlay(\"\"),而它在每条命令开头(BindEffectiveLanguage)与 kander config 的摘要处各被调用一次,单条 kander config 的定位次数由 2 次升至 4 次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。",
            "evidence": "internal/config/language.go:106(本轮新增的 readOverlay 调用);internal/menu/commands.go:12-14(每条命令开头 prepareLanguage → BindEffectiveLanguage → ConfiguredLanguage);internal/config/format.go:146(FormatConfigLines 再次调用)、format.go:112(同一函数内另有 OverlayPath(\"\"));internal/config/loadsave.go:104-106(Load 的覆盖读取);internal/config/paths.go:125(exec.Command(\"git\", …))。",
            "lineage": {
              "run_id": "pm-batch-one-r2",
              "finding_id": "PM-07"
            }
          },
          {
            "id": "PM-08",
            "tier": "low",
            "text": "review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\":{\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。本轮未改动该代码,处置为 deferred,现象仍在。如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。",
            "evidence": "internal/config/review_stages.go:49-63(classifyReviewStages);internal/config/review_stages.go:86-99(reviewStagesLooksFlat 为假时统一走 config.review_stages_has_unknown_scales)。",
            "lineage": {
              "run_id": "pm-batch-one-r2",
              "finding_id": "PM-08"
            }
          }
        ]
      },
      "assignment": {
        "run_id": "pm-batch-one-r3",
        "batch_id": "batch-one",
        "author": "claude-orchestrator",
        "basis": "assigned by each card's GOAL/OUT_OF_SCOPE and the fix commits that introduced them: PM-09 (language write-back via ConfiguredLanguage, introduced by card 2 commit 5d632af), PM-10 (savePrefs comment vs implementation, card 2 commit fa8ab21), PM-11 (ConfiguredLanguage early returns, card 2) and carried PM-06/PM-07 hit card 2; carried PM-08 hits card 1's review_stages error text",
        "items": {
          "PM-06": [
            "20260909-project-config-overlay-task"
          ],
          "PM-07": [
            "20260909-project-config-overlay-task"
          ],
          "PM-08": [
            "20260909-review-stages-per-size-task"
          ],
          "PM-09": [
            "20260909-project-config-overlay-task"
          ],
          "PM-10": [
            "20260909-project-config-overlay-task"
          ],
          "PM-11": [
            "20260909-project-config-overlay-task"
          ]
        },
        "owners": {
          "20260909-project-config-overlay-task": "cursor",
          "20260909-review-stages-per-size-task": "cursor"
        },
        "recorded_at": "2026-09-08T19:38:55.552411666Z"
      },
      "records": [
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r2-c1",
            "epoch": 2
          },
          "submitted_revision": 30,
          "record_id": "pm-08-deferred-r3",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-08",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:42:08.529885951Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "review_stages 中未知键的取值为 object 时,错误信息把它当作档名报出。例如 {\"Owner\":{\"PM\":\"auto\"}} 会报「含未知档名: Owner; 只允许 large, small」,而用户写的是角色名。本轮未改动该代码,处置为 deferred,现象仍在。如需修正,可在 classifyReviewStages 中对「非档名且取值为 object」的键改用兼顾角色与档名的文案。",
          "status": "deferred",
          "basis": "已在未改交付 9a2badc1bdb23dd946020aaaa950501081dbbd5c 上独立复现: Validate(review_stages={\"Owner\":{\"PM\":\"auto\"}}) 报「review_stages 含未知档名: Owner; 只允许 large, small」。classifyReviewStages 将 Owner 列入 unknown;reviewStagesLooksFlat 因取值为 object 为假,NormalizeReviewStages 走 config.review_stages_has_unknown_scales。与上一轮 reviews/pm-batch-one-r2/dispositions/pm-08-deferred-r1.json 同一现象,本 sync 轮不改代码,维持 deferred。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r2-c2",
            "epoch": 3
          },
          "submitted_revision": 44,
          "record_id": "pm-09-fix-r2",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-09",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:45:12.165413Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "本轮 5d632af 让 ConfiguredLanguage 返回合并覆盖后的语言,于是 TUI 选项面板保存会把覆盖文件独有的 language 写进作用域 config.json,重新违反卡 2「所有写入路径以未合并的作用域配置为读-改-写基底、覆盖值不得反向写入」的用户决策与对应验收,也与 rules/KANDER-AGENTS.md:19「Overlay values are never written back」冲突。链路:internal/config/language.go:106-119 合并覆盖 → internal/menu/options.go:153-155 在 Session.prepare 中 cfg.Language = config.ConfiguredLanguage() → options.go:163 s.Config = cfg → options.go:501-507 Session.Save() 调 config.SaveIfUnchanged(s.Config, s.existing) → internal/config/loadsave.go:352-377 直接 saveConfigAt 整份写出作用域文件。会话由 internal/tui/options_panel.go:125,130 以 LoadScope(true)+menu.NewSession 构造,保存入口为 options_panel.go:464(persistNow,任一分区提交)与 options_panel.go:522(save)。失败场景:作用域 language=en,项目 .kander-config.json={\"language\":\"ja\"}(language 在 internal/config/overlay.go:27 允许键内),用户在该项目内保存一次选项面板,作用域 config.json 的 language 即被永久改成 ja 并对所有其他项目生效,删除覆盖文件也不恢复。与 PM-01 是同一条契约,键由 tui 换成 language,且由本轮修复引入。最小修复:Session.prepare 用不合并覆盖的作用域显式语言赋给 cfg.Language(新增 configuredScopeLanguage(),或取 s.existing 的显式值),面板文案单独用 config.BindEffectiveLanguage() 绑定合并值;并补「覆盖含 language、TUI 保存后作用域 language 不变」的测试。",
          "status": "fixed",
          "basis": "独立核实目标 5d632af67ee28a19769972298a11984ced5e76e6：ConfiguredLanguage 合并覆盖后，menu.Session.prepare 用其赋值 cfg.Language，SaveIfUnchanged 整份写作用域。已新增 ConfiguredScopeLanguage() 只读未合并作用域；prepare 用该值填会话，面板文案改 BindEffectiveLanguage()。",
          "fix_commit": "32ba6ee3bb197acc504d6208fe521cb1d68d0bfc",
          "verification": "go test -count=1 ./internal/config -run 'TestConfiguredLanguage|TestConfiguredLanguageUsesOverlay' 与 go test -count=1 ./internal/menu -run TestSessionSaveLeavesOverlayLanguageIsolated 在 32ba6ee3bb197acc504d6208fe521cb1d68d0bfc 通过；最终交付 777b1f088985e3d11cec3bd9b4dde9271822119e 上 go test -count=1 ./... 1267 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r2-c2",
            "epoch": 3
          },
          "submitted_revision": 45,
          "record_id": "pm-10-fix-r2",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-10",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:45:12.538559895Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "savePrefs 的注释与实现不符,且新增的五个条件判断是空操作。internal/tui/prefs.go:28-29 声称「writes only the UI fields that differ from the unmerged scope config」,但 prefs.go:34-51 对 Columns/MinColumnWidth/Theme/Refresh/Single 一律执行 if desired.X != next.X { next.X = desired.X },每个分支都等价于无条件赋值,函数结束时 next == desired,cfg.TUI = next 与修复前的整节覆盖完全等价;真正实现覆盖隔离的是调用方(options_panel.go:542 传入作用域 TUI,prefs.go:83-90 的 saveColumns 绕开本函数)。注释描述的保护并不存在,后续维护者据此认为「传合并值也安全」时,PM-01 那类泄漏会静默回归。最小修复:要么删掉五个空判断并把注释改为「写入调用方给出的作用域 UI 偏好整节」,要么改为真正的差异写入(额外传入基线,如 savePrefs(prefs, baseline uiPrefs),只写与基线不同的字段)。",
          "status": "fixed",
          "basis": "独立核实 5d632af 上 prefs.go:28-51：注释声称按差异写入，五个 if desired.X != next.X 均为空操作，结束时 next==desired。已删除空判断，注释改为整节写入调用方快照、隔离由调用方保证。",
          "fix_commit": "777b1f088985e3d11cec3bd9b4dde9271822119e",
          "mechanical": "documentation",
          "verification": "逐句比对 777b1f088985e3d11cec3bd9b4dde9271822119e 的 internal/tui/prefs.go：1) 旧注释「writes only the UI fields that differ from the unmerged scope config」已删除；2) 五个 if desired.Columns/MinColumnWidth/Theme/Refresh/Single 空操作已删除；3) 新注释写明「writes the caller-provided UI preferences into the scope config.json as a whole TUI section」且「does not filter overlay-only fields」；4) 实现现为 cfg.TUI = prefsConfig(prefs) 整节赋值，与注释一致；5) persistUI 仍传入 prefsFromConfig(session.Config.TUI)，saveColumns 仍只 patch Columns。go test -count=1 ./internal/tui -run 'TestColumnCountPrefs|TestOptionsSaveLeavesOverlayIsolated|TestThemeChangeKeepsInterfaceState' 通过；最终提交 go test -count=1 ./... 1267 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r2-c2",
            "epoch": 3
          },
          "submitted_revision": 46,
          "record_id": "pm-11-deferred-r2",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-11",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:45:12.909046585Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "ConfiguredLanguage 的两处提前返回让界面语言与 kander config --json 不一致。其一:覆盖文件非法(JSON 错误、禁止键、未知键、非常规文件)时 internal/config/language.go:106-109 返回 \"\",界面语言退回环境语言,于是「覆盖文件有问题」这条错误本身以非用户配置的语言显示。其二:作用域 config.json 不存在时 language.go:91-94 提前返回 \"\",覆盖文件的 language 对界面无效,而 config.Load(true) 以 DefaultConfig 为底合并覆盖,kander config --json 仍输出覆盖语言、同一条命令的「语言」行却是环境语言。两种触发都少见,后果仅为提示语言不一致。若要收紧:这两个分支在返回前仍尝试合并覆盖,或至少在作用域文件缺失时以默认配置为底做同样的合并。",
          "status": "deferred",
          "basis": "独立核实 5d632af 与本轮 language.go：configuredScopeObject 在作用域文件缺失或 Validate 失败时返回 nil，ConfiguredLanguage 随之返回 \"\"；readOverlay 出错仍提前返回，不回退到作用域语言。与 Load(true) 缺文件仍合并覆盖不一致。属低等级提示语言不一致，本轮 disposition-only 不改。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r2-c2",
            "epoch": 3
          },
          "submitted_revision": 47,
          "record_id": "pm-06-deferred-r2",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-06",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:45:13.059949569Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "两个语义相近的原始 JSON 克隆实现仍并存,差别只在是否复制数组;本轮 repair.go:104 新增一处 cloneRawObjectDeep 调用,与同一调用链上 repair.go:158-166 使用的 cloneRawObject 相邻,误用其一的风险比上一轮更高。合并为单一深拷贝实现即可。",
          "status": "deferred",
          "basis": "延续 pm-batch-one-r2/PM-06（记录 reviews/pm-batch-one-r2/dispositions/pm-06-deferred-r1.json）。独立核实 5d632af：cloneRawObject（review_stages.go:36-46）与 cloneRawObjectDeep（overlay.go:33-54）仍并存；repair.go:104 用后者、fillMissingReviewStageScales 用前者。建议项，本轮不开修复。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r2-c2",
            "epoch": 3
          },
          "submitted_revision": 48,
          "record_id": "pm-07-deferred-r2",
          "run_id": "pm-batch-one-r3",
          "finding_id": "PM-07",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:45:13.187112313Z",
          "report_hash": "68e04a2a7dcd76ebb5ba8c2dd07d27ecf1b116405d02494884fb5cc97681c0c2",
          "original": "覆盖文件定位仍每次新起 git worktree list --porcelain 子进程,且本轮被加重:ConfiguredLanguage 现在也调用 readOverlay(\"\"),而它在每条命令开头(BindEffectiveLanguage)与 kander config 的摘要处各被调用一次,单条 kander config 的定位次数由 2 次升至 4 次;TUI 定时刷新与 liveness/notify 等高频路径同样受影响。可在进程内按 cwd 缓存一次定位结果。",
          "status": "deferred",
          "basis": "延续 pm-batch-one-r2/PM-07（记录 reviews/pm-batch-one-r2/dispositions/pm-07-deferred-r1.json）。独立核实：ConfiguredLanguage 仍调用 readOverlay(\"\")，BindEffectiveLanguage 与 FormatConfigLines/Load/OverlayPath 叠加，单条 kander config 仍多次 git worktree list。性能建议，本轮不开修复。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "qa-batch-one-r1",
        "batch_id": "batch-one",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "QA",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "363d52e6e53385f7ad1ccfb81b60a3d4838d0d7eb1b1a0c3fe2c99087bea7493",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T18:44:24.74922159Z",
        "finished_at": "2026-09-08T18:53:34.058131395Z",
        "duration_ms": 549308,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "100b658a6d867f9fe3e41e4d9b8755dfab44cb6b8216911974b3f3f445e8c528",
          "output.raw": "0c2dc1ebff37e450e26bea0e052b961fa6d82ebd0485e975e9bc067a46735e11",
          "prompt.txt": "30eabeb2808932fe5be48f1a7d80f7a741e90760c83614d9497f11526ff2f113",
          "report.md": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "review-context.md": "363d52e6e53385f7ad1ccfb81b60a3d4838d0d7eb1b1a0c3fe2c99087bea7493",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "QA-001",
            "tier": "high",
            "text": "覆盖文件使用文档承诺仍然合法的旧平铺 review_stages 形态时,mergeOverlayRaw 只规范化作用域一侧,深合并产生「档名与角色同层」的混合形态,Validate 直接报错,导致该项目目录下所有经 config.Load 的命令(kander config / config --json / doctor / start / resume / notify / review)全部失败,且错误指向 review_stages 而非覆盖文件。触发条件几乎必然成立:作用域 config.json 由 Save/doctor/TUI 写出时必含两档结构,作用域文件缺失时 loadScopeRawAt 也会用 DefaultConfig() 编码出两档。最小修复:在 mergeOverlayRaw 中对 overlay 原始 JSON 同样调用 NormalizeReviewStages,并补「作用域两档 + 覆盖平铺」的合并用例。",
            "evidence": "internal/config/overlay.go:205-210 (mergeOverlayRaw 仅 normalizeScopeReviewStages(scope));internal/config/overlay.go:47-70 (deepMerge 不做形态归一);internal/config/review_stages.go:88-94 (混合形态报 config.review_stages_mixes_scale_and_role_keys);internal/config/loadsave.go:112-116 (Load 用合并结果 Validate);internal/config/loadsave.go:66-79 (作用域缺失时以 DefaultConfig 两档结构作为基底);契约文档承诺旧平铺合法:docs/custom-agents.md:83、rules/KANDER-REVIEW-RULES.md:330、AGENTS.md:51"
          },
          {
            "id": "QA-002",
            "tier": "high",
            "text": "TUI 的界面偏好写回路径没有改用 LoadScope:loadPrefs 用 config.Load(true) 读到的是合并后的值,App 以此初始化,persistUI 与 saveColumns 再把 p.app 现值整节写回 config.Update(cfg.TUI = value)。当项目 .kander-config.json 设置任一 tui.* 键(如 theme=dark)时,用户在该项目下调整列数或刷新间隔,就会把覆盖文件独有的 theme 永久写入作用域 config.json,删除覆盖文件后污染仍在。这违反卡 2 的 USER_DECISIONS「所有写入路径以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与对应验收项,也与 rules/KANDER-AGENTS.md:19 的文字承诺矛盾。新增测试只断言 kanban_agent,覆盖不到该路径。最小修复:savePrefs 在 config.Update 回调内只写本次实际修改的字段,或让 loadPrefs 额外保留 LoadScope 基线供写回使用;并补覆盖文件含 tui 键时的隔离测试。",
            "evidence": "internal/tui/prefs.go:20-25 (loadPrefs 用 config.Load(true));internal/tui/cmd.go:69,104 (合并值构造 App);internal/tui/options_panel.go:537-551 (persistUI 从 p.app 组装并 savePrefs);internal/tui/prefs.go:29-35 (savePrefs 整节 cfg.TUI = value 写作用域);internal/tui/prefs.go:62-66 与 internal/tui/app.go:150 (saveColumns 同样先 loadPrefs 合并再整节写回);冲突文档 rules/KANDER-AGENTS.md:19;未覆盖该路径的测试 internal/tui/options_test.go:TestOptionsSaveLeavesOverlayIsolated"
          },
          {
            "id": "QA-003",
            "tier": "medium",
            "text": "覆盖文件的 language 键在 overlayAllowedKeys 中被接受、会出现在 kander config --json 与摘要输出里,但决定 CLI/TUI 实际界面语言的 ConfiguredLanguage() 自行直读作用域 config.json 字节,绕过覆盖合并,因此覆盖值对所有 Text() 输出完全无效。同一节的 agent_language 走 config.Load 因而生效,行为不一致。这与 rules/KANDER-AGENTS.md:18 声明的「项目覆盖 \u003e 作用域配置 \u003e 默认值」以及 EXPECTED_OUTCOME「config.Load 及其等价读取入口返回合并后的生效配置」冲突。最小修复:让 ConfiguredLanguage() 在取 explicitConfigLanguage 前套用与 loadEffective 相同的覆盖合并,并补 CLI 测试;若刻意不支持,则应把 language 移出 overlayAllowedKeys 并在规则文档写明例外。",
            "evidence": "internal/config/overlay.go:26-38 (language 在 overlayAllowedKeys);internal/config/language.go:85-106 (ConfiguredLanguage 直读 ConfigPath + readConfigBytes);internal/config/language.go:144-152,158-161 (BindEffectiveLanguage/Text 全部基于该值);internal/menu/commands.go:12-14,61 (每条命令开头调用);对照 internal/board/cmd.go:71-77 与 internal/launch/prompts.go:68-74 (agent_language 走 config.Load 生效);冲突文档 rules/KANDER-AGENTS.md:18、AGENTS.md:50"
          },
          {
            "id": "QA-004",
            "tier": "medium",
            "text": "repairValues 通过 asObject(raw) 拿到的 provided 与 repairAt 用于变更检测的 raw 是同一个 map,fillMissingReviewStageScales 就地写入 provided[\"review_stages\"] 因而污染了 raw。对于其余字段本就与规范化输出一致的配置(例如把 Save 写出的完整 config.json 手工删掉整个 \"small\" 块),repairAt 的 reflect.DeepEqual(raw, normalized) 会因回填后的 raw 而成立,doctor 既不备份也不写盘、报告 config_json_is_unchanged,磁盘上仍只有 large 档。结果是 Repair 返回的 cfg 里 small 是 large 的副本,而下一次 Load 得到的 small 是全 auto 默认值,与 doctor 的报告不一致,卡 1「缺失一档从另一档回填」的验收在该情形下未真正生效;现有 repair_test 用例的配置缺失多个可选节,DeepEqual 必为假,覆盖不到该分支。最小修复:在 repair.go:100 取得 provided 后先 cloneRawObjectDeep,使回填不再触碰 raw,并补「其余字段完整、仅缺 small 档」的用例断言 result.Changed 与磁盘内容。",
            "evidence": "internal/config/config.go:638-641 (asObject 返回同一 map);internal/config/repair.go:100 (provided 与 raw 同源);internal/config/repair.go:141,149-167 (fillMissingReviewStageScales 就地写回 provided[\"review_stages\"]);internal/config/repair.go:74 (reflect.DeepEqual(raw, normalized) 决定是否写盘);internal/config/repair_test.go:125-150 (现有用例缺失多个可选节,触达不到该分支)"
          }
        ],
        "NON_BLOCKING": [
          {
            "id": "QA-101",
            "tier": "recommend",
            "text": "overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
            "evidence": "internal/config/overlay.go:19-38 (硬编码 overlayForbiddenKeys / overlayAllowedKeys);internal/config/config.go:656-770 (Validate 无顶层键白名单,未知键被忽略)"
          },
          {
            "id": "QA-102",
            "tier": "low",
            "text": "deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
            "evidence": "internal/config/overlay.go:47-70 (deepMerge 键级合并);internal/config/rules.go:69-82 (validateRules 以 DefaultRules(false) 为底并执行 ValidateRules 依赖检查);internal/config/config.go:719-725 (缺 rules 节时用 legacyRules());AGENTS.md:49 (旧配置缺整节时七开关全开)"
          },
          {
            "id": "QA-103",
            "tier": "suggest",
            "text": "inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段,只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
            "evidence": "internal/config/overlay.go:118 (overlayPathSafe);internal/config/paths.go:39-50 (rejectLeafReparse 已做 Lstat + IsReparsePoint);internal/config/overlay.go:121-130 (重复的 Lstat + IsReparsePoint)"
          },
          {
            "id": "QA-104",
            "tier": "suggest",
            "text": "kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
            "evidence": "internal/config/format.go:137-144 (分档分支拼接为 Text(\"config.review_stages\")+\" \"+summary);internal/config/format.go:112-118,124-136 (其余行统一使用 \": \")"
          }
        ]
      },
      "assignment": {
        "run_id": "qa-batch-one-r1",
        "batch_id": "batch-one",
        "author": "claude-orchestrator",
        "basis": "assigned by each card's GOAL/OUT_OF_SCOPE and actual changed files: QA-004 and QA-104 hit card 1's doctor backfill and kander config summary; QA-001/002/003 and QA-101/102/103 hit card 2's overlay merge, write isolation and overlay allowed keys",
        "items": {
          "QA-001": [
            "20260909-project-config-overlay-task"
          ],
          "QA-002": [
            "20260909-project-config-overlay-task"
          ],
          "QA-003": [
            "20260909-project-config-overlay-task"
          ],
          "QA-004": [
            "20260909-review-stages-per-size-task"
          ],
          "QA-101": [
            "20260909-project-config-overlay-task"
          ],
          "QA-102": [
            "20260909-project-config-overlay-task"
          ],
          "QA-103": [
            "20260909-project-config-overlay-task"
          ],
          "QA-104": [
            "20260909-review-stages-per-size-task"
          ]
        },
        "owners": {
          "20260909-project-config-overlay-task": "cursor",
          "20260909-review-stages-per-size-task": "cursor"
        },
        "recorded_at": "2026-09-08T18:57:26.645213743Z"
      },
      "records": [
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c1",
            "epoch": 1
          },
          "submitted_revision": 18,
          "record_id": "qa-004-fix-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-004",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:19:33.891299675Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "repairValues 通过 asObject(raw) 拿到的 provided 与 repairAt 用于变更检测的 raw 是同一个 map,fillMissingReviewStageScales 就地写入 provided[\"review_stages\"] 因而污染了 raw。对于其余字段本就与规范化输出一致的配置(例如把 Save 写出的完整 config.json 手工删掉整个 \"small\" 块),repairAt 的 reflect.DeepEqual(raw, normalized) 会因回填后的 raw 而成立,doctor 既不备份也不写盘、报告 config_json_is_unchanged,磁盘上仍只有 large 档。结果是 Repair 返回的 cfg 里 small 是 large 的副本,而下一次 Load 得到的 small 是全 auto 默认值,与 doctor 的报告不一致,卡 1「缺失一档从另一档回填」的验收在该情形下未真正生效;现有 repair_test 用例的配置缺失多个可选节,DeepEqual 必为假,覆盖不到该分支。最小修复:在 repair.go:100 取得 provided 后先 cloneRawObjectDeep,使回填不再触碰 raw,并补「其余字段完整、仅缺 small 档」的用例断言 result.Changed 与磁盘内容。",
          "status": "fixed",
          "basis": "已独立复现: asObject 只做类型断言,repairValues 把同一 map 交给 fillMissingReviewStageScales;对 Save 写出的完整 config 仅删除 review_stages.small 后 Repair 的 result.Changed 为 false,磁盘仍缺 small,再次 Load 得到全 auto。",
          "fix_commit": "baa7b350eb3bf2f004864875e9b81d1d3e6c6eb4",
          "verification": "修复前 `go test ./internal/config -count=1 -run TestRepairWritesWhenOnlyOneReviewStageScaleIsMissing` 失败,报 a normalized config missing only small must be rewritten。repairValues 在 asObject(raw) 后 cloneRawObjectDeep,再回填。同一测试随后通过,Repair 写盘且 ValidateJSON 后 small.PM=required。最终 SHA 9a2badc1bdb23dd946020aaaa950501081dbbd5c 上 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 通过,1262 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c1",
            "epoch": 1
          },
          "submitted_revision": 21,
          "record_id": "qa-104-deferred-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-104",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:19:38.754582363Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
          "status": "deferred",
          "basis": "已核实:rebase 到 5631b184 后 internal/config/format.go:137-144 在两档不同时仍拼接 Text(\"config.review_stages\")+\" \"+summary,标签后无冒号。现象属实,本修复轮按派发仅处置、不改摘要格式。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 21,
          "record_id": "qa-001-fix-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-001",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:20.653271971Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "覆盖文件使用文档承诺仍然合法的旧平铺 review_stages 形态时,mergeOverlayRaw 只规范化作用域一侧,深合并产生「档名与角色同层」的混合形态,Validate 直接报错,导致该项目目录下所有经 config.Load 的命令(kander config / config --json / doctor / start / resume / notify / review)全部失败,且错误指向 review_stages 而非覆盖文件。触发条件几乎必然成立:作用域 config.json 由 Save/doctor/TUI 写出时必含两档结构,作用域文件缺失时 loadScopeRawAt 也会用 DefaultConfig() 编码出两档。最小修复:在 mergeOverlayRaw 中对 overlay 原始 JSON 同样调用 NormalizeReviewStages,并补「作用域两档 + 覆盖平铺」的合并用例。",
          "status": "fixed",
          "basis": "独立核实目标提交 5631b184af859cd726f07bfa119e5378a5e3ded3：mergeOverlayRaw 只调用 normalizeScopeReviewStages(scope)（当时 overlay.go:205-210），deepMerge 对同键 object 递归合并，两档作用域 + 平铺覆盖会得到 {large,small,PM}，NormalizeReviewStages 报 config.review_stages_mixes_scale_and_role_keys。与 PM-02 同根因。已在 mergeOverlayRaw 中对覆盖侧同样调用 NormalizeReviewStages。",
          "fix_commit": "2e70155858c9a5240ed21ede0d7758a7e770ade9",
          "verification": "go test -count=1 ./internal/config -run TestLoadMergesReviewStagesAfterNormalizingFlatOverlay 在 2e70155858c9a5240ed21ede0d7758a7e770ade9 通过；最终交付 f7b31bc529958ac268d2709ffd7fd99000c92bfb 上 go test -count=1 ./... 计 1265 passed，go vet ./... 通过。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 22,
          "record_id": "qa-002-fix-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-002",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:20.772029266Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "TUI 的界面偏好写回路径没有改用 LoadScope:loadPrefs 用 config.Load(true) 读到的是合并后的值,App 以此初始化,persistUI 与 saveColumns 再把 p.app 现值整节写回 config.Update(cfg.TUI = value)。当项目 .kander-config.json 设置任一 tui.* 键(如 theme=dark)时,用户在该项目下调整列数或刷新间隔,就会把覆盖文件独有的 theme 永久写入作用域 config.json,删除覆盖文件后污染仍在。这违反卡 2 的 USER_DECISIONS「所有写入路径以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与对应验收项,也与 rules/KANDER-AGENTS.md:19 的文字承诺矛盾。新增测试只断言 kanban_agent,覆盖不到该路径。最小修复:savePrefs 在 config.Update 回调内只写本次实际修改的字段,或让 loadPrefs 额外保留 LoadScope 基线供写回使用;并补覆盖文件含 tui 键时的隔离测试。",
          "status": "fixed",
          "basis": "独立核实目标提交 5631b18：loadPrefs 走 config.Load(true)；persistUI 用 App 合并值整节 savePrefs；saveColumns 先 loadPrefs 再整节写回。与 PM-01 同根因。已改为 persistUI 写会话作用域 TUI，saveColumns 只补丁 Columns，savePrefs 以作用域 cfg.TUI 为基底按字段写入。",
          "fix_commit": "fe34100f9acd41b8a79f024ff4f73134700a670d",
          "verification": "go test -count=1 ./internal/tui -run 'TestOptionsSaveLeavesOverlayIsolated|TestSaveColumnsLeavesOverlayTUIIsolated|TestInterfaceWriteDoesNotCommitOtherSessionEdits|TestThemeChangeKeepsInterfaceState' 在 fe34100f9acd41b8a79f024ff4f73134700a670d 通过；最终交付 f7b31bc529958ac268d2709ffd7fd99000c92bfb 上 go test -count=1 ./... 1265 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 23,
          "record_id": "qa-003-fix-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-003",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:20.88678387Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "覆盖文件的 language 键在 overlayAllowedKeys 中被接受、会出现在 kander config --json 与摘要输出里,但决定 CLI/TUI 实际界面语言的 ConfiguredLanguage() 自行直读作用域 config.json 字节,绕过覆盖合并,因此覆盖值对所有 Text() 输出完全无效。同一节的 agent_language 走 config.Load 因而生效,行为不一致。这与 rules/KANDER-AGENTS.md:18 声明的「项目覆盖 \u003e 作用域配置 \u003e 默认值」以及 EXPECTED_OUTCOME「config.Load 及其等价读取入口返回合并后的生效配置」冲突。最小修复:让 ConfiguredLanguage() 在取 explicitConfigLanguage 前套用与 loadEffective 相同的覆盖合并,并补 CLI 测试;若刻意不支持,则应把 language 移出 overlayAllowedKeys 并在规则文档写明例外。",
          "status": "fixed",
          "basis": "独立核实目标提交 5631b18：language 在 overlayAllowedKeys 中；ConfiguredLanguage 只读 ConfigPath 字节并 Validate 后取 explicitConfigLanguage，不调用 readOverlay/mergeOverlayRaw。agent_language 走 Load 故覆盖生效。已在 explicitConfigLanguage 前套用与 loadEffective 相同的覆盖合并。",
          "fix_commit": "f7b31bc529958ac268d2709ffd7fd99000c92bfb",
          "verification": "go test -count=1 ./internal/config -run TestConfiguredLanguageUsesOverlay 与 go test -count=1 ./internal/menu -run TestConfigUsesOverlayLanguage 在 f7b31bc529958ac268d2709ffd7fd99000c92bfb 通过；同提交 go test -count=1 ./... 1265 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 24,
          "record_id": "qa-101-deferred-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-101",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:20.987831732Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
          "status": "deferred",
          "basis": "独立核实 5631b18 overlay.go:19-31 硬编码 overlayAllowedKeys（11 个顶层键），Validate（config.go）对未知顶层键静默忽略、无共享白名单。本卡验收要求覆盖未知键报错，但不要求与 Validate 共用常量；本修复轮按派发要求对非阻塞项不开修复。建议后续卡抽单一 topLevelKeys。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 25,
          "record_id": "qa-102-deferred-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-102",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.099606746Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
          "status": "deferred",
          "basis": "独立核实 5631b18：deepMerge 对 rules 键级合并；缺整节 rules 时 Validate 用 legacyRules() 全开（config.go:719-725），validateRules 以 DefaultRules(false) 为底（rules.go:69-82）。作用域无 rules、覆盖只写部分键时其余开关会落为 false。属本卡引入的真实边角，但为本轮指定的 disposition-only 非阻塞项，不开修复。建议后续在 merge 前物化 legacyRules()。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r1-c2",
            "epoch": 1
          },
          "submitted_revision": 26,
          "record_id": "qa-103-deferred-r1",
          "run_id": "qa-batch-one-r1",
          "finding_id": "QA-103",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T19:23:21.199963176Z",
          "report_hash": "196fe0c79c9c6d2972380195d4cf8e01d6df6a5d6ce09447bae37b90b13d229b",
          "original": "inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段,只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
          "status": "deferred",
          "basis": "独立核实 5631b18 overlay.go:118 overlayPathSafe 已 rejectLeafReparse（paths.go:39-50 Lstat+IsReparsePoint），inspectOverlayCandidate 随后 121-130 再做一次。非 Windows 第二次判定不增加防护。建议清理，本轮 disposition-only 不改。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "qa-batch-one-r2",
        "batch_id": "batch-one",
        "previous_run_id": "qa-batch-one-r1",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "QA",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "777b1f088985e3d11cec3bd9b4dde9271822119e",
        "reviewed_commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "50b3d4c9ec8c462e239e27d66c7d7c9714e2ce8e917707e3dad6ebab94f1b1ac",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "failed",
        "semantic_status": "unassessed",
        "failure_reason": "invalid structured review report: 审核证据无效：structured findings required; legacy mapping required for old reports",
        "exit_code": 1,
        "created_at": "2026-09-08T19:47:50.65870062Z",
        "finished_at": "2026-09-08T19:58:46.644586071Z",
        "duration_ms": 655985,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "88ae3239bceeec632e82b7337ecfdfc44e17a59f051c937043b3e106d13e26c1",
          "output.raw": "fa437698493d4a6e11b4e0fb0da7247edc326799a4dedb33f6cb8985c134a2a2",
          "prompt.txt": "9e3df36411e23efac72d04bf833563dabefd1401c02a7c1e5065d3d26f46bd38",
          "report.md": "d9a4443c25534c571167303ea2bd1bec2fb3d2e519f13ce1987ba0d7a042e542",
          "review-context.md": "50b3d4c9ec8c462e239e27d66c7d7c9714e2ce8e917707e3dad6ebab94f1b1ac",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "records": []
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "qa-batch-one-r3",
        "batch_id": "batch-one",
        "previous_run_id": "qa-batch-one-r1",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "QA",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "777b1f088985e3d11cec3bd9b4dde9271822119e",
        "reviewed_commit": "5631b184af859cd726f07bfa119e5378a5e3ded3",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "ae5c5c50fe845ee51c8e15e5d3d884bd4b1aa4b0b31294e17ca9b3ceb221347c",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T19:59:19.92483126Z",
        "finished_at": "2026-09-08T20:10:05.080209156Z",
        "duration_ms": 645155,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "88ae3239bceeec632e82b7337ecfdfc44e17a59f051c937043b3e106d13e26c1",
          "output.raw": "648159eefcc0471d349a289037f6a4c71ebde9ee64314318cd2c951dea946d4d",
          "prompt.txt": "7bdcfa6a8d507aa8ecab5d4261ffdb044212d298ceca9878cec6266403ff7550",
          "report.md": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "review-context.md": "ae5c5c50fe845ee51c8e15e5d3d884bd4b1aa4b0b31294e17ca9b3ceb221347c",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "QA-005",
            "tier": "medium",
            "text": "PM-09 的修复只覆盖了「作用域配置有显式 language 键」一条分支。internal/menu/options.go:153-158 中,ConfiguredScopeLanguage() 返回空时回落到 config.ResolveLanguage(),而 ResolveLanguage(internal/config/language.go:152-176)的第三优先级正是进程内绑定值 configLanguage,该值在会话构造之前已由 internal/tui/cmd.go:67 / internal/menu/commands.go:13 的 BindEffectiveLanguage() 设为合并覆盖后的语言(language.go:179-186 → 120-140)。explicitConfigLanguage(language.go:69-82)在缺 language 键时返回空,而「有效但无 language 键」是本仓库明确支持并有测试的作用域形态(internal/config/config_test.go:831-838)。于是:作用域 config.json 有效、welcome_complete 为 true、无 language 键,项目 .kander-config.json 为 {\"language\":\"ja\"} 时,用户在该目录下打开 TUI 选项面板并在任一分区提交,persistNow → Session.Save → SaveIfUnchanged(internal/config/loadsave.go:346-377)就会把 ja 整份写进作用域 config.json,删除覆盖文件后污染仍在。违反卡 2 USER_DECISIONS「所有写入路径以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与对应验收项,也与 internal/config/language.go:107-109、internal/menu/options.go:162-163 的注释及 rules/KANDER-AGENTS.md:19 的承诺矛盾。新增测试 internal/menu/overlay_cli_test.go:112-155 在作用域中显式写了 language=en,只走 stored != \"\" 分支,覆盖不到该回落分支。最小修复:else 分支先 config.BindConfigLanguage(nil) 再 ResolveLanguage()(紧随的 options.go:164 BindEffectiveLanguage() 会立即重新绑回合并语言,面板文案不受影响),或新增只走 --lang/环境的 ResolveScopeLanguage() 供写入路径使用;同时修正两处过度承诺的注释,并把隔离测试扩展一例「作用域无 language 键 + 覆盖有 language」。",
            "evidence": "internal/menu/options.go:153-158(ConfiguredScopeLanguage 为空时回落 ResolveLanguage);internal/config/language.go:69-82(缺 language 键即返回空);internal/config/language.go:110-116(ConfiguredScopeLanguage);internal/config/language.go:152-176(ResolveLanguage 第三优先级取绑定值);internal/config/language.go:179-186 与 120-140(BindEffectiveLanguage 绑定的是合并覆盖后的语言);internal/tui/cmd.go:67、internal/menu/commands.go:13(会话构造前已绑定);internal/menu/options.go:504 与 internal/config/loadsave.go:346-377(Session.Save → SaveIfUnchanged 整份写作用域文件);internal/config/config_test.go:831-838(无 language 键是受支持并有测试的作用域形态);测试盲区 internal/menu/overlay_cli_test.go:112-155(作用域显式 language=en);冲突注释与文档 internal/config/language.go:107-109、internal/menu/options.go:162-163、rules/KANDER-AGENTS.md:19"
          }
        ],
        "NON_BLOCKING": [
          {
            "id": "QA-101",
            "tier": "recommend",
            "text": "本轮未改动,现象未变。overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
            "evidence": "internal/config/overlay.go:19-31(硬编码 overlayForbiddenKeys / overlayAllowedKeys);internal/config/config.go:656(Validate 无顶层键白名单,未知键被忽略)",
            "lineage": {
              "run_id": "qa-batch-one-r1",
              "finding_id": "QA-101"
            }
          },
          {
            "id": "QA-102",
            "tier": "low",
            "text": "本轮未改动,现象未变。deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
            "evidence": "internal/config/overlay.go:56-70(deepMerge 键级合并);internal/config/rules.go:69-82(validateRules 以 DefaultRules(false) 为底并执行 ValidateRules 依赖检查);internal/config/config.go:720(缺 rules 节时用 legacyRules());AGENTS.md:49(旧配置缺整节时七开关全开)",
            "lineage": {
              "run_id": "qa-batch-one-r1",
              "finding_id": "QA-102"
            }
          },
          {
            "id": "QA-103",
            "tier": "suggest",
            "text": "本轮未改动,现象未变。inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段,只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
            "evidence": "internal/config/overlay.go:118(overlayPathSafe);internal/config/paths.go:39-50(rejectLeafReparse 已做 Lstat + IsReparsePoint);internal/config/overlay.go:121-130(重复的 Lstat + IsReparsePoint)",
            "lineage": {
              "run_id": "qa-batch-one-r1",
              "finding_id": "QA-103"
            }
          },
          {
            "id": "QA-104",
            "tier": "suggest",
            "text": "本轮未改动,现象未变。kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
            "evidence": "internal/config/format.go:138-142(分档分支拼接为 Text(\"config.review_stages\")+\" \"+summary);internal/config/format.go:112-118,124-136(其余行统一使用 \": \")",
            "lineage": {
              "run_id": "qa-batch-one-r1",
              "finding_id": "QA-104"
            }
          },
          {
            "id": "QA-105",
            "tier": "suggest",
            "text": "修复范围新增的覆盖侧 review_stages 规范化把 NormalizeReviewStages 的错误原样上抛,而这些文案只提到 review_stages、不含文件路径。项目提交的 .kander-config.json 写成 {\"review_stages\": {\"large\": {...}, \"PM\": \"auto\"}} 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键两类错误都是带绝对路径报错的,诊断力不一致。建议在 mergeOverlayRaw 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 mergeOverlayRaw,loadEffective 与 ConfiguredLanguage 两个调用点都已持有该路径)。",
            "evidence": "internal/config/overlay.go:209-212(覆盖侧 normalizeReviewStagesField 直接返回原错误);internal/config/review_stages.go:83-99(错误文案只含键名,不含文件路径);对照 internal/config/overlay.go:193-201(JSON 非法与根类型错误均带 path)、internal/config/overlay.go:85-103(禁止键/未知键均带 path)"
          }
        ]
      },
      "assignment": {
        "run_id": "qa-batch-one-r3",
        "batch_id": "batch-one",
        "author": "claude-orchestrator",
        "basis": "assigned by each card's GOAL/OUT_OF_SCOPE and the commits that introduced them: QA-005 (ResolveLanguage fallback in Session.prepare writes overlay language, card 2 commit 32ba6ee) and QA-105 (overlay-side review_stages normalization error lacks file path, card 2 commit c1e3851) plus carried QA-101/102/103 hit card 2; carried QA-104 hits card 1's kander config summary",
        "items": {
          "QA-005": [
            "20260909-project-config-overlay-task"
          ],
          "QA-101": [
            "20260909-project-config-overlay-task"
          ],
          "QA-102": [
            "20260909-project-config-overlay-task"
          ],
          "QA-103": [
            "20260909-project-config-overlay-task"
          ],
          "QA-104": [
            "20260909-review-stages-per-size-task"
          ],
          "QA-105": [
            "20260909-project-config-overlay-task"
          ]
        },
        "owners": {
          "20260909-project-config-overlay-task": "cursor",
          "20260909-review-stages-per-size-task": "cursor"
        },
        "recorded_at": "2026-09-08T20:11:10.201517709Z"
      },
      "records": [
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r3-c1",
            "epoch": 3
          },
          "submitted_revision": 39,
          "record_id": "qa-104-deferred-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-104",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:12:58.302710865Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "本轮未改动,现象未变。kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
          "status": "deferred",
          "basis": "已在未改交付 9a2badc1bdb23dd946020aaaa950501081dbbd5c 上独立复现: FormatConfigLines 在两档不同时输出「审核环节 大: PM=required CSA=auto Hacker=auto QA=auto」与「审核环节 小: PM=skip CSA=auto Hacker=auto QA=auto」。format.go:138-139 相同时用 Text+\": \",141-142 分档时用 Text+\" \"+summary,标签后无冒号。与上一轮 reviews/qa-batch-one-r1/dispositions/qa-104-deferred-r1.json 同一现象,本 sync 轮不改代码,维持 deferred。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r3-c2",
            "epoch": 4
          },
          "submitted_revision": 57,
          "record_id": "qa-005-fix-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-005",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:19:17.494488305Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "PM-09 的修复只覆盖了「作用域配置有显式 language 键」一条分支。internal/menu/options.go:153-158 中,ConfiguredScopeLanguage() 返回空时回落到 config.ResolveLanguage(),而 ResolveLanguage(internal/config/language.go:152-176)的第三优先级正是进程内绑定值 configLanguage,该值在会话构造之前已由 internal/tui/cmd.go:67 / internal/menu/commands.go:13 的 BindEffectiveLanguage() 设为合并覆盖后的语言(language.go:179-186 → 120-140)。explicitConfigLanguage(language.go:69-82)在缺 language 键时返回空,而「有效但无 language 键」是本仓库明确支持并有测试的作用域形态(internal/config/config_test.go:831-838)。于是:作用域 config.json 有效、welcome_complete 为 true、无 language 键,项目 .kander-config.json 为 {\"language\":\"ja\"} 时,用户在该目录下打开 TUI 选项面板并在任一分区提交,persistNow → Session.Save → SaveIfUnchanged(internal/config/loadsave.go:346-377)就会把 ja 整份写进作用域 config.json,删除覆盖文件后污染仍在。违反卡 2 USER_DECISIONS「所有写入路径以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与对应验收项,也与 internal/config/language.go:107-109、internal/menu/options.go:162-163 的注释及 rules/KANDER-AGENTS.md:19 的承诺矛盾。新增测试 internal/menu/overlay_cli_test.go:112-155 在作用域中显式写了 language=en,只走 stored != \"\" 分支,覆盖不到该回落分支。最小修复:else 分支先 config.BindConfigLanguage(nil) 再 ResolveLanguage()(紧随的 options.go:164 BindEffectiveLanguage() 会立即重新绑回合并语言,面板文案不受影响),或新增只走 --lang/环境的 ResolveScopeLanguage() 供写入路径使用;同时修正两处过度承诺的注释,并把隔离测试扩展一例「作用域无 language 键 + 覆盖有 language」。",
          "status": "fixed",
          "basis": "独立核实目标 777b1f088985e3d11cec3bd9b4dde9271822119e：ConfiguredScopeLanguage 在缺 language 键时返回空，Session.prepare 回落到 ResolveLanguage；cmd.go/commands.go 已先 BindEffectiveLanguage，绑定的是合并覆盖语言。已新增 ResolveScopeLanguage（只走 --lang/环境，忽略进程内绑定值）；prepare 的两条空回落均改用该函数；并修正 ConfiguredScopeLanguage 与 prepare 的注释，使其不再声称隔离已完成。一并核查写入路径：Update/Save/Repair 经 loadValidated 只读作用域；savePrefs/saveColumns 经 Update 读作用域；SetLanguageIfPresent 写调用方显式语言；FormatConfigLines 只读展示。未发现其他以合并后语言为基底的写入。",
          "fix_commit": "4832ee9db7912db15d297a32ee8888fff757bf4a",
          "verification": "go test -count=1 ./internal/config -run 'TestResolveScopeLanguageIgnoresBoundConfig' 与 go test -count=1 ./internal/menu -run 'TestSessionSaveLeavesOverlayLanguageIsolated' 在 4832ee9db7912db15d297a32ee8888fff757bf4a 通过；同提交 go build ./...、go vet ./...、go test -count=1 ./... 1269 passed。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r3-c2",
            "epoch": 4
          },
          "submitted_revision": 58,
          "record_id": "qa-105-deferred-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-105",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:19:22.353916363Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "修复范围新增的覆盖侧 review_stages 规范化把 NormalizeReviewStages 的错误原样上抛,而这些文案只提到 review_stages、不含文件路径。项目提交的 .kander-config.json 写成 {\"review_stages\": {\"large\": {...}, \"PM\": \"auto\"}} 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键两类错误都是带绝对路径报错的,诊断力不一致。建议在 mergeOverlayRaw 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 mergeOverlayRaw,loadEffective 与 ConfiguredLanguage 两个调用点都已持有该路径)。",
          "status": "deferred",
          "basis": "独立核实 777b1f0 overlay.go:209-212：覆盖侧 normalizeReviewStagesField 仍原样返回 NormalizeReviewStages 错误，文案只含键名不含覆盖文件路径；对照同文件 JSON 非法与禁止/未知键均带绝对路径。属真实 UX 不一致。本轮 disposition-only 非阻塞项，不开修复。建议后续把路径传入 mergeOverlayRaw 并包装错误。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r3-c2",
            "epoch": 4
          },
          "submitted_revision": 59,
          "record_id": "qa-101-deferred-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-101",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:19:22.506638788Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "本轮未改动,现象未变。overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r1/QA-101（记录 reviews/qa-batch-one-r1/dispositions/qa-101-deferred-r1.json）。独立核实 4832ee9：overlay.go:19-31 仍硬编码 overlayAllowedKeys（11 个顶层键），Validate（config.go）对未知顶层键静默忽略、无共享白名单。现象未变。本轮 disposition-only 不开修复。建议后续抽单一 topLevelKeys。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r3-c2",
            "epoch": 4
          },
          "submitted_revision": 60,
          "record_id": "qa-102-deferred-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-102",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:19:22.651393332Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "本轮未改动,现象未变。deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r1/QA-102（记录 reviews/qa-batch-one-r1/dispositions/qa-102-deferred-r1.json）。独立核实 4832ee9：deepMerge 仍对 rules 键级合并；缺整节 rules 时 Validate 用 legacyRules() 全开，validateRules 以 DefaultRules(false) 为底。作用域无 rules、覆盖只写部分键时其余开关会落为 false。现象未变。本轮 disposition-only 不开修复。"
        },
        {
          "authorization": {
            "dispatch_id": "fix-batch-one-r3-c2",
            "epoch": 4
          },
          "submitted_revision": 61,
          "record_id": "qa-103-deferred-r3",
          "run_id": "qa-batch-one-r3",
          "finding_id": "QA-103",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:19:22.781842839Z",
          "report_hash": "bda947562e9303b98458d1ddc16fa7efc5af5d627130ed3d3e21600c19a532d8",
          "original": "本轮未改动,现象未变。inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段,只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r1/QA-103（记录 reviews/qa-batch-one-r1/dispositions/qa-103-deferred-r1.json）。独立核实 4832ee9 overlay.go:118 overlayPathSafe 已 rejectLeafReparse（paths.go Lstat+IsReparsePoint），inspectOverlayCandidate 随后仍重复 Lstat + IsReparsePoint。非 Windows 第二次判定不增加防护。现象未变。本轮 disposition-only 不改。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260909-config-layering-group",
        "run_id": "qa-batch-one-r4",
        "batch_id": "batch-one",
        "previous_run_id": "qa-batch-one-r3",
        "task_ids": [
          "20260909-project-config-overlay-task",
          "20260909-review-stages-per-size-task"
        ],
        "role": "QA",
        "reviewer": "claude",
        "model": "opus",
        "effort": "high",
        "cwd": "/home/dualf/works/kander/worktrees/20260909-config-layering-group",
        "base": "7466cd6acfd3077bdf311c0bb0ca163566d37ba6",
        "commit": "4832ee9db7912db15d297a32ee8888fff757bf4a",
        "reviewed_commit": "777b1f088985e3d11cec3bd9b4dde9271822119e",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "95bea1f066e6c5ccc321aabf76007dc2f566e17486e6867c85e43a90e7e8e838",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "kander_version": "20260908T155319Z-7466cd6acfd3",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T20:22:31.564457669Z",
        "finished_at": "2026-09-08T20:28:21.678181755Z",
        "duration_ms": 350113,
        "hashes": {
          "error.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "evidence.txt": "91fe171e87f77484f8a999e473b35ebf9586be67158b7e01c824b52fc8d91467",
          "output.raw": "2598a7aebc710149c3d7218533c586a1602fd876223494fece725423b598b578",
          "prompt.txt": "0ff65705bc8f7c936e420ee5224fcde119af402ba3963ef4a9b5e3834ebd36da",
          "report.md": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "review-context.md": "95bea1f066e6c5ccc321aabf76007dc2f566e17486e6867c85e43a90e7e8e838",
          "stdout.log": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "27905b6fdfa87f264566eae7892fdeaeec6df5901498c7c7ffee01bc59b94493"
        },
        "published": {
          "20260909-project-config-overlay-task": true,
          "20260909-review-stages-per-size-task": true
        }
      },
      "findings": {
        "FINDINGS": [],
        "NON_BLOCKING": [
          {
            "id": "QA-101",
            "tier": "recommend",
            "text": "本轮未改动,现象未变。overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
            "evidence": "internal/config/overlay.go:19-31(硬编码 overlayAllowedKeys)、internal/config/overlay.go:92(据此拒绝未知键);internal/config/config.go:656(Validate 无顶层键白名单,未知键被忽略)",
            "lineage": {
              "run_id": "qa-batch-one-r3",
              "finding_id": "QA-101"
            }
          },
          {
            "id": "QA-102",
            "tier": "low",
            "text": "本轮未改动,现象未变。deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
            "evidence": "internal/config/overlay.go:58-70(deepMerge 键级合并);internal/config/rules.go:74(validateRules 以 DefaultRules(false) 为底并执行依赖检查);internal/config/config.go:720(缺 rules 节时用 legacyRules());AGENTS.md:49(旧配置缺整节时七开关全开)",
            "lineage": {
              "run_id": "qa-batch-one-r3",
              "finding_id": "QA-102"
            }
          },
          {
            "id": "QA-103",
            "tier": "suggest",
            "text": "本轮未改动,现象未变。inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段(overlay.go:128-130),只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
            "evidence": "internal/config/overlay.go:118(inspectOverlayCandidate 调用 overlayPathSafe)、internal/config/overlay.go:105-111(overlayPathSafe → rejectLeafReparse);internal/config/paths.go:39-46(rejectLeafReparse 已做 Lstat + IsReparsePoint);internal/config/overlay.go:124-130(重复的 Lstat + IsReparsePoint)",
            "lineage": {
              "run_id": "qa-batch-one-r3",
              "finding_id": "QA-103"
            }
          },
          {
            "id": "QA-104",
            "tier": "suggest",
            "text": "本轮未改动,现象未变。kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
            "evidence": "internal/config/format.go:141-143(分档分支拼接为 Text(\"config.review_stages\")+\" \"+summary);internal/config/format.go:138-139(折叠分支使用 \": \")、format.go:130,145,152(其余行统一使用 \": \")",
            "lineage": {
              "run_id": "qa-batch-one-r3",
              "finding_id": "QA-104"
            }
          },
          {
            "id": "QA-105",
            "tier": "suggest",
            "text": "本轮未改动,现象未变。mergeOverlayRaw 对覆盖副本调用 normalizeReviewStagesField 后把 NormalizeReviewStages 的错误原样上抛,而这些文案只提到 review_stages、不含文件路径。项目提交的 .kander-config.json 写成 {\"review_stages\": {\"large\": {...}, \"PM\": \"auto\"}} 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键两类错误都是带绝对路径报错的,诊断力不一致。建议在 mergeOverlayRaw 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 mergeOverlayRaw,loadEffective 与 ConfiguredLanguage 两个调用点都已持有该路径)。",
            "evidence": "internal/config/overlay.go:205-213(覆盖侧 normalizeReviewStagesField 直接返回原错误);internal/config/review_stages.go(错误文案只含键名,不含文件路径);对照 internal/config/overlay.go:193-201(JSON 非法与根类型错误均带 path)、internal/config/overlay.go:85-103(禁止键/未知键均带 path)",
            "lineage": {
              "run_id": "qa-batch-one-r3",
              "finding_id": "QA-105"
            }
          }
        ]
      },
      "assignment": {
        "run_id": "qa-batch-one-r4",
        "batch_id": "batch-one",
        "author": "claude-orchestrator",
        "basis": "carried non-blocking items keep their original attribution: QA-101/102/103/105 hit card 2 (overlay), QA-104 hits card 1 (kander config summary)",
        "items": {
          "QA-101": [
            "20260909-project-config-overlay-task"
          ],
          "QA-102": [
            "20260909-project-config-overlay-task"
          ],
          "QA-103": [
            "20260909-project-config-overlay-task"
          ],
          "QA-104": [
            "20260909-review-stages-per-size-task"
          ],
          "QA-105": [
            "20260909-project-config-overlay-task"
          ]
        },
        "owners": {
          "20260909-project-config-overlay-task": "cursor",
          "20260909-review-stages-per-size-task": "cursor"
        },
        "recorded_at": "2026-09-08T20:28:59.457725201Z"
      },
      "records": [
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r4-c1",
            "epoch": 4
          },
          "submitted_revision": 47,
          "record_id": "qa-104-deferred-r4",
          "run_id": "qa-batch-one-r4",
          "finding_id": "QA-104",
          "batch_id": "batch-one",
          "task_id": "20260909-review-stages-per-size-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:30:49.094607803Z",
          "report_hash": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "original": "本轮未改动,现象未变。kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。",
          "status": "deferred",
          "basis": "已在未改交付 9a2badc1bdb23dd946020aaaa950501081dbbd5c 上再次复现: FormatConfigLines 两档不同时输出「审核环节 大: PM=required CSA=auto Hacker=auto QA=auto」与「审核环节 小: PM=skip CSA=auto Hacker=auto QA=auto」。format.go:138-139 折叠用 \": \",141-143 分档用空格拼接,130/145/152 等其余行仍用 \": \"。与 reviews/qa-batch-one-r3/dispositions/qa-104-deferred-r3.json 同一现象,本 sync 轮不改代码,维持 deferred。"
        },
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r4-c2",
            "epoch": 5
          },
          "submitted_revision": 69,
          "record_id": "qa-101-deferred-r4",
          "run_id": "qa-batch-one-r4",
          "finding_id": "QA-101",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:30:50.127195415Z",
          "report_hash": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "original": "本轮未改动,现象未变。overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r3/QA-101（记录 reviews/qa-batch-one-r3/dispositions/qa-101-deferred-r3.json）。独立核实交付 4832ee9db7912db15d297a32ee8888fff757bf4a：overlay.go:19-31 仍硬编码 overlayAllowedKeys（11 个顶层键），overlay.go:92 据此拒绝未知键；Validate（config.go:656）对未知顶层键静默忽略、无共享白名单。现象未变。本轮 sync 仅处置、无代码改动。建议后续抽单一 topLevelKeys。"
        },
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r4-c2",
            "epoch": 5
          },
          "submitted_revision": 70,
          "record_id": "qa-102-deferred-r4",
          "run_id": "qa-batch-one-r4",
          "finding_id": "QA-102",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:30:50.276356776Z",
          "report_hash": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "original": "本轮未改动,现象未变。deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r3/QA-102（记录 reviews/qa-batch-one-r3/dispositions/qa-102-deferred-r3.json）。独立核实 4832ee9：deepMerge（overlay.go:58-70）仍对 rules 键级合并；缺整节 rules 时 Validate 用 legacyRules() 全开（config.go:720），validateRules 以 DefaultRules(false) 为底（rules.go:74）。作用域无 rules、覆盖只写部分键时其余开关会落为 false。现象未变。本轮 sync 仅处置、无代码改动。"
        },
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r4-c2",
            "epoch": 5
          },
          "submitted_revision": 71,
          "record_id": "qa-103-deferred-r4",
          "run_id": "qa-batch-one-r4",
          "finding_id": "QA-103",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:30:50.422757993Z",
          "report_hash": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "original": "本轮未改动,现象未变。inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段(overlay.go:128-130),只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r3/QA-103（记录 reviews/qa-batch-one-r3/dispositions/qa-103-deferred-r3.json）。独立核实 4832ee9：overlay.go:118 调用 overlayPathSafe（105-111 → rejectLeafReparse，paths.go:39-46 已 Lstat+IsReparsePoint），inspectOverlayCandidate 124-130 仍重复 Lstat + IsReparsePoint。非 Windows 第二次判定不增加防护。现象未变。本轮 sync 仅处置、无代码改动。"
        },
        {
          "authorization": {
            "dispatch_id": "sync-batch-one-r4-c2",
            "epoch": 5
          },
          "submitted_revision": 72,
          "record_id": "qa-105-deferred-r4",
          "run_id": "qa-batch-one-r4",
          "finding_id": "QA-105",
          "batch_id": "batch-one",
          "task_id": "20260909-project-config-overlay-task",
          "author": "cursor",
          "recorded_at": "2026-09-08T20:30:50.559228158Z",
          "report_hash": "ee2397a79304a0b858fc7b9586931155013e1825f59648743e71fe7248270f62",
          "original": "本轮未改动,现象未变。mergeOverlayRaw 对覆盖副本调用 normalizeReviewStagesField 后把 NormalizeReviewStages 的错误原样上抛,而这些文案只提到 review_stages、不含文件路径。项目提交的 .kander-config.json 写成 {\"review_stages\": {\"large\": {...}, \"PM\": \"auto\"}} 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键两类错误都是带绝对路径报错的,诊断力不一致。建议在 mergeOverlayRaw 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 mergeOverlayRaw,loadEffective 与 ConfiguredLanguage 两个调用点都已持有该路径)。",
          "status": "deferred",
          "basis": "延续 qa-batch-one-r3/QA-105（记录 reviews/qa-batch-one-r3/dispositions/qa-105-deferred-r3.json）。独立核实 4832ee9 overlay.go:205-213：覆盖侧 normalizeReviewStagesField 仍原样返回 NormalizeReviewStages 错误；review_stages.go 文案只含键名不含覆盖文件路径；对照同文件 JSON 非法与禁止/未知键均带绝对路径。现象未变。本轮 sync 仅处置、无代码改动。"
        }
      ]
    }
  ]
}


Caller supplemental context (verbatim; does not replace originals):
Fixes included this round (PM 视角,PM-09、PM-10 的修复 32ba6ee、777b1f0 与其后的 4832ee9 均为本轮新增材料): 第三修复轮——卡 2 4832ee9(作用域无 language 键时,选项面板保存不再把覆盖 language 经 ResolveLanguage 回落写入作用域;补隔离测试,QA-005)。此前两轮:卡 1 baa7b35/9a2badc(QA-004/PM-03、PM-04);卡 2 c1e3851/fa8ab21/5d632af(QA-001/PM-02、QA-002/PM-01、QA-003)、32ba6ee/777b1f0(PM-09、PM-10)。非阻塞项处置:卡 1 QA-104/PM-08 deferred;卡 2 QA-101/102/103/105、PM-06/07/11 deferred,PM-05 rejected。各处置原件由工具随本次 run 自动附带。本批次已用完三轮修复上限。
Review focus: (1) 逐条核实上一轮 FINDINGS 是否按处置记录真实关闭;(2) 只审 reviewed-commit..4832ee9 的新增改动是否引入、加重或掩盖问题,或破坏触及的需求;(3) 未改动代码视为已接受,不重新审计。
Verification records: 编排者在组工作树 HEAD 4832ee9db7912db15d297a32ee8888fff757bf4a 运行 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部通过,工作树干净。
Implementation note: 修复由各卡执行 Agent(cursor)在任务分支完成并 rebase 到组分支;编排者未修改代码。
OUTPUT CONTRACT (critical): only your FINAL message is captured as the report. Your final message must be the COMPLETE report and must END with one standalone fenced block ```kander-findings containing a JSON object with both FINDINGS and NON_BLOCKING arrays (each item: id, tier, text, evidence; carried-forward items use lineage {run_id, finding_id}). Do NOT send a separate closing summary after the report; a final message without that block is rejected by the tool and the run is recorded as failed.