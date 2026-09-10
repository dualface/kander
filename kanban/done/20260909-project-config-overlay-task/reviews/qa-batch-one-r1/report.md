# QA 审核报告

**Role**: QA
**Commit**: `5631b184af859cd726f07bfa119e5378a5e3ded3`(范围 `7466cd6a..5631b184`)
**Task Context**: 任务组 `20260909-config-layering-group` batch-one 两张卡 —— `20260909-review-stages-per-size-task`(`review_stages` 按 large/small 分档)与 `20260909-project-config-overlay-task`(项目 `.kander-config.json` 覆盖作用域配置)。

**Reviewed Scope**:
- 只读检查了 `/home/dualf/works/kander/worktrees/20260909-config-layering-group` 的工作树内容与 evidence 中的 COMMIT TREE / PATCH;未执行任何命令写入,未运行构建与测试(编排者在该 HEAD 记录的 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全通过,按指示视为证据)。
- 重点走查:`internal/config/{review_stages,overlay,loadsave,repair,format,paths,language,rules}.go`、`internal/flow/flow.go`、`internal/menu/{options,commands,doctor,doctor_repair}.go`、`internal/tui/{options_form,options_panel,options_doctor,options_view,prefs,cmd,app}.go`、三语 i18n、`AGENTS.md`、`docs/custom-agents.md`、`rules/KANDER-{AGENTS,KANBAN-RULES,REVIEW-RULES}.md`,并沿消费者链核对 `config.Load` / `config.LoadScope` 的全部生产调用点。
- 文件行数硬规则:范围内涉及的非生成文件最大为 `internal/config/config.go` 822 行、`internal/tui/options_form.go` 741 行,均未越过 1000 行,不触发该规则。

## 行为 / 质量核对

| 需求 / 行为 | 证据 | 结论 |
| --- | --- | --- |
| 两档 `review_stages` 校验、旧平铺兼容、混合形态报错含冲突键 | `internal/config/review_stages.go:79-135`、`review_stages_test.go:44-72` | 通过(Observed) |
| `NormalizeReviewStages` 导出且深拷贝原始对象 | `review_stages.go:79-114`、`review_stages_test.go:139-171` | 通过(Observed) |
| `ReviewStageFor` 替换全部平铺读取 | 全仓 `grep ReviewStages`:`internal/{menu,flow,tui}` 生产代码已无 `cfg.ReviewStages[role]` | 通过(Observed) |
| `Save` 往返写出两档 | `review_stages_test.go:196-249` | 通过(Observed) |
| `kander config` 摘要按档展示 / 相同时折叠 | `format.go:87-145`、`review_stages_test.go:212-244` | 通过(Observed) |
| doctor 缺档回填 | `repair.go:141-167` | **不完全**(见 QA-004) |
| TUI 每角色按档提供阶段选择并保存 | `options_form.go:560-573`、`options_form.go:647-661`、`options_test.go` | 通过(Observed) |
| `OverlayPath` Git 主工作树定位 + 非 Git 向上查找 | `overlay.go:136-172`、`overlay_test.go:73-152` | 通过(Observed) |
| 深合并语义(对象递归、标量/数组整体替换) | `overlay.go:47-70`、`overlay_test.go:26-72` | 通过(Observed) |
| 合并前对**作用域** `review_stages` 规范化 | `overlay.go:72-83,205-210` | 通过,但覆盖侧未规范化(见 QA-001) |
| 禁止键 / 未知键报错含路径与键名 | `overlay.go:85-103`、`overlay_test.go:161-190` | 通过(Observed) |
| symlink / 非常规文件防护复用 | `overlay.go:105-135`、`overlay_test.go:191-225` | 通过(Observed) |
| `Save`/`Update`/`SaveIfUnchanged`、doctor Repair、选项面板以 `LoadScope` 为基底 | `loadsave.go:127-133,281-302`、`repair.go:37-59`、`options_panel.go:125`、`options_doctor.go:63-65` | 通过 |
| TUI 全部写入路径不反向污染作用域配置 | `internal/tui/prefs.go:20-35,62-66` | **未满足**(见 QA-002) |
| `kander config` 覆盖路径输出 + TUI 提示行 | `format.go:112-118`、`options_panel.go:106-113`、`options_view.go:135-141` | 通过(Observed) |
| 文档同步(REVIEW-RULES 第 3 级、AGENTS 路径表与已接受风险、AGENTS.md、docs) | `rules/KANDER-REVIEW-RULES.md:330,338`、`rules/KANDER-AGENTS.md:14-20`、`AGENTS.md:28,51`、`docs/custom-agents.md:5,74-83` | 通过,但两处与实现不符(并入 QA-001 / QA-002) |
| 三语 i18n 键齐全 | `internal/i18n/locales/{en,ja,zh-CN}.json` 新增 12 个键一一对齐 | 通过(Observed) |

