Role: PM  
Commit: `21590e5c36d8c2f44e67fcf5cde76da4661d25bd`  
Task Context: [task-spec.md](/tmp/codex-review.1198037cf581726d8cfe65cbe17a6d28/task-spec.md)  
Reviewed Scope: 仅 `5169b6a..21590e5` 修复范围、直接影响及已有 finding 闭环。

**QA-004 仍部分修复；其余已有 finding 保持 closed。** 以下行为判断均为 Inferred，高置信度。

| Finding | 状态 | 目标提交证据 |
|---|---|---|
| PM-001 | closed | `internal/tui/options_form.go:266–285、334–343`：摘要、表单读取作用域 session。 |
| PM-002 | closed | `internal/tui/options_form.go:727–764`：比较最近应用值，恢复初始值仍应用、保存。 |
| PM-003 | closed | `internal/tui/options_panel.go:128–135、201–206`：overlay 校验失败设置 `loadErr`、清空旧 session。 |
| PM-004 | closed | `internal/tui/options_reload_test.go:236、252–261`：改盘前捕获旧结果，新结果应用后投递旧结果，检查身份及值。 |
| PM-005 | closed | `internal/tui/options_panel.go:138–143、212–215`：未初始化时清除语言绑定。 |
| QA-001 | closed | `internal/tui/options_panel.go:97–100、114–119、198–200`：结果携带加载序号，拒绝过期结果。 |
| QA-002 | closed | `internal/config/language.go:69–81、111–116`：缺省语言保留显式键及环境回退语义。 |
| QA-003 | closed | `internal/tui/options_form.go:736–764`：表单变化独立决定重建、保存。 |
| QA-004 | 部分修复 | `internal/tui/options_panel.go:124–144`：语言读取提前，但仍第二次读取作用域；`internal/config/language.go:84–104` 仍吞掉读取、解码及校验错误。 |

**PM-006 — medium — QA-004 的作用域语言快照修复未闭环**

Inferred，高置信度：本次仅将 `ConfiguredScopeLanguage()` 移到代理探测前，没有让语言使用首次读取结果。作用域已初始化、语言为 `ja`、环境为英语、overlay 未覆盖语言时，若首次 `LoadScope` 成功后、第二次作用域读取前，外部编辑使文件暂时成为无效 JSON，第二次读取错误仍变成空语言。session 加载继续成功，`loadErr` 为空，最终清除绑定并显示英语。

证据：`internal/tui/options_panel.go:124–144、201–215`；`internal/config/language.go:84–116、174–184`。违反 `task-spec.md:25、37` 的失败可见及语言使用本次加载结果要求。新增测试 `internal/tui/options_reload_test.go:398–420` 只在 session 构建阶段改盘，未覆盖仍存在的两次读取间隔。

最小修复：首次作用域读取同时保留显式语言及初始化状态，与已读 overlay 计算语言；删除该路径的二次作用域读取。

验收表仅列相对上轮的状态变化：

| 要求 | 预期行为 | 代码证据 | 状态 |
|---|---|---|---|
| `task-spec.md:25、37` | 有效语言使用本次读取结果，读取失败可见 | `internal/tui/options_panel.go:124–144`；`internal/config/language.go:84–116` | Complete 改为 Partial |

沿用上轮口径：Complete 6，Partial 1，Unverifiable 1；Missing、Contradicted 均为 0。

Observed：HEAD 匹配目标提交，工作树干净，修复范围 `git diff --check` 通过。Unverifiable：只读环境未独立运行测试；作者记录称目标提交 TUI 及全量测试通过。

NON-BLOCKING: none

Observed：已尝试删除任务文件，因 `Read-only file system` 失败。

```kander-findings
{"FINDINGS":[{"id":"PM-006","tier":"medium","text":"Inferred，高置信度：QA-004 的作用域语言快照修复仍不完整。本次只把 ConfiguredScopeLanguage 移到代理探测前，仍未使用首次 LoadScope 的读取结果。作用域已初始化、语言为 ja、环境为英语且 overlay 未覆盖语言时，首次读取成功后、第二次作用域读取前，外部编辑使文件暂时成为无效 JSON，二次读取错误仍被吞为空语言；session 加载成功、loadErr 为空，最终清除绑定并显示英语。违反 task-spec.md:25、37 的失败可见及语言使用本次加载结果要求。最小修复：首次作用域读取同时保留显式语言及初始化状态，与已读 overlay 计算语言，删除该路径的二次作用域读取。","evidence":"internal/tui/options_panel.go:124–144 首次 LoadScope 后仍调用 ConfiguredScopeLanguage；internal/config/language.go:84–116 再次读盘并将读取、解码、校验错误变为空语言；internal/tui/options_panel.go:201–215 成功路径清除空语言绑定；internal/config/language.go:174–184 随后回退环境。internal/tui/options_reload_test.go:398–420 仅覆盖 session 构建阶段改盘，未覆盖两次读取间隔。契约：/tmp/codex-review.1198037cf581726d8cfe65cbe17a6d28/task-spec.md:25、37。"}],"NON_BLOCKING":[]}
```