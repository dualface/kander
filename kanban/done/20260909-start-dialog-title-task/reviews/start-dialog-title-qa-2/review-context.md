Review focus: (1) 标题是否按 phase 与记录的成败在当帧切换，且不含任务 ID；(2) 启动语义、异步预览、选卡丢弃、滚轮与结果关闭是否保持不变；(3) 三语文案键一致，结束态不解析 dialog.message。
Verification records: go test ./internal/tui ./internal/i18n -count=1 于 c54c6d95d3080c38bd57ab02bbd95b2fca63da98 通过；go test ./... -count=1 于同一提交通过，用例 1270。
Environment gaps: 无。
Mandatory structured output: after the analysis, emit exactly one fenced block named kander-findings with JSON fields FINDINGS and NON_BLOCKING. Use empty arrays when there are no items. A report without this block fails validation. Example:
```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```