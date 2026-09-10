# 实现计划

## 步骤

1. 从组分支创建任务分支与工作树,填入 `TASK_BRANCH`。
2. 新增 `OverlayPath` / 深合并 / 覆盖键校验;改造 `Load` 返回合并结果,导出 `LoadScope` 供写入与选项面板。
3. 合并前对作用域 `review_stages` 调用 `NormalizeReviewStages`。
4. TUI 选项面板与 doctor 保存/修复只读作用域配置;有覆盖文件时面板顶部提示。
5. `kander config` 非 JSON 输出覆盖文件绝对路径;三语 i18n。
6. 同步 `rules/KANDER-AGENTS.md`、`KANDER-KANBAN-RULES.md`、`AGENTS.md`、`docs/custom-agents.md`。
7. 按验收补齐定位、合并、报错、写入隔离、CLI/TUI 测试;运行 `go test ./...` 与 `go vet ./...`。
8. 变基到最新组分支,记录最终交付 SHA,`move review`。

## 受影响模块

- `internal/config`: 定位、合并、Load/LoadScope
- `internal/menu`: `kander config` / doctor CLI 测试
- `internal/tui`: 选项面板提示与保存隔离
- `internal/i18n`: 新增文案键
- 规则与文档: 路径表、优先级、`agents` 信任边界

## 验证

- 单元测试覆盖 Git / 非 Git 定位、深合并、禁止键与未知键、symlink / 非常规文件、写入隔离、旧平铺 `review_stages` 合并。
- CLI 测试覆盖 `kander config` 合并输出与覆盖路径行、doctor 修复不回写覆盖值。
- TUI 测试覆盖提示行与保存隔离。
- `go test ./...` 与 `go vet ./...`。

## 发布与回滚

- 交付到组分支;由编排方快进到组分支并安排审核。
- 回滚: 还原本卡提交即可;作用域 `config.json` 格式未变,覆盖文件缺失时行为与现状一致。
