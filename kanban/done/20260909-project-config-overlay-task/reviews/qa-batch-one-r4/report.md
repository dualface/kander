# QA 审核报告

**Role**: QA
**Commit**: `4832ee9db7912db15d297a32ee8888fff757bf4a`(增量复审;本角色上一轮审核提交为 `777b1f088985e3d11cec3bd9b4dde9271822119e`,新增材料为修复范围 `777b1f0..4832ee9`)
**Task Context**: 任务组 `20260909-config-layering-group` batch-one 两张卡 —— `20260909-review-stages-per-size-task`(`review_stages` 按 large/small 分档)与 `20260909-project-config-overlay-task`(项目 `.kander-config.json` 覆盖作用域配置)。

**Reviewed Scope**:
- 修复范围为单个提交 `4832ee9`(*fix: keep overlay language out of scope when the key is omitted*),改动 4 个文件:`internal/config/language.go`、`internal/config/language_test.go`、`internal/menu/options.go`、`internal/menu/overlay_cli_test.go`(`git diff --stat 777b1f0..4832ee9`:113 insertions / 23 deletions)。
- 逐条核实上一轮 QA-005 与 QA-101/102/103/104/105 在 `4832ee9` 上的实际状态。
- 因 QA-005 的契约是「覆盖值不得反向写入作用域」,沿 `language` 的全部写入链重走一遍以判断修复是否完整:`menu.Session.prepare`+`Session.Save` → `SaveIfUnchanged`、`config.Repair`/`repairValues`、`config.SetLanguageIfPresent`(安装向导)、`install.Perform`/`runWizard`。
- `777b1f0` 以前未改动的代码按指示视为已接受,不重新审计;仅在判断修复影响时作为上下文引用。
- 本次为只读审核,**未执行任何构建或测试命令**;编排者在 `4832ee9` 记录的 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全通过(1269 passed)按指示作为证据采信,我未发现与之矛盾的代码事实。审核过程中未修改仓库,`git status --porcelain` 为空、HEAD 仍为 `4832ee9`。
- 文件行数硬规则:修复范围内改动的非生成文件为 `internal/menu/options.go` 595 行、`internal/menu/overlay_cli_test.go` 243 行、`internal/config/language.go` 204 行、`internal/config/language_test.go` 96 行,均远低于 1000 行,不触发该规则。

---

## 一、上一轮发现的逐条核实

| ID | 作者处置 | 本轮结论 | 关键证据(`4832ee9`) |
| --- | --- | --- | --- |
| QA-005 | fixed(`4832ee9`) | **已关闭** | `internal/config/language.go:163-170` 新增 `ResolveScopeLanguage()`,只经 `CLILanguage()`(`language.go:54-66`,`--lang`/`KANDER_LANG_CLI`)与 `localeLanguage()`(`language.go:141-151`),**不读进程内绑定值** `configLanguage`;`internal/menu/options.go:154-160` 的两条空回落分支全部改用该函数,`BindEffectiveLanguage()` 仍在其后(`options.go:165`)只影响面板文案 |
| QA-101 | deferred | **仍开放**(现象未变) | `internal/config/overlay.go:19-31` 仍硬编码 11 个允许键;`internal/config/config.go:656` `Validate` 仍无顶层键白名单 |
| QA-102 | deferred | **仍开放**(现象未变) | `internal/config/overlay.go:58-70` `deepMerge` 仍对 `rules` 键级合并;`internal/config/rules.go:74` 仍以 `DefaultRules(false)` 为底;`internal/config/config.go:720` 缺整节时用 `legacyRules()` |
| QA-103 | deferred | **仍开放**(现象未变) | `internal/config/overlay.go:118` `overlayPathSafe` → `internal/config/paths.go:39-46` 已 `Lstat`+`IsReparsePoint`,`overlay.go:124-130` 仍重复一次 |
| QA-104 | deferred | **仍开放**(现象未变) | `internal/config/format.go:138-139` 折叠时 `Text("config.review_stages")+": "`,`format.go:141-143` 分档时仍是 `Text("config.review_stages")+" "+summary`,标签后无冒号 |
| QA-105 | deferred | **仍开放**(现象未变) | `internal/config/overlay.go:205-213` `mergeOverlayRaw` 仍把覆盖侧 `normalizeReviewStagesField` 的错误原样上抛,文案不含覆盖文件路径 |