---

## 主要发现(Gate Findings)

### QA-001 — high — 覆盖文件使用旧平铺 `review_stages` 会让该项目下所有读配置的命令直接报错

**声明类型**:Observed

**证据**
- `internal/config/overlay.go:205-210`:`mergeOverlayRaw` 只对 `scope` 调用 `normalizeScopeReviewStages`,`overlay` 原样进入 `deepMerge`。
- `internal/config/overlay.go:47-70`:`deepMerge` 对 `review_stages` 做键级合并,不做形态归一。
- `internal/config/review_stages.go:88-94`:同层出现档名与角色即返回 `config.review_stages_mixes_scale_and_role_keys`。
- `internal/config/loadsave.go:112-116`:`Load` 用合并结果调 `Validate`,失败即整体返回错误。
- 契约文档明确承诺旧平铺仍合法:`docs/custom-agents.md:83`、`rules/KANDER-REVIEW-RULES.md:330`、`AGENTS.md:51`(均无「覆盖文件除外」的限定)。

**可达失败场景**
项目提交 `.kander-config.json` 内容为 `{"review_stages": {"PM": "required"}}`(文档说明的合法旧平铺形态)。作用域 `config.json` 由 `Save` / doctor / TUI 写出,必然含两档 `review_stages`;即使作用域文件不存在,`loadScopeRawAt`(`loadsave.go:66-79`)也会用 `DefaultConfig()` 编码出两档结构。合并结果为 `{"large":{…},"small":{…},"PM":"required"}` → 混合形态 → `Load` 报 `review_stages 同时含档名与角色: PM, large, small`。

**影响**
该项目目录下 `kander config`、`config --json`、`doctor`(非修复读取)、`start` / `resume` / `notify` / `review` 等所有经 `config.Load` 的入口全部失败,错误信息指向 `review_stages` 而非覆盖文件,用户无从判断是覆盖文件的形态问题。这正是本卡 DISCUSSION 要消除的「合并产生混合形态」跨卡缺口,只是修在了单侧。

**最小修复**
在 `mergeOverlayRaw` 中对 `overlay` 也执行同一规范化(把 `normalizeScopeReviewStages(scope)` 扩展为对两份原始 map 各调一次),并补一条「作用域两档 + 覆盖平铺」的合并用例到 `internal/config/overlay_test.go`。

---

### QA-002 — high — TUI 界面偏好的写回路径以合并后的配置为基底,把覆盖文件独有的 `tui.*` 值写进作用域 `config.json`

**声明类型**:Observed

**证据**
- `internal/tui/prefs.go:20-25`:`loadPrefs()` 使用 `config.Load(true)`(已合并覆盖)读取 `cfg.TUI`。
- `internal/tui/cmd.go:69,104`:该合并值直接构造 `App`(`Theme` / `Columns` / `RefreshSecs` / `Single`)。
- `internal/tui/options_panel.go:537-551` `persistUI()`:用 `p.app.*` 现值组装 `uiPrefs` 后调用 `savePrefs`。
- `internal/tui/prefs.go:29-35` `savePrefs()`:`config.Update(func(cfg){ cfg.TUI = value })`,整节覆写作用域 `config.json`。
- `internal/tui/prefs.go:62-66` `saveColumns()`:同样先 `loadPrefs()`(合并)再整节写回;经 `internal/tui/app.go:150` 挂在列数调整上。
- 与该行为直接冲突的文档:`rules/KANDER-AGENTS.md:19`「Writes … only update the scope `config.json`. Overlay values are never written back.」

