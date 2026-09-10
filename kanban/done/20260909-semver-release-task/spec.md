# 版本身份改为语义化版本, 发版改为 tag 触发

- TYPE: Chore
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 12:45
- OWNER: cursor
- SESSION: cursor 4fc2b5b9-ca98-4b03-953f-f144bf9517a0
- WINDOW: herdr:w2T:tM:w2T:pM
- STARTED_AT: 2026-09-09 12:54
- FINISHED_AT: 2026-09-09 13:38
- TASK_BRANCH: semver-release
- RESULT: completed

## GOAL

把 kander 的版本身份从 `<构建时间戳>-<git hash>` 改为语义化版本, 并把发版触发从「push 到 main」改为「推送 `v*` tag」, 首个语义版本为 0.5.0.

原因: 时间戳版本号无法被 Homebrew formula 稳定引用, 且每次 push main 都自动发版的节奏不适合对外分发.

## USER_DECISIONS

- 发版触发改为 `on: push: tags: ['v*']`; push 到 main 不再产生 release.
- `version.String()` 只返回语义版号, 不再拼接构建时间戳和 git hash.
- 首个语义版本为 0.5.0.
- 用户已知悉并接受: 评审运行清单 (`internal/review/archive.go:137` 写入的版本串) 与 TUI 版本显示将不再包含 commit 信息, 线上排查时无法从版本串定位到具体构建.

## EXPECTED_OUTCOME

- `internal/version` 只暴露一个可注入变量 `Version`, 默认值 `dev`; `String()` 返回它, 函数签名不变.
- 推送 tag `v0.5.0` 后, GitHub Actions 产出 tag 名为 `v0.5.0` 的 release, 六个平台的产物名与 `checksums.txt` 的生成方式保持不变.
- 该 release 中的二进制执行 `kander version` 输出 `kander 0.5.0`.
- 本地 `make` 构建出的二进制版本号来自 `git describe --tags --always`; 无 tag 或非 git 目录下回退为 `dev`, 构建不失败.
- push 到 main 不再触发 Release workflow.

## ACCEPTANCE_CRITERIA

- [ ] `internal/version/version.go` 中不再存在 `BuildTimestamp` 与 `GitHash`; 新增 `Version`, 默认值为 `dev`; `String()` 对空白值仍回退为 `dev`.
- [ ] `internal/version/version_test.go` 与 `internal/cli/cli_test.go` 已同步改为断言语义版号, `go vet ./...` 与 `go test ./...` 全部通过.
- [ ] `.github/workflows/release.yml` 的 `on:` 只保留 `push: tags: ['v*']`, 不再含 `branches: [main]`.
- [ ] release.yml 中版本取自 `${GITHUB_REF_NAME#v}`, ldflags 只注入 `Version`, 不再注入时间戳与 hash.
- [ ] release 的 `tag_name` 直接使用 `github.ref_name`, 推送 `v0.5.0` 后 release 页面显示的 tag 为 `v0.5.0` 而非 `vv0.5.0`.
- [ ] `Makefile` 与 `make-windows.cmd` 只注入 `Version`, 默认值取 `git describe --tags --always`, 且在无 tag 或非 git 目录下能构建成功并得到 `dev`.
- [ ] 六个目标平台 (windows/linux/darwin × amd64/arm64) 的产物名与打包方式 (windows 用 zip, 其余用 tar.gz) 与改动前一致, `checksums.txt` 仍覆盖全部产物.
- [ ] 推送 `v0.5.0` 后, `https://github.com/dualface/kander/releases/latest/download/kander-darwin-arm64.tar.gz` 返回 200.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不修 `release.yml` 里与本次目标无关的既有问题 (例如 action 版本未按 SHA 固定, 未做发版前的 tag 唯一性校验), 避免把无关改动混进这次发版链路调整.
- 加固: 不做 macOS 代码签名与公证. 用户已选择方案 A, 靠 `curl` 安装绕开 Gatekeeper 隔离标记, 说明写在 README.
- 共享契约与文档: `version.String()` 的函数签名与调用方 (`internal/cli/cli.go:76`, `internal/tui/options_view.go:58`, `internal/review/archive.go:137`) 属本卡范围, 必须保持可编译且行为正确; 但 README 的 `brew install` 安装说明与发版操作手册不在本卡, 作为纯文档改动单独处理.
- 邻近功能: 不改 `dualface/homebrew-tap` 仓库的 formula, 也不改 `scripts/release.sh`. tap 的更新由该脚本在本地完成 (读 release 的 checksums.txt 后重写 formula 并 push), 不进 CI; 脚本已先行落地, 不属于本卡改动范围.

## DISCUSSION

