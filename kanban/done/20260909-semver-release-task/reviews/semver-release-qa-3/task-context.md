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
- FINISHED_AT:
- TASK_BRANCH: semver-release
- RESULT:

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

- 2026-09-09: 任务分支 `semver-release`, 基线 `aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78` (`origin/develop`), 交付提交 `5172edad0c23c2f85265461d6238215f443849a9`. `internal/version` 只保留可注入 `Version` (默认 `dev`), `String()` 对空白回退 `dev`; 删除 `BuildTimestamp`/`GitHash`. Release workflow 改为 `on.push.tags: ['v*']`, 版本取 `${GITHUB_REF_NAME#v}`, `tag_name` 用 `github.ref_name`. Makefile / `make-windows.cmd` 只注入 `Version`; 有 tag 时用 `git describe --tags --always`, 无 tag 或非 git 目录回退 `dev`. `AGENTS.md` 包说明已同步. 未改调用方、README、`scripts/release.sh`.
- 交付自检 (commit `5172edad0c23c2f85265461d6238215f443849a9`):
  1. `git diff --check aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78 5172edad0c23c2f85265461d6238215f443849a9`: 干净.
  2. 本轮新增/改动文件均远低于 1000 行; 相对基线无净增超限 (最大 `AGENTS.md` 127 行未变).
  3. 行为说明已更新: `version.go` 注释与 `AGENTS.md` 包表; 无过期注释.
  4. 无死代码: 工作树内 `BuildTimestamp|GitHash` 已清零; `component` 已删除.
  5. 无冗余测试: 旧时间戳断言已替换为语义版号, 未重复覆盖同一行为.
  6. `go vet ./...` 与 `go test ./... -count=1` 在该提交通过, 21 个包 ok.
  7. 全量测试通过: 命令 `go test ./... -count=1`, 提交 `5172edad0c23c2f85265461d6238215f443849a9`, 21 个包.
- 补充验证: `make VERSION=0.5.0` 后 `./kander version` 输出 `kander 0.5.0`; 临时非 git 目录与无 tag 仓库的 describe 回退均为 `dev`. 本仓库已有旧时间戳 tag, 默认 `make` 会注入 `git describe` 结果 (符合「有 tag 用 describe」). 产物循环与改动前一致: 六平台, windows zip, 其余 tar.gz, `sha256sum -- * > checksums.txt`.
- 待闭环: 评审通过并合入 `develop` 后推送 `v0.5.0`, 再验证 release 页 tag 名与 latest 下载 URL.

## SUMMARY

