# Options 项目配置实施计划

## 目标

按安装模式在 TUI Options 提供 Global / Project 入口：Global 写作用域 `config.json`，Project 只写 `.kander-config.json`，按键存在性展示继承并支持恢复。

## 步骤

1. `internal/config` 增加 overlay 写入目标解析、稀疏键读写、合并校验与 CAS 保存。
2. `internal/menu.Session` 双缓冲：切 tab 保留未保存编辑；保存只写当前目标；浏览与模型补全不生成覆盖。
3. TUI 增加 tab 栏、项目/覆盖/基础路径、继承前缀与恢复继承；Project tab 禁止 persistUI 写 scope。
4. 中英日文案经 `internal/i18n`；更新 released rules 与 `docs/custom-agents.md`。
5. 临时目录测试安装模式、继承/覆盖/恢复、保存隔离与窄屏路径；`go test ./...`。

## 受影响模块

- `internal/config`：overlay 位置与稀疏写入
- `internal/menu`：Session 双目标
- `internal/tui`：Options tabs 与继承展示
- `internal/i18n`：中英日
- `rules/KANDER-AGENTS.md`、`docs/custom-agents.md`、`AGENTS.md`

## 验证

- 单元测试覆盖键存在性（false / 空串 / 空数组 / 等于基础值的显式覆盖）、worktree 路径、空 overlay 不建文件、KANDER_CONFIG 不改写 overlay 目标。
- TUI 测试覆盖 tabs、切 tab 不保存、Project 保存隔离、继承前缀、保存失败保留编辑、窄屏。
- `go test ./...`

## 发布与回滚

任务分支 `tui-project-options` 从 `origin/develop` 检出，合入 `develop`。回滚即还原该分支提交。

## 风险

- persistUI / seedReviewRole 可能把有效值写进错误目标；Project 保存只写 overlay 稀疏键，进入审核区不写覆盖。
- 非 Git 无现有覆盖时以当前目录为创建目标，与向上查找的已有文件保持同一读写路径。


## 实施结果

- 交付 commit：`a20afa99042029137d1fd753a70735c81378f01b`
- PTY 真实终端验收：`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
- 后续修复：`dc478b74db628bd95efb7bc784350d641b16c44e`、`47d574090b21cb0f27c666d12340eec2fb09ae73`、`7c2e345c385be33087c3925ab4be18d11109a353`
- Delivery Self-Check 记录于 spec.md IMPLEMENTATION；`go test ./... -count=1 -timeout 240s` 于 `7c2e345c385be33087c3925ab4be18d11109a353`：1304 pass / 0 fail / 1 skip / 21 packages。
