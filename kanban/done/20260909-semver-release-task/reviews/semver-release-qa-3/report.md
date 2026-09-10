# QA 评审报告

**Role**: QA
**Commit**: `96ddc888e30a399ddbf23b19d4818cfe3ac49ed1`(基线 `aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78`)
**Task Context**: 版本身份改为注入式版本号(首版 0.5.0),发版触发从 push main 改为推送 `v*` tag;本轮增量为纯注释改动(`version.go`、`AGENTS.md` 不再把注入值称作语义版本)。

**Reviewed Scope**

- 变更集全部 7 个文件:`.github/workflows/release.yml`、`AGENTS.md`、`Makefile`、`make-windows.cmd`、`internal/version/version.go`、`internal/version/version_test.go`、`internal/cli/cli_test.go`。
- 追踪的消费方:`internal/cli/cli.go:76`、`internal/tui/options_view.go:58`、`internal/review/archive.go:137` → `internal/board/reviews.go:220 PrepareReviewRun`(version 参数无格式校验,仅落记录)。
- 全仓检索确认 `BuildTimestamp|GitHash` 已清零;`*.md` 中无「push 到 main 发版」或旧时间戳版本格式的残留描述。
- 只读检查,未执行构建/测试;调用方提供的验证记录(`go test ./internal/version ./internal/cli` ok、`go vet ./... && go test ./...` 21 包 ok、`make VERSION=0.5.0` → `kander 0.5.0`、非 git/无 tag 回退 `dev`)与代码一致,采信为证据。
- `scripts/release.sh` 不在本提交的 COMMIT TREE 内(`scripts/` 下仅 `guard-kanban-write.sh`),且属 OUT_OF_SCOPE,其与产物名的一致性标记为 Unverifiable;本次未改动产物循环与 `checksums.txt` 生成方式,不构成对该脚本的破坏。
- 体积规则:改动文件最大 `AGENTS.md` 127 行,均远低于 1000 行,规则不触发。

## 行为 / 质量核对

| 要求 | 结论 | 证据 | 声明 |
| --- | --- | --- | --- |
| `internal/version` 仅暴露可注入 `Version`,默认 `dev`,`String()` 签名不变 | 符合 | `internal/version/version.go:9,12`;`component` 已删除,`strings` 仍被使用,无死代码 | Observed |
| `String()` 对空白值回退 `dev` | 符合 | `internal/version/version.go:13-17`;`version_test.go:15-24`(`Version=" "` → `dev`) | Observed |
| 调用方未改动且行为正确 | 符合 | `cli.go:76`、`options_view.go:58`、`archive.go:137` 均只消费 `String()`;`PrepareReviewRun`(`reviews.go:220-238`)不校验 version 格式 | Observed |
| `on:` 仅 `push: tags: ['v*']` | 符合 | `.github/workflows/release.yml:6-8` | Observed |
| VERSION 取自 `${GITHUB_REF_NAME#v}`,ldflags 只注入 `Version` | 符合 | `release.yml:31,35` | Observed |
| `tag_name` 用 `github.ref_name`,不产生 `vv0.5.0` | 符合 | `release.yml:60`;`name:` 仍用去 v 的 `env.VERSION`(`release.yml:61`) | Observed |
| 六平台产物名与打包方式、`checksums.txt` 不变 | 符合 | `release.yml:38-55` 相对基线仅上下文行,未改 | Observed |
| 本地 make:有 tag 用 `git describe --tags --always`,否则 `dev`,构建不失败 | 符合 | `Makefile:10`(先以 `git describe --tags` 探测,失败走 `dev`);本仓实测 `git describe --tags` → `v20260908T154432Z-7466cd6acfd3-21-g96ddc88` | Observed |
| Windows 脚本行为与 Makefile 对齐 | 符合 | `make-windows.cmd:13-19`;块内使用 `if not errorlevel 1`(运行期求值,不受未开启延迟展开影响),`for /f` 赋值与第 23/26 行的 `%VERSION%` 展开分属不同语句,取值正确 | Observed |
| 测试同步且不冗余 | 符合 | `version_test.go` 两例分别覆盖注入值与空白回退;`cli_test.go:113-125` 覆盖 CLI 输出,无重复断言 | Observed |
| 推送 tag 后 release 页与 latest 下载 URL | 未验证(需真实发版) | 卡内标记为待闭环 | Unverifiable |

## 门禁发现(Gate Findings)

### QA-1 — medium [mechanical] — `version.go` 对本地 make 回退条件的注释与实现不符

**Claim**: Observed
**Evidence**:
- `internal/version/version.go:7-8`:`// Release builds inject the tag with the leading v stripped. Local make` / `// injects \`git describe --tags --always\`, or "dev" when that is unavailable.`
- `Makefile:7-10`:先执行 `git describe --tags`(不带 `--always`)探测,只有成功时才取 `git describe --tags --always`,否则 `echo dev`;该文件自身的注释明确写着 `A checkout with no tags still succeeds with --always (short hash only); treat that like "no tag" and use dev`。

