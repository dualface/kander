# 接管验证证据

最终提交：`df567055d3d884c9acb4b2d5bec0a926b87b1a5a`。

## 验证边界

只修改 process 解析器及其回归测试、权威文档。没有新增进程调用、文件操作或数据迁移；没有改动组分支。

## 初次最终提交全量测试

`go test -json ./... -count=1`：退出 1；1442 pass / 1 fail / 1 skip（含子测试）。

失败输出：

```text
=== RUN   TestSubscriptionDispatchDeadlineDoesNotWaitForProbe
    subscribe_dispatch_runtime_test.go:62: open /tmp/TestSubscriptionDispatchDeadlineDoesNotWaitForProbe287308831/003/events: no such file or directory
--- FAIL: TestSubscriptionDispatchDeadlineDoesNotWaitForProbe (0.35s)
```

同一最终提交执行 `go test ./internal/liveness -run '^TestSubscriptionDispatchDeadlineDoesNotWaitForProbe$' -count=5`：退出 0，5 次通过。`git diff f5dc7c31c665c347b63bb8fd389822197371118a HEAD -- internal/liveness` 为空；没有修改或放宽该测试。疑似调度时序问题，但未确定根因，不能把失败记录改写成通过。

## 构建

- `go build ./...`：退出 0。
- `go vet ./...`：退出 0。
- `GOOS=windows go build ./...`：退出 0；未宣称 Windows 原生运行已验证。

## 交付自检

1. `git diff --check d97964c06adb942b94b25c7e5c5bfb04795172e4 HEAD` 无输出，退出 0；工作树干净。
2. `wc -l`：output.go 437、output_test.go 413；新增修改代码文件均不超过 1000 行。
3. docs/output-parsing.md 同步空 select 与非法 equals 的校验约束；没有修改 README 或调用方文档。
4. 新增实现仅两个现有校验分支，无新增未调用函数/状态；旧 ReviewSources、TerminalSources、jsonStringOrRaw 引用搜索为零。
5. 新用例分别验证直接 Go 值校验、JSON 解码保留显式空 select、合法 JSON 类型不被拒绝，以及 absent 和 equals 必须在同一行满足；未删除或放宽测试。
6. 最终提交 process 测试在初次全量运行中 74 pass / 0 fail（含子测试）；最终全量复跑结果附后。
7. 所有全量结论绑定精确提交、命令与计数；失败结果独立保留。

## 最终全量复跑

`go test -json -p 2 ./... -count=1`：退出 0；1443 pass / 0 fail / 1 skip（含子测试），顶层测试 796 pass / 0 fail / 1 skip，21 个包通过。process 为 74 pass / 0 fail（含子测试）。该次只降低包并行度，没有跳过失败测试或放宽断言。

最终本地与远端任务分支均为 `df567055d3d884c9acb4b2d5bec0a926b87b1a5a`；`git ls-remote` 核实远端组分支仍为 `d97964c06adb942b94b25c7e5c5bfb04795172e4`。新提交等待编排器接收。