### QA-005 关闭的判定依据(不是形式关闭)

1. **根因消除点明确**:上一轮的失败链是 `ConfiguredScopeLanguage()==""` → `ResolveLanguage()` → 第三优先级取 `configLanguage`,而该值已被 `internal/tui/cmd.go:67` / `internal/menu/commands.go:12-14` 的 `BindEffectiveLanguage()` 设为**合并覆盖后**的语言。新的 `ResolveScopeLanguage()` 在 `language.go:166-171` 直接跳过 `configLanguage`,该链路断开。
2. **`ResolveLanguage` 的重构是行为等价的**:旧实现依次判 `cliLanguageOverride` → `os.Getenv(EnvLangCLI)!=""` 时的 `EnvLang` → `bound` → locale(`git show 7466cd6:internal/config/language.go`);新实现 `language.go:174-185` 的 `CLILanguage()` 恰好封装了前两步(`language.go:54-66`),其后 `bound` 与 `localeLanguage()` 顺序不变。抽出的 `localeLanguage()`(`language.go:141-151`)与原尾部逻辑逐行相同。所有 `ResolveLanguage` 的读展示调用点(`internal/config/format.go:149`、`internal/launch/prompts.go:68`、`internal/board/cmd.go:71`、`language.go:198,203`)行为不变。
3. **新测试能真正触达原失败分支**:`internal/menu/overlay_cli_test.go:160-221` 的 `TestSessionSaveLeavesOverlayLanguageIsolatedWhenScopeOmitsKey` 用 `defaultPayload`(`internal/menu/menu_harness_test.go:215-229`,**不含 `language` 键**)写作用域,覆盖文件为 `{"language":"ja"}`,并先断言 `ConfiguredLanguage()=="ja"` 且 `ConfiguredScopeLanguage()==""`(测试自身确认已进入回落分支且绑定值确为 `ja`),再断言 `session.Config.Language != "ja"`、`Save` 后 `LoadScope().Language != "ja"`、覆盖文件字节不变、`Load(true).Language == "ja"`。若把 `options.go:157/160` 回改成 `ResolveLanguage()`,该测试必失败。
4. **写入链全量复查未发现同根因残口**:`Session.Save`(`internal/menu/options.go:504`)→ `SaveIfUnchanged` 整份写 `s.Config`,其 `Language` 只来自 `prepare` 的两条分支或用户显式选择(`options.go:241`);`config.Repair` 的 `repairValues`(`internal/config/repair.go:93-99`)取的是 `CLILanguage()` 而非绑定值,且 `raw` 来自作用域路径;`config.SetLanguageIfPresent`(`internal/config/loadsave.go:304-330`)写调用方给出的 `lang`,唯一非测试调用点 `internal/install/perform.go:98-105` 的 `req.Language` 由 `internal/install/wizard.go:41-66` 的交互式选择产生(且 `applyLanguage` 已把它设为 CLI 覆盖),不会静默吸收覆盖值。`internal/menu/options.go:575` 的 `cfg.Language = existing.Language` 仅在 `NewSessionForTest` 中。

---

## 二、修复范围新增改动的行为 / 质量核对