**可达失败场景**
项目 `.kander-config.json` 写 `{"tui": {"theme": "dark"}}`,作用域配置为 `theme: "auto"`。用户在该项目下打开 TUI(显示 dark),按键调整列数或在选项面板改刷新间隔 → `saveColumns` / `persistUI` 把整个 `TUI` 结构(含 `theme: "dark"`)写入作用域 `config.json`。此后即便删除覆盖文件,用户全局配置的主题也已被永久改成项目值。

**影响**
违反卡 2 的 USER_DECISIONS「所有写入路径……以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与验收项「TUI 保存后作用域 `config.json` 中不含覆盖文件独有的值」,并直接损害 THREAT_MODEL 列出的头号资产「用户作用域配置的完整性」。新增的 `internal/tui/options_test.go:TestOptionsSaveLeavesOverlayIsolated` 只断言 `kanban_agent`,覆盖不到 `tui` 这条路径,所以测试全绿。

**最小修复**
`savePrefs` 内部改为在 `config.Update` 的回调里逐字段写入本次真正被用户修改的项(或让 `loadPrefs` 保留一份 `LoadScope` 基线供写回使用),使写回基底始终是未合并的作用域值;并把上述测试扩展为覆盖文件含 `tui` 键后调用 `saveColumns` / `persistUI`,断言作用域 `theme` 不变。

---

### QA-003 — medium — 覆盖文件的 `language` 键被接受并出现在 `kander config --json`,但对命令行界面语言完全无效

**声明类型**:Observed

**证据**
- `internal/config/overlay.go:26-38`:`language` 在 `overlayAllowedKeys` 中,覆盖文件可合法设置。
- `internal/config/language.go:85-106` `ConfiguredLanguage()`:自行 `ConfigPath()` + `readConfigBytes()` 直读作用域文件,完全绕过 `Load` 与覆盖合并。
- `internal/config/language.go:144-152` `BindEffectiveLanguage()` 基于 `ConfiguredLanguage()`,而 `Text()`(`language.go:158-161`)是全部 CLI / TUI 文案的出口;`internal/menu/commands.go:12-14,61` 在每条命令开头调用它。
- 对照 `internal/board/cmd.go:71-77`、`internal/launch/prompts.go:68-74`:`agent_language` 走的是 `config.Load(false)`,确实生效 —— 同一节配置的两个键行为不一致。
- 与之冲突的文档:`rules/KANDER-AGENTS.md:18`「Runtime priority is project overlay > scope config > defaults」,未列例外;`AGENTS.md:50` 说明 `language` 决定 kander 自身界面语言。

**可达失败场景**
项目提交 `{"language": "ja"}`。`kander config --json` 输出 `"language": "ja"`,`kander config` 摘要的语言行也显示 ja,但同一条命令的所有标签、错误信息、doctor 报告仍用作用域语言输出。用户据 `--json` 判断配置已生效,实际行为不符。

**影响**
`kander config --json` 的输出契约(EXPECTED_OUTCOME:「输出合并结果」)与实际运行时行为分叉;文档承诺的优先级对该键不成立。

**最小修复**
让 `ConfiguredLanguage()` 走合并后的读取入口(读到原始作用域对象后套用与 `loadEffective` 相同的覆盖合并,再取 `explicitConfigLanguage`),并补一条「覆盖文件设 `language` → `kander config` 人类可读输出切换语言」的 CLI 测试到 `internal/menu/overlay_cli_test.go`。若刻意不支持,则应把 `language` 移出 `overlayAllowedKeys` 并在 `rules/KANDER-AGENTS.md:18` 写明该例外。

---

### QA-004 — medium — `doctor` 的缺档回填因就地修改原始 map 而被变更检测吞掉,结果不落盘

**声明类型**:Observed

**证据**
- `internal/config/config.go:638-641` `asObject` 返回的是同一个 map,不做拷贝。
- `internal/config/repair.go:100`:`provided, _ := asObject(raw)` —— `provided` 与 `repairAt` 持有的 `raw` 是同一对象。
- `internal/config/repair.go:141` → `149-167` `fillMissingReviewStageScales(provided)`:`provided["review_stages"] = normalized` 就地改写了 `raw`。
- `internal/config/repair.go:74`:`if exists && reflect.DeepEqual(raw, normalized) { return cfg, result, nil }` —— 比较的 `raw` 已经被回填污染,与修复后编码出的 `normalized` 变得相等,于是既不备份也不写盘,`result.Changed` 为 `false`。