**失败场景**:在有提交但没有可达 tag 的 checkout(例如 fork、未同步 tag 的克隆)中执行 `make`,`git describe --tags --always` 本身完全可用并会输出短 hash,但构建注入的是 `dev`。按 `version.go` 的注释,读者会判定「describe 不可用」,转而排查 git 环境或 CI 取版本逻辑,而真实原因是 Makefile 有意把「无 tag」映射为 `dev`。

**影响**:本轮唯一的改动目标就是让注释与实现一致(commit message: *The comments must match that*),而这条注释仍与 `Makefile:7-10` 自身的说明相互矛盾,后续排查版本号来源时会被误导。

**最小修复**:把 `internal/version/version.go:7-8` 的第二句改为按「是否有可达 tag」表述,例如:`Local make injects the git describe --tags output when a tag is reachable, and "dev" otherwise (including a non-git directory).`

## NON-BLOCKING

### QA-2 — suggest — 改为 tag 触发后,仓库不再有任何针对日常推送的自动校验

**Claim**: Observed
**Evidence**:`.github/workflows/release.yml:6-8` 是 `.github/workflows/` 下唯一的 workflow 文件(目录内仅此一项);其 `Test` 步骤(`release.yml:23-26` 的 `go vet ./...` 与 `go test ./...`)此前随每次 push main 运行,现在只在推送 `v*` tag 时运行。

**影响/理由**:验收条件明确要求 `on:` 只保留 tag 触发,因此这不是缺陷;但副作用是 develop/main 上的常规提交与 PR 不再有任何 CI 校验,问题会推迟到发版当次才暴露(发版路径本身仍会因 `Test` 失败而中止,不会产出坏 release,故影响限于反馈延迟)。

**最小改动**:如需保留原有校验节奏,新增一个独立的 CI workflow(`on: push: branches: [develop, main]` + `pull_request`)只跑 `go vet ./... && go test ./...`,不涉及打包与发布。属可选项,由负责人决定。

```kander-findings
{"FINDINGS":[{"id":"QA-1","tier":"medium","mechanical":"documentation","text":"internal/version/version.go:7-8 的注释称本地 make 注入 `git describe --tags --always`、\"or dev when that is unavailable\",与 Makefile:7-10 的实现不符:Makefile 先用不带 --always 的 `git describe --tags` 探测,仅在探测成功时才取 describe 结果,无可达 tag 时一律注入 dev。失败场景:在有提交但无可达 tag 的 checkout(fork、未同步 tag 的克隆)中执行 make,`git describe --tags --always` 完全可用并会输出短 hash,但二进制版本仍是 dev;按注释判断会误以为 describe 不可用而去排查 git 环境。影响:本轮改动的唯一目的就是让注释匹配实现(commit message: The comments must match that),该注释却与 Makefile 自身对同一分支的说明相互矛盾,后续定位版本号来源时会被误导。最小修复:将第 7-8 行第二句改为按「是否有可达 tag」表述,例如 `Local make injects the git describe --tags output when a tag is reachable, and \"dev\" otherwise (including a non-git directory).`","evidence":"internal/version/version.go:7-8(`// Release builds inject the tag with the leading v stripped. Local make` / `// injects `git describe --tags --always`, or \"dev\" when that is unavailable.`);Makefile:7-10(`VERSION ?= $(shell if git describe --tags >/dev/null 2>&1; then git describe --tags --always; else echo dev; fi)`,并附注释 `A checkout with no tags still succeeds with --always (short hash only); treat that like \"no tag\" and use dev`)。两处对「无 tag 的 git checkout」这一路径给出相反描述。"}],"NON_BLOCKING":[{"id":"QA-2","tier":"suggest","text":"改为 tag 触发后,仓库不再有针对日常推送/PR 的自动校验。release.yml 的 Test 步骤(go vet ./... 与 go test ./...)此前随每次 push main 运行,现在只在推送 v* tag 时运行,而 .github/workflows/ 下没有第二个 workflow。这符合验收条件(on: 只保留 tags: ['v*']),不是缺陷;副作用是 develop/main 的常规提交问题会推迟到发版当次才暴露(发版路径仍会因 Test 失败中止,不会产出坏 release,影响限于反馈延迟)。可选改动:新增一个独立 CI workflow(on: push 到 develop/main + pull_request)只跑 go vet ./... && go test ./...,不涉及打包与发布。是否值得由负责人决定。","evidence":".github/workflows/release.yml:6-8(on: push: tags: ['v*'],基线为 branches: [main]);.github/workflows/release.yml:23-26(Test 步骤 go vet ./... / go test ./...);.github/workflows/ 目录下仅 release.yml 一个文件。"}]}
```