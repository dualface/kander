# 2026-09-08 首轮审核作者核实与交付

任务：20260907-probe-result-classification-task。
审核 base：4889d3fb93f8d2d639832b9da5588d668714d9bc。
审核 commit：96b59578954da4cc208684333f293d47bb4b2a0e。
最新交付 SHA / 当前组基线：96b59578954da4cc208684333f293d47bb4b2a0e。
任务分支：probe-result-classification，已正常推送 origin/probe-result-classification；本地 HEAD、远端任务跟踪引用、远端组跟踪引用三者相同，工作区干净。

## 分配与审核来源

本卡只处理 QA-S2（suggest）。QA-R1/S1 按派回信息由 P2 核实，本卡不代判断、不代改。主控提供的 PM（Codex）与 QA（Grok）首轮均针对上方 commit，均无 gate findings；CSA/Hacker 按项目特例 N/A。本卡未重新启动 Reviewer，不生成审核机器索引。原文完整保留于 [QA 原文](review-round1-qa-original.md) 与 [PM 原文](review-round1-pm-original.md)。

## QA-S2 作者结论：Rejected（本轮不修改）

审核描述的现象属实，已独立复现。处置为 Rejected，原因是该分支在审核 base 已存在且保持原样，本卡冻结契约第 2 项明确要求“保留窗口、会话、Agent 不匹配及 Codex 空引用不反查的现有语义”。本卡没有把这些分支新改成 stopped，也没有隐藏原有行为。对复查身份事实改用 unknown 或返回未验证可用的新地址，需要另行确认语义，不作为本轮冻结契约内的修复。

代码证据：commit 96b59578954da4cc208684333f293d47bb4b2a0e 的 internal/liveness/classify.go:69–77。有效唯一反查后，复查确认 pane 消失、Agent 或非空 session 不匹配时，返回 stopped、NewWindow 为空且只保留原地址原因；复查命令报错则走 :65–67 的 unknown 包装，正常复查通过则走 :93 的 drifted/NewWindow。

历史核对：分别 git show 4889d3fb93f8d2d639832b9da5588d668714d9bc:internal/liveness/classify.go 与 git show 96b59578954da4cc208684333f293d47bb4b2a0e:internal/liveness/classify.go，提取 `pane := paneProbe.Pane` 到读取 agent_status 之间的分支，Python assert 比较完全相等，输出：

```text
PASS: herdr recheck nil/agent/session branches byte-identical at base and reviewed HEAD.
```

运行核对：通过 Go overlay 加入临时测试，未写入仓库源码、未产生代码提交。每种场景都断言实际执行顺序为旧 pane get、pane list、新 pane get；覆盖消失、Agent 不匹配、session 不匹配、复查命令失败及正常匹配五种场景。测试使用临时目录与假 herdr，五场景 PASS；源码和原始输出附后，不能视作真实终端实机验证。

## 同步与验证

初始任务 HEAD 为 387d2362ee91662ef3cc998116c68debf5e08854。工作区和主工作区均干净；fetch 当前组与任务分支成功，然后在本卡工作区执行 git rebase origin/group/20260907-runtime-observation-group，同步到 96b59578954da4cc208684333f293d47bb4b2a0e。无冲突、无新增提交；git log origin/group/20260907-runtime-observation-group..HEAD 无输出。正常 push 成功（387d236..96b5957），未使用 force，未修改组分支或 develop。

在最终交付 HEAD 实际执行：

- go test ./...：全部通过，liveness 2.081s、probe 0.693s。
- go test -race ./internal/liveness ./internal/probe ./internal/notify ./internal/takeover：全部通过，依次 3.202s、2.005s、2.453s、1.078s。
- go build ./...：通过，无输出。
- go vet ./...：通过，无输出。
- git diff --check：通过，无输出。
- gofmt -l internal/liveness internal/probe internal/notify internal/takeover：无输出。
- git diff --exit-code origin/group/20260907-runtime-observation-group HEAD：通过，无差异。

## 未解决项与后续

- [QA-S2][suggest][Rejected] 唯一反查后的复查身份失败仍报告 stopped，缺少复查阶段说明且不返回 NewWindow。影响：list/get 之间 pane 变化时，操作者只能看到旧地址失效原因。理由：审核 base 已存在且本轮契约要求保留身份不匹配语义；现象已复现，建议保留给用户复核。若后续用户决定统一复查失败语义，再单独确认契约；本轮不创建或启动新任务。
- [验证缺口] 未执行原生 Windows 和真实 tmux/herdr/Agent 验证；本轮 Linux 自动测试与假 CLI 只证明相应覆盖场景，不等同实机通过。

本轮作者核实完成，待主控接收、完成本批门禁判断、集成及派回收尾。P3 和其他 todo 保持暂停。任务分支和工作区保留，RESULT 未写 completed。

## 临时复现命令与原始输出

