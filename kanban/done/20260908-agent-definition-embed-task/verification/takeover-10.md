# 接管核验记录（epoch 10）

- 核验者：Codex；日期：2026-09-09。
- 最终提交：`b216ca25cb328300b3a45c54bc00a5ca442a89d7`；直接父提交及本轮组基线：`d97964c06adb942b94b25c7e5c5bfb04795172e4`。
- 本轮只增加或加强测试与 JSON 测试固件；生产代码与已发布规则不变。

## 命令与结果

- `go test -json ./...`：退出码 0；21 个包通过；测试及子测试 1459 项通过、1 项跳过、0 项失败。跳过 `TestWindowsConsoleLauncher`，原生 Windows 测试不在本卡范围。执行时工作树内容与最终提交一致。原始日志 `/tmp/kander-embed-takeover-final-tests.jsonl`。
- `go build ./...`：退出码 0。
- `go vet ./...`：退出码 0。
- `GOOS=windows go build ./...`：退出码 0。
- `git diff --check d97964c06adb942b94b25c7e5c5bfb04795172e4 b216ca25cb328300b3a45c54bc00a5ca442a89d7`：退出码 0，无输出。
- `git status --porcelain`：提交后无输出。
- `git merge-base --is-ancestor d97964c06adb942b94b25c7e5c5bfb04795172e4 b216ca25cb328300b3a45c54bc00a5ca442a89d7`：退出码 0，可直接快进交付组分支。
- `git push origin agent-definition-embed`：成功，`d97964c..b216ca2`。
- 组全区间 `git diff --check 8abdfe2e6430a00e19383f84f23088a8ff743be0 d97964c06adb942b94b25c7e5c5bfb04795172e4`：检出 `internal/review/definition_unix_test.go:353: new blank line at EOF.`；该文件由 A2 交付，本轮未修改。不能据本轮干净宣称整个组差异干净。

## 基线兼容核验

从 `8abdfe2e6430a00e19383f84f23088a8ff743be0` 的 Git archive 创建一次性临时基线，只用于读取和运行隔离测试；未创建或改动任何组分支。

- 基线的 `Validate(minimalPayload(nil))` 输出形成 `internal/config/testdata/minimal-cursor-config.json`，现有 round-trip 测试增加逐字节基线比较，保留原断言。
- 同一 `TestBuiltinIntegrationPreservesTargetsAndBytes` 在基线与本轮树均通过：四个内置 agent × 全局/项目 × 新建/追加，共 16 组；每组检查完整目标路径、完整写入内容、识别结果及两次重复执行不改字节。
- 分别构建基线与本轮 CLI，以临时 HOME、临时配置、空 PATH 运行 `config --json`。默认配置与既有 minimal Cursor 配置两种输出均逐字节相同。
- 同样空 PATH 下，临时空看板 `init`、`list` 均成功，未要求安装四个 CLI。首次误传 `init <project-path>` 同时设置 KANBAN_DIR 被工具拒绝；改为仅 `init` 后通过，未绕过拒绝。
- CLI 比较原始输出位于 `/tmp/kander-embed-cli-op6a5q7d/`；本记录保留结论，不把本机临时目录写入仓库。

## 交付自查

1. 本轮差异无冲突标记、尾空白或 EOF 漂移；上述组全区间既有空行单列交编排器。
2. 本轮新增三个 Go 测试文件分别 66、81、68 行；修改的代码文件均未跨越 1000 行，最大为 `internal/notify/notify_test.go` 530 行。
3. 生产行为、文档与注释未发生需要同步的改变。新增注释均为英文。
4. 新测试均有调用入口，无新增未使用生产接口。
5. 新断言补足具体契约证据：完整基线字节、真实接管入口回滚、就绪前禁止投递、投递早于会话发现、两个 launcher 的内置 argv 与直接 notify 行为。保留原测试期望；未增加全屏快照。
6. 受影响 config/install/launch/notify 的定向与全包测试通过；命令与最终提交见上。
7. 全量通过数见上，不把测试通过等同于组审核完成。

## 已有处置复核

- PM-01 / QA-01 同根因：`internal/launch/agent.go` 的 durable 不确定投递短路排除 pane；三种失败返回统一关闭路径，接管调用方恢复原文。原作者记录保留，本轮通过实际接管失败测试补证，不重开已核验 finding。
- PM-02：自定义 path+dialect 包装器不继承审核资格；PM-09：审核 effort 随审核模型键显隐。现有 config/menu 测试通过，原作者记录保留。
- PM-10：文档成对声明契约与 `agents_review_test.go` 在 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 已一致；测试保留 Env/Inspection/HomeEnv 不填充断言。原 `2361d6e` 经同期交付映射到 `b653e81` 与 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，无需伪造或改写旧作者记录。
- PM-06：维持既有 rejected（low）。空 rules_target 不集成，RulesSpec 未给自定义名继承目标；该取舍仍列于未解决项供用户复核。
- 当前计划 `agent-def-embed-cycle` 为 pending，含 unsealed-plan 与 agent-def-batch-one。QA 最新 `qa-agent-def-b1-r7` 的执行错误为 unknown model id，语义 unassessed；需要编排器修正审核调用并完成既有计划。

- 返回 review 前运行 `kander check 20260908-agent-definition-embed-task`：退出码 0，`ok: 1 tasks`；liveness=alive。本地任务分支与远程任务分支均为最终交付 SHA，组分支仍为 d97964c，工作树干净。