- 影响面 (grep `BuildTimestamp|GitHash` 的完整结果): `internal/version/version.go`, `internal/version/version_test.go`, `internal/cli/cli_test.go`, `Makefile:7-10`, `make-windows.cmd:13-21`, `.github/workflows/release.yml:37`. 共 6 个文件, 无遗漏.
- `internal/tui/options_view.go:58` 与 `internal/review/archive.go:137` 只消费 `version.String()`, 不需要改动, 但两处显示/记录的内容会随之变短.
- 易错点: 现在 release.yml 写的是 `tag_name: v${{ env.VERSION }}`. 改成 tag 触发后如果沿用这行, 会把 `v0.5.0` 再拼一次前缀得到 `vv0.5.0`. 必须改为直接用 `github.ref_name`.
- 易错点: `git describe --tags --always` 在浅克隆 (CI 的 `actions/checkout` 默认 `fetch-depth: 1`) 下拿不到 tag. 但 CI 路径的版本号来自 `GITHUB_REF_NAME` 而非 `git describe`, 所以不受影响; `git describe` 只用于本地 Makefile 的默认值.
- 发版脚本 `scripts/release.sh` 已于建卡后单独落地 (纯 shell, 按项目规则免走看板). 它假定本卡改造完成后的行为: 推 `v*` tag 触发 CI, 产物名与 checksums.txt 不变. 本卡实现若改动产物名或打包方式, 需同步改该脚本的 TAP_TARGETS.
- SELF_REVIEW: 已对照用户确认的计划逐条自检. 目标与产出一致 (语义版号 + tag 触发 + 首版 0.5.0 三项均已落到验收条件); 用户决策一栏只写用户实际确认过的四条, 未把「加 git describe 作为本地默认值」这类我方建议写成用户决策 (该项写在 EXPECTED_OUTCOME 与验收条件里, 属实现选择); 边界四类均已给出排除或纳入及理由, 且未把达成目标必需的调用方修改排除在外; 验收条件均可执行可判定, 覆盖六个文件的改动与端到端发版验证, 未引入越界要求. 自检中修正了两处: 补上了 `tag_name` 会产生 `vv0.5.0` 的易错点, 以及 `checksums.txt` 与产物名保持不变的回归条件.

## IMPLEMENTATION

- 2026-09-09 实现: 分支 `semver-release`, 基线 `aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78`, 首交付 `5172edad0c23c2f85265461d6238215f443849a9`. Version 注入 + tag 触发发版; 自检与 `go test ./... -count=1` (21 包) 均在该提交通过.
- 2026-09-09 评审与合入: 计划 `semver-release-cycle` / 批次 `semver-release-batch` 已 close. 有效结论 run: `semver-release-pm-3`, `semver-release-qa-3`. 失败 run `semver-release-pm-1`/`pm-2`/`qa-1`/`qa-2` 因缺结构化 findings, 已在 close 的 resolved_failures 绑定到成功 run. PM-01 与 QA-1 (同一注释问题) 已在 `211c104282f181d5fabe6cc49b03583b0bed269f` 机械修复. 最终交付 `211c104282f181d5fabe6cc49b03583b0bed269f` 已推入 `origin/develop`. 推送 annotated tag `v0.5.0`; Actions run 34315337784 成功. release tagName=`v0.5.0` (非 vv0.5.0), name=`kander 0.5.0`. latest 下载 GET 200; linux-amd64 二进制 `kander version` 输出 `kander 0.5.0`. checksums.txt 覆盖六平台产物. 本地 develop 另有 4 个未推送用户提交 (含 README/`scripts/release.sh`), 已 rebase 到 `origin/develop` 之上, 未随本卡推送.

## SUMMARY

- 实际产出: 版本身份改为可注入 `Version` (默认 `dev`); Release 仅由 `v*` tag 触发; 首个语义版本 `v0.5.0` 已发布. 最终交付 `211c104282f181d5fabe6cc49b03583b0bed269f` 在 `origin/develop`.
- 偏差: 无合同偏差. 本地 `make` 在已有 tag 的仓库注入 `git describe` 串 (可含 `v` 前缀); CI 剥离 `v` 后注入, 与 EXPECTED_OUTCOME 一致.
- 验收: 8/8 已满足. tag 页 https://github.com/dualface/kander/releases/tag/v0.5.0 ; latest 下载 GET 200; 发布二进制 `kander 0.5.0`.
- 未决 (按 run 引用, 均已 disposition rejected):
  - [PM][suggest] PM-N1 日常 push 不再有 CI; 状态 rejected; reviews/semver-release-pm-3/dispositions/semver-pm-n1-rejected.json
  - [QA][suggest] QA-2 与 PM-N1 同因; 状态 rejected; reviews/semver-release-qa-3/dispositions/semver-qa-2-rejected.json
  - [PM][suggest] PM-N2 本地/发布 `v` 前缀不一致; 状态 rejected; reviews/semver-release-pm-3/dispositions/semver-pm-n2-rejected.json
- 结论: 验收通过, 可完成.

## REVIEWS

- {"run_id":"semver-release-pm-1","batch_id":"semver-release-batch","role":"PM","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"5172edad0c23c2f85265461d6238215f443849a9","report":"reviews/semver-release-pm-1/report.md"}
- {"run_id":"semver-release-qa-1","batch_id":"semver-release-batch","role":"QA","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"5172edad0c23c2f85265461d6238215f443849a9","report":"reviews/semver-release-qa-1/report.md"}
- {"run_id":"semver-release-pm-2","batch_id":"semver-release-batch","role":"PM","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"5172edad0c23c2f85265461d6238215f443849a9","report":"reviews/semver-release-pm-2/report.md"}
- {"run_id":"semver-release-qa-2","batch_id":"semver-release-batch","role":"QA","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"5172edad0c23c2f85265461d6238215f443849a9","report":"reviews/semver-release-qa-2/report.md"}
- {"run_id":"semver-release-qa-3","batch_id":"semver-release-batch","role":"QA","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"96ddc888e30a399ddbf23b19d4818cfe3ac49ed1","report":"reviews/semver-release-qa-3/report.md"}
- {"run_id":"semver-release-pm-3","batch_id":"semver-release-batch","role":"PM","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"96ddc888e30a399ddbf23b19d4818cfe3ac49ed1","report":"reviews/semver-release-pm-3/report.md"}
