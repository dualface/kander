# Self-installing binary: embed the rules and replace the install script with a wizard

- 类型: Feature
- SIZE: large
- 任务组:
- 创建时间: 2026-09-06 21:53
- 负责人: cursor
- 会话: cursor dbe03c85-cc25-4bbd-b35f-1377c3c19712
- 窗口: herdr:wX:tF:wX:pZ
- 开始时间: 2026-09-06 23:21
- 完成时间: 2026-09-07 00:38
- 任务分支: self-install-binary
- 结果: completed

## 任务目标

把 kander 的安装能力从两个外部脚本收进二进制本身.

今天安装完全依赖仓库根的 `install.sh` (487 行) 与 `install.ps1` (664 行), 它们从**已检出的源码树**里把 `rules/*.md` 拷到规则目录. 后果是 `go install`、下载单个 release 二进制、`go run` 三种方式装出来的规则目录都是空的, `menu.rulesIntegration` 与 `launch` 的规则加载指令会指向不存在的文件. 两个脚本还各自硬编码了一整套中英双语文案, 与 `internal/i18n` 目录完全重复.

改为: 规则 markdown 以 `go:embed` 编进二进制; 首次运行检测到「没有配置文件, 且自己不在安装目录里」时进入交互向导 — 选语言、选安装目的地、释出规则、把自己拷到目的地, 再启动目的地二进制走既有的 doctor → options 首次运行流程. 删除两个脚本.

## 用户决策

以下为用户明确确认的决定, 不得在实施中变更:

1. 删除 `install.sh` 与 `install.ps1`, 安装逻辑全部进 Go.
2. 只做交互向导, **不**提供 `--yes` / `--scope` 一类非交互参数; 另加 `kander install` 子命令用于重跑.
3. 语言选择同时决定三件事: 向导 UI 语言、释出哪套规则 markdown、写入配置供 Agent 使用.
4. 规则 markdown 改为 `rules/cn/` + `rules/en/` 双语目录, **en 缺失的文件回落 cn 原文**; 本轮**不做**规则正文翻译.
5. 升级路径为两条: 新增 `kander install` 子命令, 以及 doctor 检测规则文件缺失/过期并修复.
6. **不**在 options 里切换语言时自动重释规则 (用户在多选中未选此项).
7. `install.sh` 的下列行为全部照搬: 项目安装及 `.git/info/exclude` 写入; 符号链接/目录穿越安全拒绝; `AGENTS.md` 符链且不覆盖用户已有文件; 旧版 onevoke/kanban 残留清理提示.
8. 走看板执行.

## 预期成果

- 一个干净环境里, 拿到单个 kander 二进制直接运行即可完成安装, 全程不需要源码树, 不需要 shell 脚本.
- 装完后规则目录内容与二进制内嵌版本一致, 且可被 doctor 校验和修复.
- 仓库内不再有 `install.sh` / `install.ps1`, 也不再有以子进程驱动脚本的安装测试.
- 安装路径的文件写入与其余写入一致, 全部经过 `internal/fs`; `AGENTS.md` 中「安装脚本是唯一例外」的条款随之删除.

## 验收条件

