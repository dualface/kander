# 公共输出解析结构交付报告

## 实际变更

- 新增 `internal/process` 的 `OutputSpec` / `LineCondition`、全集与子集校验/解析入口、占位符展开.
- 新增 `docs/output-parsing.md` 作为随仓库发布的权威描述.
- 更新 `AGENTS.md` 包表与文档索引.
- 未改 `internal/review`、`internal/terminal`、`internal/config`、`internal/launch`, 无新调用点.

## 最终提交

`116bd740953cce2040d13934038cb6601c01966a` on `20260909-process-output-parser`

基线: `group/20260908-agent-definition-group` @ `8abdfe2e6430a00e19383f84f23088a8ff743be0`

## 纸面验证

见 `plan.md`. Codex 原样文件、Claude/Cursor 的 `type`/`subtype`/`is_error`、Grok 的 `stopReason`, 以及带同名 `content` 的 ndjson 污染场景, 均可表达, 未扩展本结构.

## 验证

全部在 `116bd740953cce2040d13934038cb6601c01966a` 上重跑:

- `go test ./internal/process -count=1`: 52 pass / 0 fail
- `go test ./... -count=1`: 1305 pass / 0 fail
- `go vet ./...`: 通过
- `go build ./...`: 通过
- `GOOS=windows go build ./...`: 通过
- `git diff --stat` 相对组分支基线: 7 files, +1123/-2, 范围符合验收

## 偏差与未决

无. 调用方迁移与规则同步不在本卡. 组级评审与合入由编排器处理.

## 验收结论

本卡合同范围内的实现、测试与文档已交付, 待编排器接收任务分支.