```text
命令：go test -overlay /tmp/kander-p1-qa-s2-h360tqs2/overlay.json ./internal/liveness -run ^TestQAS2HerdrRecheckFacts$ -count=1 -v
=== RUN   TestQAS2HerdrRecheckFacts
=== RUN   TestQAS2HerdrRecheckFacts/gone
    lookup_test.go:226: status=stopped NewWindow="" detail="pane 不存在: w1:p1"; old get/list/new get confirmed
=== RUN   TestQAS2HerdrRecheckFacts/agent-mismatch
    lookup_test.go:226: status=stopped NewWindow="" detail="pane 不存在: w1:p1"; old get/list/new get confirmed
=== RUN   TestQAS2HerdrRecheckFacts/session-mismatch
    lookup_test.go:226: status=stopped NewWindow="" detail="pane 不存在: w1:p1"; old get/list/new get confirmed
=== RUN   TestQAS2HerdrRecheckFacts/probe-error
    lookup_test.go:226: status=unknown NewWindow="" detail="原地址失效: pane 不存在: w1:p1; 反查: 反查 pane 探测失败: pane 不存在: w9:p9: QA-S2 controlled probe error"; old get/list/new get confirmed
=== RUN   TestQAS2HerdrRecheckFacts/matching
    lookup_test.go:226: status=drifted NewWindow="herdr:w9:t9:w9:p9" detail="pane 不存在: w1:p1"; old get/list/new get confirmed
--- PASS: TestQAS2HerdrRecheckFacts (0.01s)
    --- PASS: TestQAS2HerdrRecheckFacts/gone (0.00s)
    --- PASS: TestQAS2HerdrRecheckFacts/agent-mismatch (0.00s)
    --- PASS: TestQAS2HerdrRecheckFacts/session-mismatch (0.00s)
    --- PASS: TestQAS2HerdrRecheckFacts/probe-error (0.00s)
    --- PASS: TestQAS2HerdrRecheckFacts/matching (0.00s)
PASS
ok  	github.com/dualface/kander/internal/liveness	0.019s
```

## 临时复现源码

```go

func TestQAS2HerdrRecheckFacts(t *testing.T) {
 for _, test := range []struct { mode, status, window string }{
  {"gone", Stopped, ""},
  {"agent-mismatch", Stopped, ""},
  {"session-mismatch", Stopped, ""},
  {"probe-error", Unknown, ""},
  {"matching", Drifted, "herdr:w9:t9:w9:p9"},
 } {
  t.Run(test.mode, func(t *testing.T) {
   resetLang(t)
   installPOSIXFakes(t, true)
   calls := filepath.Join(t.TempDir(), "calls")
   t.Setenv("QA_S2_CALLS", calls)
   t.Setenv("QA_S2_MODE", test.mode)
   script := `#!/bin/sh
printf '%s\n' "$*" >> "$QA_S2_CALLS"
if [ "$2" = list ]; then
 printf '%s\n' '{"result":{"panes":[{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"codex","agent_session":{"value":"wanted"}}]}}'
 exit 0
fi
if [ "$3" = w1:p1 ] || [ "$QA_S2_MODE" = gone ]; then
 printf '%s\n' '{"error":{"code":"pane_not_found","message":"gone"}}' >&2
 exit 1
fi
if [ "$QA_S2_MODE" = probe-error ]; then
 printf '%s\n' 'QA-S2 controlled probe error' >&2
 exit 1
fi
agent=codex
reference=wanted
if [ "$QA_S2_MODE" = agent-mismatch ]; then agent=claude; fi
if [ "$QA_S2_MODE" = session-mismatch ]; then reference=other; fi
printf '{"result":{"pane":{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"%s","agent_status":"idle","agent_session":{"value":"%s"}}}}\n' "$agent" "$reference"
`
   if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil { t.Fatal(err) }
   result := ClassifyTask(board.Entry{TaskID:"qa-s2"}, "- SESSION: codex wanted\n- WINDOW: herdr:w1:t1:w1:p1\n")
   if result.Status != test.status || result.NewWindow != test.window { t.Fatalf("report=%+v", result) }
   log, err := os.ReadFile(calls)
   if err != nil || string(log) != "pane get w1:p1\npane list\npane get w9:p9\n" { t.Fatalf("calls=%s error=%v", log, err) }
   if test.status == Stopped && (strings.Contains(result.Detail, "反查:") || !strings.Contains(result.Detail, "w1:p1")) { t.Fatalf("detail=%s", result.Detail) }
   if test.status == Unknown && (!strings.Contains(result.Detail, "反查:") || !strings.Contains(result.Detail, "QA-S2 controlled probe error")) { t.Fatalf("detail=%s", result.Detail) }
   t.Logf("status=%s NewWindow=%q detail=%q; old get/list/new get confirmed", result.Status, result.NewWindow, result.Detail)
  })
 }
}
```