**可达失败场景**
取一份由 `Save` 写出的完整 `config.json`(所有键齐全),手工删除 `review_stages` 里整个 `"small"` 块,`"large"` 保留四个角色。运行 `kander doctor`:`fillMissingReviewStageScales` 把 `raw` 的 `review_stages` 补成两档;`cfg.ReviewStages` 也是同一内容;其余字段本就与规范化输出一致 → `DeepEqual` 成立 → 直接 return。doctor 打印 `menu.config_json_is_unchanged`,文件里依然只有 `large`。

**影响**
`Repair` 返回的 `cfg` 中 `small` 是 `large` 的副本,而磁盘上没有 `small`,下一次 `Load` 得到的 `small` 是全 `auto` 默认值 —— doctor 的报告与后续实际生效值不一致,卡 1 验收项「缺失一档时从另一档回填」在这一情形下未真正生效。现有 `repair_test.go:125-150` 用的是缺失多个可选节的配置,`DeepEqual` 必然为假,因此覆盖不到该分支。

**最小修复**
在 `repairValues`(`repair.go:100`)取得 `provided` 后先 `provided = cloneRawObjectDeep(provided)`,使 `fillMissingReviewStageScales` 与 `recoverConfigFields` 都不再触碰 `repairAt` 用于变更检测的 `raw`;并在 `repair_test.go` 增补「其余字段完整、仅缺 `small` 档」的用例,断言 `result.Changed` 为真且磁盘文件含两档。

---

## NON-BLOCKING

### QA-101 — recommend — `overlayAllowedKeys` 手工复制了顶层 schema 键集,新增配置键时覆盖文件会误报未知键
`internal/config/overlay.go:19-38` 硬编码了 11 个允许键,而 `Validate`(`internal/config/config.go:656-770`)本身并无键白名单、对未知键静默忽略,两处没有任何共享来源。后续为 `config.json` 新增一个顶层键时,若忘记同步这张表,覆盖文件里使用该键会被 `config.overlay_has_unknown_keys` 拒绝,且错误只提示「未知键」,排查方向具有误导性。改法:把允许键集合从 `Config` 结构体的 json tag 反射生成(或在 `config.go` 中定义单一 `topLevelKeys` 常量供两处引用),禁止键仍单独维护。

### QA-102 — low — 作用域配置无 `rules` 节时,覆盖文件写入 `rules` 的任一子键会把其余六个模块变为关闭
`internal/config/overlay.go:47-70` 的 `deepMerge` 对 `rules` 做键级合并;当作用域是「合法旧配置、整节缺失 `rules`」时(`AGENTS.md:49` 明确这种配置七个开关全开),合并后 `rules` 节从「不存在」变成「只有覆盖写的那一个键」,`validateRules`(`internal/config/rules.go:69-82`)以 `DefaultRules(false)` 为底,其余六项落为 `false`。若覆盖写的是 `{"task_groups": true}`,还会因 `ValidateRules` 的 task_groups⇒git 依赖直接让 `Load` 报错。改法:在 `mergeOverlayRaw` 中,当 `scope` 无 `rules` 键而 `overlay` 有时,先把 `legacyRules()` 的完整值物化进 `scope` 再合并。

### QA-103 — suggest — `inspectOverlayCandidate` 对同一路径做了两次 reparse 判定
`internal/config/overlay.go:118` 调用的 `overlayPathSafe` → `rejectLeafReparse`(`internal/config/paths.go:39-50`)内部已经做了 `os.Lstat` + `fs.IsReparsePoint`;紧接着 `overlay.go:121-130` 又做了一次 `os.Lstat` + `fs.IsReparsePoint`。第二次判定在非 Windows 分支上永远不会命中(能走到这里说明第一次已判定为非 reparse),读代码时容易误以为两者防的是不同东西。改法:去掉 `overlay.go:128-130` 这段,只保留 `Lstat` 结果用于 `IsRegular` 判断,Windows 分支的逐段校验由 `overlayPathSafe` 保证。

