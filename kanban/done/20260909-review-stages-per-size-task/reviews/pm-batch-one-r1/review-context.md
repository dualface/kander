Review focus:
(1) review_stages 两档结构:旧平铺兼容、混合形态报错、NormalizeReviewStages 导出、ReviewStageFor 替换全部平铺读取、Save 往返写出两档、doctor 缺档回填、TUI/flow/format 按档展示与折叠。
(2) .kander-config.json 覆盖:OverlayPath 的 Git 主工作树定位与非 Git 向上查找、校验前深合并语义(对象递归、标量/数组整体替换)、合并前对作用域 review_stages 规范化、禁止键与未知键报错含路径与键名、symlink/reparse 防护复用。
(3) 写入隔离:Save/Update/SaveIfUnchanged、doctor Repair、TUI 选项面板、安装向导以未合并的作用域配置(LoadScope)为基底,覆盖值不反向写入作用域 config.json;TUI 提示行与 kander config 覆盖路径输出。
(4) 文档与规则同步:rules/KANDER-REVIEW-RULES.md Review Stages 第 3 级按 SIZE 取值及混合规格按 large;rules/KANDER-AGENTS.md 路径表与优先级、agents 可执行路径的已接受风险说明;AGENTS.md、docs/custom-agents.md;三语 i18n 键齐全。
Verification records: 编排者在组工作树 HEAD 5631b184af859cd726f07bfa119e5378a5e3ded3 运行 `go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部通过,工作树无未提交/未跟踪文件。
Implementation note: 两张卡由执行 Agent(cursor)分别在任务分支实现,编排者未修改代码;请重点复查跨卡接缝(卡 2 依赖卡 1 导出的规范化函数)与半改状态。