- [x] `go build ./... && go test ./... && go vet ./...` 全绿, `gofmt -l .` 无输出, `git diff --check HEAD` 无告警.
- [x] `rules/` 成为 Go 包并以 `go:embed` 内嵌 `cn/` 与 `en/` 两套; `rules/en/` 用一份**非 `.md` 的占位文件** (如 `README.txt`, 说明英文规则放这里) 使目录非空以通过编译, **不翻译任何规则正文**. `Names()` 以 `cn/` 为唯一真源, 占位文件不会被枚举或安装.
- [x] 语言解析: 请求 en 而 `rules/en/` 缺该文件时返回 cn 原文并可报告实际语言; 请求 cn 永不回落.
- [x] 干净 HOME 下运行未安装的二进制进入向导; 依次选语言与目的地后, 二进制落在 `~/.local/bin/kander` 且具可执行位, `~/.agents/` 下 10 个规则文件齐全, `AGENTS.md` 为指向 `KANDER-AGENTS.md` 的相对符链.
- [x] 已存在的 `~/.agents/AGENTS.md` 用户文件在安装后逐字节不变.
- [x] 项目安装写入 `<主 worktree>/.kander/{bin,rules}` 并向 `.git/info/exclude` 幂等追加 `/.kander/`; 同一次运行不触碰全局目录 (以 `$HOME` 下的 canary 文件未被创建/修改为证).
- [x] 从 linked worktree 发起的项目安装归一到主 worktree; 非 Git 目录被拒绝.
- [x] 全局安装时探测 `~/.local/bin` 下的旧版残留入口 (`onevoke`, `kanban`, `onevoke-review.sh`, `onevoke-review` 及其 Windows 变体), 存在时提示用户是否删除, **默认保留**; 用户确认后才删除, 且删除只发生在新 `kander` 已校验可执行之后. 残留入口是目录时硬失败, 不静默跳过.
- [x] 安全拒绝: 目标位置是目录时拒绝; 项目模式下目标是符号链接/reparse point 时拒绝; 源二进制是符号链接时拒绝.
- [x] 幂等: 连续运行两次 `kander install` 均成功, 不重复追加 exclude, 已在目的地时跳过自拷贝.
- [x] Windows 上目标 `kander.exe` 已存在且被占用而无法直接替换时, 先将其改名让位再写入新文件, 安装不因此失败; 遗留的让位文件在下次安装或 doctor 时清理. (完整的重启式自更新不在本轮范围.)
- [x] 向导选定的语言在交接后仍然生效 — 选中文装完后配置中 `language` 为 `cn` (当前 `internal/config/repair.go` 会强制写 `en`, 必须一并修正).
- [x] 交接后的进程自动运行 doctor 并打开 options; 在**没有 kanban 目录**的路径下同样成立.
- [x] 已有配置的用户在非看板目录运行 `kander` 时行为不变, 仍报「找不到看板」, 不被拉进 options 或向导.
- [x] 开发工作流不被劫持: 源码树内构建出的 `./kander` 直接运行不进向导.
- [x] doctor 能报出规则文件缺失与内容不符并修复; 用户手改过的规则文件不被静默覆盖.
- [x] 配置语言与已安装规则语言不一致时, doctor 以 note 提示漂移, **不**改判健康状态, **不**自动重释规则 (对应用户决策 6).
- [x] `kander install` 出现在 `commandNames` 与 `--help` 输出中并可分发.
- [x] `kander install` 不接受 `--yes` / `--scope` / `--project` 等非交互参数 (对应用户决策 2), 传入时按未知参数报错并给出用法.
- [x] `install.sh`、`install.ps1` 及 `internal/cli/install_posix_test.go`、`internal/cli/install_windows_test.go` 已删除, 其断言意图由新的进程内测试覆盖.
- [x] 新增向导文案在 `en.json` 与 `zh-CN.json` 中成对存在, `internal/i18n` 既有目录测试通过.
- [x] 遵守仓库「语言约定」(`AGENTS.md`, 提交 f56eacb): 新增 Go 代码的行/块/文档注释全部英文, 提交备注全部英文; 面向用户的字符串仍走 `internal/i18n` 双语资源, 不硬编码.
- [x] 文档同步: `README.md` 安装节、`AGENTS.md` (第 94 行例外条款 + 第 104 行 `rules/` 路径 + 子命令表)、`Makefile` 的 `install` 目标、以及 `rules/cn/KANDER-BASE-RULES.md` 中「POSIX 用 install.sh, 原生 Windows 用 install.ps1」一句.

## 威胁模型

本任务把特权较高的文件操作 (写可执行文件、建链接、写 Git exclude) 从 shell 移进 Go, 因此按安全任务对待.

