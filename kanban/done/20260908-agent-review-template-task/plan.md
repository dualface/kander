# 审核模板迁移计划

## 目标

把四个内置 reviewer 的 argv、环境、home、结果解析与提示词投递从 Go 按名分支迁到定义文件；`reviewers.<role>` 与 `kander review [agent]` 接受任何定义了 `args.review` 的 agent。输出解析只走 `internal/process` 的审核子集入口。

## 阶段

1. **定义字段与校验**
   - `AgentArgs.Review` 与 `AgentReview`（env/cwd/home_env/home_policy/output_name/inspection/spawns_helpers/snapshot_spec/path/stdin/prompt_files/output）。
   - 校验：cwd 与 home_policy 取值、stdin 与 `{instruction}` 互斥、prompt_files 路径与模板、占位符白名单、空元素、`ValidateReviewOutput`（拒 `stderr`）。
   - 错误信息带 agent 名与字段名。
   - 四份嵌入 JSON 写入与改动前一致的审核 argv/env/inspection/helpers/snapshot/output。
2. **执行路径去分支**
   - `agentSettingsFor` / `reviewerArguments` / `parseReviewOutput` / spec 快照 / stdout 路由 / home 缺失全部读定义字段。
   - 内置可执行名：`*_REVIEW_BIN` > 嵌入原始 `path`；自定义：`review.path` 缺省回落 `path`。
   - 未声明 success 时：退出码 0 且提取文本非空。
3. **候选名单**
   - 删除 `ReviewAgents`；config/doctor/选项面板/`ReviewModelLines` 改为「定义了 `args.review`」。
   - 仅有 start 模板的自定义 agent 写入 `reviewers.PM` 时校验失败；doctor 不改写该值。
4. **测试**
   - 四内置 argv/env/cwd/home/output/inspection/helpers/snapshot/stdin/prompt.txt 表驱动对比。
   - 成功条件先于提取；home 策略；自定义 file/stdout/regex/ndjson；stdin none + prompt_files 端到端。
   - 现有 review 包测试保持期望值。
5. **文档与发布规则**
   - `docs/custom-agents.md` 审核节改写并只链到 `docs/output-parsing.md`。
   - 同步 `KANDER-REVIEW-RULES.md`、`KANDER-KANBAN-RULES.md`、`KANDER-BASE-RULES.md` 中限定四个 reviewer 的句子。

## 影响模块

- `internal/config`（定义、校验、reviewer 名单）
- `internal/review`（执行、解析、校验）
- `internal/process`（argv 空值丢弃展开，复用已有模板与解析）
- `internal/menu`（选项面板与 doctor 候选）
- `docs/custom-agents.md` 与 `rules/` 三份发布规则

## 验证

- 针对性：`go test ./internal/config ./internal/process ./internal/review ./internal/menu`
- 全量：`go test ./...`、`go vet ./...`、`go build ./...`、`GOOS=windows go build ./...`
- `git diff --check` 与交付自检

## 发布与回滚

- 交付到组分支 `group/20260908-agent-definition-group`，不直接改 `develop`。
- 回滚：丢弃任务分支即可；定义文件与 Go 同提交，无单独数据迁移。