| 需求 / 行为 | 证据 | 结论 |
| --- | --- | --- |
| 作用域缺 `language` 键时,选项面板保存不写入覆盖语言 | `internal/menu/options.go:154-160`、`internal/config/language.go:163-171` | 通过(Observed) |
| 作用域有显式 `language` 时仍原样保留(不被回落覆盖) | `options.go:154-156` `stored != ""` 分支未改动;原测试 `overlay_cli_test.go:112-158` 保留 | 通过(Observed) |
| 面板文案仍跟随合并后的覆盖语言 | `options.go:165` `BindEffectiveLanguage()` 位置与语义未变;`language.go:119-136` `ConfiguredLanguage()` 仍做覆盖合并(QA-003 修复未被破坏) | 通过(Observed) |
| `ResolveLanguage` 重构对既有读取路径行为等价 | 与 `7466cd6` 原实现逐分支比对(见上文第 2 点);`CLILanguage()` 为既有函数,另一调用点 `repair.go:97` 语义不变 | 通过(Observed) |
| `--lang` / `KANDER_LANG_CLI` 优先级未被新函数打乱 | `ResolveScopeLanguage` 先 `CLILanguage()` 后 locale,与 `ResolveLanguage` 前两级一致 | 通过(Observed) |
| 锁使用正确(无嵌套持锁) | `CLILanguage` 在 `language.go:55-57` 取放锁后返回,`ResolveLanguage`/`ResolveScopeLanguage` 在其外再取锁;`ApplyLanguageArgument`/`BindConfigLanguage` 持锁期间不调用 `CLILanguage` | 通过(Observed) |
| 新增注释与实现一致 | `language.go:107-110`(`ConfiguredScopeLanguage` 空值含义与「写入路径不得回落 `ResolveLanguage`」)、`options.go:162-164`(会话语言=未合并显式值或 CLI/env 回落,面板文案走合并) 均与 `options.go:154-165` 实际代码相符;上一轮 QA-005 指出的两处过度承诺注释已改写 | 通过(Observed) |
| 新增测试非冗余、断言指向被测行为 | `language_test.go:30-46` 在同一用例内做 `ResolveLanguage`(保留绑定值)与 `ResolveScopeLanguage`(忽略绑定值)的对照断言,与既有 `TestResolveLanguageJapaneseLocale`(`language_test.go:48-61`,只测 locale 回落、绑定值为 nil)覆盖不同分支;`overlay_cli_test.go:160-221` 与 `:112-158` 分别覆盖「缺键回落」与「显式键」两条互斥分支 | 通过(Observed) |
| 覆盖文件字节在新增写入路径后不变 | `overlay_cli_test.go:207-210` | 通过(Observed) |
| 运行时合并语义未被写入隔离削弱 | `overlay_cli_test.go:217-220` 断言 `Load(true).Language == "ja"` | 通过(Observed) |
| 文档承诺与实现一致 | `rules/KANDER-AGENTS.md:19`「Overlay values are never written back」在本轮后不再有已知反例(见第一节第 4 点) | 通过(Observed) |

---

## 主要发现(Gate Findings)

**无。** 本轮修复范围 `777b1f0..4832ee9` 未引入、未加重、未掩盖任何门禁级问题,也未破坏它所触及的需求:QA-005 的根因被真实消除,`ResolveLanguage` 的重构与原实现逐分支等价,新增测试能触达原失败分支且不与既有用例重复。上一轮已关闭的 QA-001/002/003/004 所涉文件(`internal/config/overlay.go`、`repair.go`、`internal/tui/prefs.go`、`options_panel.go`、`options_form.go`)在本修复范围内完全未改动,保持关闭。

---

## NON-BLOCKING

### QA-101 — recommend — `overlayAllowedKeys` 手工复制顶层 schema 键集,新增配置键时覆盖文件会误报未知键
本轮未改动,现象未变。`internal/config/overlay.go:19-31` 仍硬编码 11 个允许键(`overlay.go:92` 据此拒绝),而 `internal/config/config.go:656` 的 `Validate` 无顶层键白名单、对未知键静默忽略,两处没有共享来源。后续为 `config.json` 新增顶层键时若忘记同步这张表,覆盖文件里使用该键会被 `config.overlay_has_unknown_keys` 拒绝,错误方向具有误导性。改法:从 `Config` 结构体 json tag 反射生成允许键集合,或在 `config.go` 定义单一 `topLevelKeys` 常量供两处引用,禁止键仍单独维护。

### QA-102 — low — 作用域无 `rules` 节时,覆盖文件写入 `rules` 任一子键会把其余六个模块变为关闭
本轮未改动,现象未变。`internal/config/overlay.go:58-70` 的 `deepMerge` 对 `rules` 做键级合并;当作用域是「合法旧配置、整节缺失 `rules`」(该形态下 `internal/config/config.go:720` 用 `legacyRules()` 七开关全开)时,合并后 `rules` 节从「不存在」变为「只含覆盖写的那一个键」,`internal/config/rules.go:74` 的 `validateRules` 以 `DefaultRules(false)` 为底,其余六项落为 `false`;若覆盖写的是 `{"task_groups": true}`,还会因 task_groups⇒git 依赖让 `Load` 直接报错。改法:`mergeOverlayRaw` 中当 `scope` 无 `rules` 键而 `overlay` 有时,先把 `legacyRules()` 的完整值物化进 `scope` 再合并。