- **资产**: 用户 `~/.local/bin` 下的可执行入口; `~/.agents/` 下的规则原文 (会被 Agent 当作指令读取); 项目 `.git/info/exclude`; 用户既有的 `AGENTS.md` / `CLAUDE.md` 集成配置.
- **可信主体**: 运行安装的本地用户本人.
- **攻击者能力**: 能在目标路径预置符号链接、junction 或其他 reparse point, 诱导安装器把二进制或规则写到目录外 (经典 symlink 攻击); 能预置同名目录制造写入歧义; 在 Windows 上能借 reparse point 在校验与写入之间切换目标 (TOCTOU).
- **对策**: 全部写入经 `internal/fs` 的固定父句柄 + no-follow 语义, 逐分量拒绝 reparse point; 目标为目录时硬失败; 绝不覆盖用户已有的 `AGENTS.md`; 旧入口删除只在新二进制校验可执行之后进行. 保留脚本原有的作用域差异: 全局模式不拒绝符链规则文件 (允许用户把规则并入 dotfiles 仓库), 项目模式拒绝.
- **非目标**: 不防御已能任意写入用户 HOME 的本地攻击者; 不做二进制签名或来源校验.

## 不在本轮范围

按「既有问题 / 加固 / 共享契约与文档 / 相邻功能」四类逐条界定:

- **既有问题 — `check` 子命令被重复注册**: `internal/cli/wire_board.go` 的 `board.RunCheck` 与 `internal/liveness/bind.go` 的 `liveness.RunCheck` 写同一 map key, 后者 init 更晚而胜出. 排除理由: 与安装无因果关系, 且需用户确认哪个才是预期行为, 本轮不擅自改语义. 已在汇报中向用户点出, 是否另开卡由用户决定.
- **既有问题 — doctor 不校验 Agent 侧规则集成**: `menu.rulesIntegration` 只在 options 保存时调用. 排除理由: 属 doctor 检查面的独立扩展, 本轮只补「规则文件是否存在且与内嵌一致」这一条, 避免验收面失控.
- **加固 — 二进制签名 / 校验和 / 自动更新**: 排除理由: 需要发布渠道与密钥管理决策, 用户未提出, 且与「首次运行自安装」目标正交.
- **加固 — Windows 覆盖正在运行的 exe 的完整自更新链路**: 本轮只做到「已在目的地则跳过拷贝」加改名让位的兜底, 不做完整的重启式自更新. 排除理由: 真正的自更新需要版本发现机制, 属独立功能.
- **共享契约与文档 — 规则正文的英文翻译**: 本轮只搭 `rules/en/` 目录并让缺失文件回落 cn, **不翻译任何规则正文, 一份也不翻**. 排除理由: 用户决策 4 是不加限定的「本轮不做规则正文翻译」. 早期草案曾以「embed 空目录编译失败」为由主张必须先翻译入口文件 `KANDER-AGENTS.md`, 该主张已废弃 — 目录形式的 `//go:embed en` 会收录任意非点开头文件, 用非 `.md` 占位文件即可满足非空要求, 无需触碰规则正文. 规则正文精度直接影响 Agent 行为, 需逐份复核, 应另开卡.
- **共享契约与文档 — `share/` 载荷**: 仓库中并不存在 `share/` 目录, 脚本里的相关分支是死代码. 本轮只保留 `InstallPaths.ShareDir` 字段并建出目录, 不搬运任何载荷. 排除理由: 无内容可搬; 字段保留以免影响既有路径契约.
- **相邻功能 — 非交互 / CI 安装模式**: 用户明确表示暂不需要. 排除理由: 用户决策 2. 注意这会使「脚本化项目安装」能力随 `install.sh --project` 一并消失, 已如实告知.
- **相邻功能 — 切换界面语言时自动重释规则**: 用户在多选中未选择该项. 排除理由: 用户决策 6; doctor 会以 note 形式提示语言漂移, 但不改判健康状态、不自动重写.
- **相邻功能 — 首次运行自动 `kander init` 建看板**: 本轮只保证无看板时 options 仍能打开, 不代用户在任意目录创建看板. 排除理由: 建看板是用户对项目的决定, 不应由安装器代做.

## 讨论与决策

**关键技术结论 (实施前已核实)**

