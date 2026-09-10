# 公共输出解析结构实施计划

## 范围

在 `internal/process` 新增声明式输出解析与占位符展开, 纯新增, 不改现有调用方.
文档落点 `docs/output-parsing.md`, `AGENTS.md` 包表与文档索引追加一条.

## 纸面验证: 四个内置 reviewer 与污染场景

下表用本结构表达现有解析与成功判定. 实际迁移不在本卡.
单元格过长时换行. 结论: 全部可表达, 不扩展结构.

| 场景 | 现有行为 | 本结构表达 | 可否表达 |
| --- | --- | --- | --- |
| Codex | 读结果文件原样; 文本非空才算完成 (`args.go` 读 `outputFile`) | `source=file`, `parse=raw`; 不声明 `success`. 文本非空由调用方兜底 | 是 |
| Claude | 读结果文件 JSON; `type=result` 且 `subtype=success` 且 `is_error=false`, 取 `result` 字符串 | `source=file`, `parse=json_field:result`, `success=[{json_field:type,equals:"result"},{json_field:subtype,equals:"success"},{json_field:is_error,equals:false}]` | 是 |
| Cursor | 与 Claude 相同 (`type`/`subtype`/`is_error` + `result`) | 同上 | 是 |
| Grok | 读结果文件 JSON; `stopReason=end_turn`, 取 `text` | `source=file`, `parse=json_field:text`, `success=[{json_field:stopReason,equals:"end_turn"}]` | 是 |
| ndjson 污染 | 同名 `content` 的 assistant / tool / meta 行混流 | `source=stdout`, `format=ndjson`, `select=[{json_field:role,equals:"assistant"}]`, `parse=json_field:content`, `join="\n"` (缺省). 只有 assistant 行进入提取 | 是 |

四个内置 reviewer 均保持 `format` 缺省 `json`, 不声明 `select`/`join`.
Codex 的非零退出不在本结构内, 由调用方退出码兜底.

Claude/Cursor/Grok 的"提取后文本非空"同样是调用方兜底, 不是行条件.

### 污染场景输入 (必须同时带 content)

```text
{"role":"assistant","content":"# Title"}
{"role":"tool","content":"TOOL_BODY"}
{"role":"assistant","content":"## Section"}
{"role":"meta","type":"session.resume_hint","content":"To resume this session: ..."}
```

期望提取: `# Title\n## Section`. `TOOL_BODY` 与 resume hint 不得出现.

## 模块与入口

- `OutputSpec` / `LineCondition`: 结构体与 JSON 标签
- `ValidateOutput` / `ParseOutput`: 全集 (`stdout|stderr|file`)
- `ValidateReviewOutput` / `ParseReviewOutput`: 审核子集, 拒绝 `stderr`
- `ValidateTerminalOutput` / `ParseTerminalOutput`: 终端子集, 拒绝 `file`
- `ValidateTemplate` / `ExpandTemplate` / `ValidateArgv`: 占位符白名单与 `{{`/`}}`
- 本卡不读文件、不启动进程; `source` 只声明来源, 文本由调用方传入

## 实施步骤

1. 类型、校验、子集入口
2. json / ndjson 求值 (切分 -> 筛选 -> 提取 -> 合并; 成功判定先行)
3. 占位符与转义
4. 单元测试覆盖全部验收项
5. `docs/output-parsing.md` 与 `AGENTS.md`

## 验证

- `go test ./internal/process`
- `go test ./...`
- `go vet ./...`
- `go build ./...`
- `GOOS=windows go build ./...`
- `git diff --stat` 只含 `internal/process`, `docs/output-parsing.md`, `AGENTS.md`

## 发布与回滚

交付到组分支 `group/20260908-agent-definition-group`.
回滚: 丢弃本任务分支即可, 无调用方, 无数据迁移.