### QA-103 — suggest — `inspectOverlayCandidate` 对同一路径做了两次 reparse 判定
本轮未改动,现象未变。`internal/config/overlay.go:118` 调用的 `overlayPathSafe`(`overlay.go:105-111`)内部已执行 `rejectLeafReparse` → `internal/config/paths.go:39-46` 的 `os.Lstat` + `fs.IsReparsePoint`,而 `overlay.go:124-130` 又重复一次;第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防的是不同东西。改法:去掉 `overlay.go:128-130`,只保留 `Lstat` 结果用于 `IsRegular` 判断。

### QA-104 — suggest — 两档不同时的 `kander config` 摘要行缺少冒号,与其余各行格式不一致
本轮未改动,现象未变。`internal/config/format.go:138-139` 折叠时输出 `审核环节: PM=…`,`format.go:141-143` 不折叠时输出 `审核环节 大: PM=…`(标签后无冒号)。`FormatConfigLines` 中其他每一行都是 `标签 + ": " + 值`(如 `format.go:130,145,152`),按 `标签:` 前缀切分输出的脚本会漏掉分档形态。改法:分档时也用 `Text("config.review_stages")+": "+summary`,由 `FormatReviewStagesSummary` 返回的档名前缀承担区分。

### QA-105 — suggest — 覆盖侧 `review_stages` 规范化失败时,错误信息不指出是 `.kander-config.json` 的问题
本轮未改动,现象未变。`internal/config/overlay.go:205-213` 的 `mergeOverlayRaw` 对覆盖副本调用 `normalizeReviewStagesField` 后直接把错误原样上抛,而这些文案(`internal/config/review_stages.go` 的 `config.review_stages_mixes_scale_and_role_keys` / `config.review_stages_has_unknown_scales` / `config.review_stages_has_unknown_roles`)都只提到 `review_stages`,不含文件路径。项目提交的 `.kander-config.json` 写成 `{"review_stages": {"large": {...}, "PM": "auto"}}` 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键都是带路径报错的(`overlay.go:193-201`、`overlay.go:85-103`),诊断力不一致。改法:在 `mergeOverlayRaw` 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 `mergeOverlayRaw`,`loadEffective` 与 `ConfiguredLanguage` 两个调用点都已持有该路径)。

(候选项无裁剪;共 5 条,均为上一轮延续,未超过十条上限。本轮修复范围未产生新的非阻塞候选项。)