### QA-104 — suggest — 两档不同时的 `kander config` 摘要行缺少冒号,与其余各行格式不一致
`internal/config/format.go:137-144`:折叠时输出 `审核环节: PM=…`,不折叠时输出 `审核环节 大: PM=…`(标签后无冒号,冒号跑到了档名后面)。`FormatConfigLines` 中其他每一行都是 `标签 + ": " + 值`(如 `format.go:112-118,124-136`),脚本按 `标签:` 前缀切分输出时会漏掉分档形态。改法:分档时改为 `Text("config.review_stages")+": "+summary`,由 `FormatReviewStagesSummary` 返回的 `大: …` / `小: …` 承担档名前缀。

（无被裁剪的候选项。）

```kander-findings
{"FINDINGS":[{"id":"QA-001","tier":"high","text":"覆盖文件使用文档承诺仍然合法的旧平铺 review_stages 形态时,mergeOverlayRaw 只规范化作用域一侧,深合并产生「档名与角色同层」的混合形态,Validate 直接报错,导致该项目目录下所有经 config.Load 的命令(kander config / config --json / doctor / start / resume / notify / review)全部失败,且错误指向 review_stages 而非覆盖文件。触发条件几乎必然成立:作用域 config.json 由 Save/doctor/TUI 写出时必含两档结构,作用域文件缺失时 loadScopeRawAt 也会用 DefaultConfig() 编码出两档。最小修复:在 mergeOverlayRaw 中对 overlay 原始 JSON 同样调用 NormalizeReviewStages,并补「作用域两档 + 覆盖平铺」的合并用例。","evidence":"internal/config/overlay.go:205-210 (mergeOverlayRaw 仅 normalizeScopeReviewStages(scope));internal/config/overlay.go:47-70 (deepMerge 不做形态归一);internal/config/review_stages.go:88-94 (混合形态报 config.review_stages_mixes_scale_and_role_keys);internal/config/loadsave.go:112-116 (Load 用合并结果 Validate);internal/config/loadsave.go:66-79 (作用域缺失时以 DefaultConfig 两档结构作为基底);契约文档承诺旧平铺合法:docs/custom-agents.md:83、rules/KANDER-REVIEW-RULES.md:330、AGENTS.md:51"},{"id":"QA-002","tier":"high","text":"TUI 的界面偏好写回路径没有改用 LoadScope:loadPrefs 用 config.Load(true) 读到的是合并后的值,App 以此初始化,persistUI 与 saveColumns 再把 p.app 现值整节写回 config.Update(cfg.TUI = value)。当项目 .kander-config.json 设置任一 tui.* 键(如 theme=dark)时,用户在该项目下调整列数或刷新间隔,就会把覆盖文件独有的 theme 永久写入作用域 config.json,删除覆盖文件后污染仍在。这违反卡 2 的 USER_DECISIONS「所有写入路径以未合并的作用域配置为读-改-写基底,覆盖值不得反向写入」与对应验收项,也与 rules/KANDER-AGENTS.md:19 的文字承诺矛盾。新增测试只断言 kanban_agent,覆盖不到该路径。最小修复:savePrefs 在 config.Update 回调内只写本次实际修改的字段,或让 loadPrefs 额外保留 LoadScope 基线供写回使用;并补覆盖文件含 tui 键时的隔离测试。","evidence":"internal/tui/prefs.go:20-25 (loadPrefs 用 config.Load(true));internal/tui/cmd.go:69,104 (合并值构造 App);internal/tui/options_panel.go:537-551 (persistUI 从 p.app 组装并 savePrefs);internal/tui/prefs.go:29-35 (savePrefs 整节 cfg.TUI = value 写作用域);internal/tui/prefs.go:62-66 与 internal/tui/app.go:150 (saveColumns 同样先 loadPrefs 合并再整节写回);冲突文档 rules/KANDER-AGENTS.md:19;未覆盖该路径的测试 internal/tui/options_test.go:TestOptionsSaveLeavesOverlayIsolated"},{"id":"QA-003","tier":"medium","text":"覆盖文件的 language 键在 overlayAllowedKeys 中被接受、会出现在 kander config --json 与摘要输出里,但决定 CLI/TUI 实际界面语言的 ConfiguredLanguage() 自行直读作用域 config.json 字节,绕过覆盖合并,因此覆盖值对所有 Text() 输出完全无效。同一节的 agent_language 走 config.Load 因而生效,行为不一致。这与 rules/KANDER-AGENTS.md:18 声明的「项目覆盖 > 作用域配置 > 默认值」以及 EXPECTED_OUTCOME「config.Load 及其等价读取入口返回合并后的生效配置」冲突。最小修复:让 ConfiguredLanguage() 在取 explicitConfigLanguage 前套用与 loadEffective 相同的覆盖合并,并补 CLI 测试;若刻意不支持,则应把 language 移出 overlayAllowedKeys 并在规则文档写明例外。","evidence":"internal/config/overlay.go:26-38 (language 在 overlayAllowedKeys);internal/config/language.go:85-106 (ConfiguredLanguage 直读 ConfigPath + readConfigBytes);internal/config/language.go:144-152,158-161 (BindEffectiveLanguage/Text 全部基于该值);internal/menu/commands.go:12-14,61 (每条命令开头调用);对照 internal/board/cmd.go:71-77 与 internal/launch/prompts.go:68-74 (agent_language 走 config.Load 生效);冲突文档 rules/KANDER-AGENTS.md:18、AGENTS.md:50"},{"id":"QA-004","tier":"medium","text":"repairValues 通过 asObject(raw) 拿到的 provided 与 repairAt 用于变更检测的 raw 是同一个 map,fillMissingReviewStageScales 就地写入 provided[\"review_stages\"] 因而污染了 raw。对于其余字段本就与规范化输出一致的配置(例如把 Save 写出的完整 config.json 手工删掉整个 \"small\" 块),repairAt 的 reflect.DeepEqual(raw, normalized) 会因回填后的 raw 而成立,doctor 既不备份也不写盘、报告 config_json_is_unchanged,磁盘上仍只有 large 档。结果是 Repair 返回的 cfg 里 small 是 large 的副本,而下一次 Load 得到的 small 是全 auto 默认值,与 doctor 的报告不一致,卡 1「缺失一档从另一档回填」的验收在该情形下未真正生效;现有 repair_test 用例的配置缺失多个可选节,DeepEqual 必为假,覆盖不到该分支。最小修复:在 repair.go:100 取得 provided 后先 cloneRawObjectDeep,使回填不再触碰 raw,并补「其余字段完整、仅缺 small 档」的用例断言 result.Changed 与磁盘内容。","evidence":"internal/config/config.go:638-641 (asObject 返回同一 map);internal/config/repair.go:100 (provided 与 raw 同源);internal/config/repair.go:141,149-167 (fillMissingReviewStageScales 就地写回 provided[\"review_stages\"]);internal/config/repair.go:74 (reflect.DeepEqual(raw, normalized) 决定是否写盘);internal/config/repair_test.go:125-150 (现有用例缺失多个可选节,触达不到该分支)"}],"NON_BLOCKING":[{"id":"QA-101","tier":"recommend","text":"overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。","evidence":"internal/config/overlay.go:19-38 (硬编码 overlayForbiddenKeys / overlayAllowedKeys);internal/config/config.go:656-770 (Validate 无顶层键白名单,未知键被忽略)"},{"id":"QA-102","tier":"low","text":"deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。","evidence":"internal/config/overlay.go:47-70 (deepMerge 键级合并);internal/config/rules.go:69-82 (validateRules 以 DefaultRules(false) 为底并执行 ValidateRules 依赖检查);internal/config/config.go:719-725 (缺 rules 节时用 legacyRules());AGENTS.md:49 (旧配置缺整节时七开关全开)"},{"id":"QA-103","tier":"suggest","text":"inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段,只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。","evidence":"internal/config/overlay.go:118 (overlayPathSafe);internal/config/paths.go:39-50 (rejectLeafReparse 已做 Lstat + IsReparsePoint);internal/config/overlay.go:121-130 (重复的 Lstat + IsReparsePoint)"},{"id":"QA-104","tier":"suggest","text":"kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。","evidence":"internal/config/format.go:137-144 (分档分支拼接为 Text(\"config.review_stages\")+\" \"+summary);internal/config/format.go:112-118,124-136 (其余行统一使用 \": \")"}]}
```

（本次为只读审核,未修改任何文件;`/tmp/claude-review.6d7f22b132f5a838336134cd0e3acb9f/prompt.txt` 按只读约束未删除,不影响结论。）