- `go:embed` 只能引用包目录及其下层, 而 `rules/` 在仓库根. 因此让 `rules/` 目录自身成为 Go 包 (仓库根无任何 `.go` 文件, 不会冲突), 而不是把规则搬进 `internal/`.
- `//go:embed en/*.md` 在目录无匹配文件时**编译失败**, 且 embed 会跳过点开头文件, `.gitkeep` 无效. 但**不需要**因此翻译规则正文: 改用目录形式 `//go:embed en` 收录任意非点开头文件, 放一份 `rules/en/README.txt` 占位即可. 语言解析只查 `en/<name>.md`, 且 `Names()` 以 `cn/` 为真源, 占位文件既不会被枚举也不会被安装. (早期草案误判此处为「必须先翻译入口文件」的硬前置, 与用户决策 4 冲突, 已按上述方案消解.) 以上四点已用一次性最小 Go 模块实测确认: 空 `en/` 报 `cannot embed directory en: contains no embeddable files`; 只放 `.gitkeep` 报同样错误 (点开头文件被跳过); 放 `README.txt` 后 `//go:embed cn en` 编译通过且只收录 `cn/*.md` 与 `en/README.txt`; 读取不存在的 `en/<name>.md` 正常落空可回落 cn.
- `config.ProjectInstallPaths()` 与 `config.EnsureProjectGitExclude()` 目前是死代码 (除测试外无调用点), 显然为 Go 侧安装器预留, 项目安装与 git exclude 直接复用即可.
- `internal/fs` 已具备脚本手写的全部安全原语: `IsReparsePoint`、根锚定 fail-closed 校验、固定父句柄原子替换、`AppendUniqueLine`. 但 `WriteTextAtomic` 只收 string 且不设可执行位, 发布二进制需要新增一个二进制原子写入 helper.
- **阻塞点**: `internal/tui/cmd.go` 的首次运行顺序是 doctor → `board.BoardRoot()` → 开 options; 而 `BoardRoot()` 在找不到 `kanban/` 时返回错误并直接 `fail`. 于是全新机器上交接过去只会跑完 doctor 就退出, **永远到不了 options**. 必须让「配置不存在 + 定位不到看板」降级为空看板继续开 options (`board.IsBoardNotFound` 已是现成判定).
- **语言会被抹掉**: `internal/config/repair.go` 的 `repairValues` 硬写 `defaults.Language = "en"`, 而 `recoverConfigFields` 只用已提供字段覆盖默认值. 若向导不写配置 (它不应写, 否则 `config.Exists()` 变真会导致交接后 doctor 与 options 都不触发), 配置由子进程 doctor 创建时语言必然落成 `en`, 用户选的中文被丢弃. 修法是让 `repairValues` 尊重显式语言 (`KANDER_LANG_CLI`), 向导用 `config.ApplyLanguageArgument` 设置, 该变量随 `os.Environ()` 自动流进子进程.
- 首次运行判定取「配置不存在 **且** 二进制不在安装 bin 目录」的合取, 天然不劫持开发机 (开发机有配置); 另需源码树识别与逃生开关, 覆盖「开发机上删了配置」的情况.
- 仓库在建卡当日新增了「语言约定」(`AGENTS.md`, 提交 f56eacb): 提交备注与代码注释一律英文, Markdown 文档 (含 `rules/`) 保持中文, 面向用户字符串仍走 i18n. 本卡新增的 Go 代码与提交须遵守; 规则 markdown 的中文正文不受影响, `rules/en/` 是给最终用户的翻译, 与该约定不冲突.
- 不拆任务组: 本特性中间态会破坏仓库 — 例如规则一旦移入 `rules/cn/`, 现有 `install.sh` 的 `rules/*.md` glob 立刻失效. 必须整体落地.