按只读约束,本次审核未修改、创建或删除仓库内任何文件,工作树保持干净、HEAD 未移动;按任务文件末尾的要求,已删除 `/tmp/claude-review.f945a55acba2c6f059f4b895ee5c3e5f/prompt.txt`(仓库外的一次性任务文件)。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[{"id":"QA-101","tier":"recommend","text":"本轮未改动,现象未变。overlayAllowedKeys 手工复制了 config.json 的 11 个顶层键,而 Validate 本身无键白名单、对未知键静默忽略,两处没有共享来源。后续新增顶层配置键时若忘记同步这张表,覆盖文件使用该键会被 config.overlay_has_unknown_keys 拒绝,错误提示方向具有误导性。建议把允许键集合改为从 Config 结构体的 json tag 反射生成,或在 config.go 定义单一 topLevelKeys 常量供两处引用,禁止键仍单独维护。","evidence":"internal/config/overlay.go:19-31(硬编码 overlayAllowedKeys)、internal/config/overlay.go:92(据此拒绝未知键);internal/config/config.go:656(Validate 无顶层键白名单,未知键被忽略)","lineage":{"run_id":"qa-batch-one-r3","finding_id":"QA-101"}},{"id":"QA-102","tier":"low","text":"本轮未改动,现象未变。deepMerge 对 rules 做键级合并:当作用域是「合法旧配置、整节缺失 rules」(该形态下七个开关全开)且覆盖文件写入 rules 的任一子键时,合并后 rules 节从「不存在」变为「只含该键」,validateRules 以 DefaultRules(false) 为底,其余六项落为 false;若覆盖写的是 {\"task_groups\": true},还会因 task_groups 依赖 git 的校验让 Load 直接报错。建议在 mergeOverlayRaw 中,当 scope 无 rules 键而 overlay 有时,先把 legacyRules() 的完整值物化进 scope 再合并。","evidence":"internal/config/overlay.go:58-70(deepMerge 键级合并);internal/config/rules.go:74(validateRules 以 DefaultRules(false) 为底并执行依赖检查);internal/config/config.go:720(缺 rules 节时用 legacyRules());AGENTS.md:49(旧配置缺整节时七开关全开)","lineage":{"run_id":"qa-batch-one-r3","finding_id":"QA-102"}},{"id":"QA-103","tier":"suggest","text":"本轮未改动,现象未变。inspectOverlayCandidate 对同一路径做了两次 reparse 判定:overlayPathSafe 调用的 rejectLeafReparse 内部已执行 os.Lstat + fs.IsReparsePoint,紧接着函数体又重复了一次,该第二次判定在非 Windows 分支上永远不会命中,读代码时容易误以为两者防护的是不同对象。建议删去重复段(overlay.go:128-130),只保留 Lstat 结果用于 IsRegular 判断,Windows 的逐段校验仍由 overlayPathSafe 承担。","evidence":"internal/config/overlay.go:118(inspectOverlayCandidate 调用 overlayPathSafe)、internal/config/overlay.go:105-111(overlayPathSafe → rejectLeafReparse);internal/config/paths.go:39-46(rejectLeafReparse 已做 Lstat + IsReparsePoint);internal/config/overlay.go:124-130(重复的 Lstat + IsReparsePoint)","lineage":{"run_id":"qa-batch-one-r3","finding_id":"QA-103"}},{"id":"QA-104","tier":"suggest","text":"本轮未改动,现象未变。kander config 人类可读输出在两档相同时是「审核环节: PM=…」,两档不同时变成「审核环节 大: PM=…」——标签后缺冒号,冒号移到了档名后面,与 FormatConfigLines 中其余每一行统一的「标签 + \": \" + 值」格式不一致,按标签冒号前缀切分输出的脚本会漏掉分档形态。建议分档时也用 Text(\"config.review_stages\")+\": \"+summary,由 FormatReviewStagesSummary 返回的档名前缀承担区分。","evidence":"internal/config/format.go:141-143(分档分支拼接为 Text(\"config.review_stages\")+\" \"+summary);internal/config/format.go:138-139(折叠分支使用 \": \")、format.go:130,145,152(其余行统一使用 \": \")","lineage":{"run_id":"qa-batch-one-r3","finding_id":"QA-104"}},{"id":"QA-105","tier":"suggest","text":"本轮未改动,现象未变。mergeOverlayRaw 对覆盖副本调用 normalizeReviewStagesField 后把 NormalizeReviewStages 的错误原样上抛,而这些文案只提到 review_stages、不含文件路径。项目提交的 .kander-config.json 写成 {\"review_stages\": {\"large\": {...}, \"PM\": \"auto\"}} 时,用户在该目录下运行任何命令都会看到一条像是在说自己作用域配置有问题的错误;而同一文件的 JSON 非法与禁止/未知键两类错误都是带绝对路径报错的,诊断力不一致。建议在 mergeOverlayRaw 中把覆盖侧的规范化错误包一层带覆盖文件绝对路径的文案(需把路径传进 mergeOverlayRaw,loadEffective 与 ConfiguredLanguage 两个调用点都已持有该路径)。","evidence":"internal/config/overlay.go:205-213(覆盖侧 normalizeReviewStagesField 直接返回原错误);internal/config/review_stages.go(错误文案只含键名,不含文件路径);对照 internal/config/overlay.go:193-201(JSON 非法与根类型错误均带 path)、internal/config/overlay.go:85-103(禁止键/未知键均带 path)","lineage":{"run_id":"qa-batch-one-r3","finding_id":"QA-105"}}]}
```