自审: 通过. 核对项与修正如下 — (1) 目标与成果对齐用户原始描述的五个步骤, 未擅自扩大; (2) 用户决策八条逐条回填自三轮确认答复, 其中「不自动重释规则」与「不做非交互模式」是用户**未选/否定**的项, 已明确记为约束而非建议; (3) 验收条件覆盖全部预期成果, 且把两个实施前核实出的阻塞点 (无看板时 options 打不开、语言被 repair 抹成 en) 写成可判定条目, 避免实现者遗漏; (4) 边界方面, 「英文规则正文翻译」按用户决策 4 完整排除, 本轮零翻译 — 此处原写作「保留翻译入口文件这一 embed 硬前置」, 该说法已被下述第 (6) 条推翻并作废, 不再成立; (5) 修正一处初始遗漏 — 首版契约未包含 `rules/cn/KANDER-BASE-RULES.md` 中指向 install.sh/ps1 的规则原文, 该文件会被安装并由 Agent 当指令读取, 不改会指导 Agent 使用已删除的脚本, 已补入验收条件. (6) 首轮卡审 FAIL 后修正两处: 其一, 原「不在本轮范围」把作者自己发现的 embed 编译约束写成「排除理由: 用户已确认」, 把技术必要性伪装成用户对翻译入口文件的授权, 与用户决策 4「本轮不做规则正文翻译」冲突 — 已改用非 `.md` 占位文件方案, 该冲突不复存在, 本轮一份规则正文都不翻, 决策 4 完整成立, 无需退回用户裁决; 其二, 补上 Windows 改名让位兜底的验收条目, 原文只在范围说明里提到却无对应可判定条件. (7) 第二轮卡审 FAIL 后再修正: 补上旧版 onevoke/kanban 残留清理提示的验收条目 — 该项是用户决策 7 的第四条, 首两版契约只在决策里列出却未落到任何验收条件, 属确认需求丢失; 同时改写本行第 (4) 条, 撤回其中已被推翻的「翻译入口文件是硬前置」说法, 避免与第 (6) 条并存误导; 另补 doctor 语言漂移只提示不改判、以及 install 不接受非交互参数两条验收. 无需新增用户决策的歧义项.

卡审: PASS — 由不共享建卡会话上下文的独立子 Agent 只读卡片、用户原始需求与仓库源码出具, 共三轮. 前两轮 FAIL 各查出一处实质缺陷: 其一, 「不在本轮范围」把作者自行发现的 embed 编译约束写成「排除理由: 用户已确认」, 把技术必要性伪装为用户对翻译入口文件的授权, 与用户决策 4 冲突; 其二, 用户决策 7 第四项「旧版 onevoke/kanban 残留清理提示」被列为确认需求后在预期成果与全部验收条件中消失. 另有 Windows 改名让位兜底、doctor 语言漂移提示、install 拒收非交互参数三处验收缺口, 以及自审记录中已被推翻却未撤回的旧说法. 全部修正后第三轮通过: 四项照搬行为逐条核对与 install.sh/install.ps1 真实语义一致 (默认保留、确认后删、删除前校验新入口可执行、目录目标硬失败), 五项载荷技术断言与源码一致, 无新增矛盾.

## 实施与验证

- 工作目录: `/home/dualf/works/kander/worktrees/self-install-binary` (集成后已清理)
- 任务分支: `self-install-binary` (已合入 `develop` 并删除)
- 审核 base: `586852545206645bf6fd09e6980651aba268f61e`
- 交付 commit: `77d52105533e52015c7fab5967d77875c583c5d7`
- 验证: `go build ./... && go test ./... && go vet ./...` 全绿 (rebase 到 `origin/develop` 后重跑同样全绿); `gofmt -l .` 空; `git diff --check HEAD` 无告警.
- 审核: 白名单命中功能开发、安装/发布、CLI 契约. PM (codex) 与 QA (codex) 首轮 FAIL; 修复后增量复审关闭 PM-001/002/003 与 QA-001/002/003/004. QA-005 为机械项, 主代理在 `9bb77c4`/`77d5210` 核实 `TestWriteBinaryBusyRenameAside` 断言 `dest.old` 后通过, 未重跑. CSA/Hacker 按本仓库 AGENTS.md 为 N/A. rebase 无冲突, 未重审.
- 环境缺口: 本机非 Windows; 占用 exe 的真实进程占用用例只在 `GOOS=windows` 编译. 交互 Huh 向导未在真实 TTY 端到端点按.

## 完成总结

规则以 `go:embed` 编进二进制, `install.sh`/`install.ps1` 已删除. 干净环境下运行未安装的 kander 进入向导; `kander install` 可重跑; doctor 修复缺失/过期规则且不覆盖手改. 已合入 `develop` `77d52105533e52015c7fab5967d77875c583c5d7`. 验收 24/24. 偏差: 全局规则符链不写穿 (按威胁模型), 只如实 stamp; 无看板时 options 可开但不自动 `kander init`.
