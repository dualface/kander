# 增量复审新增非阻断项：原作者处置

本轮审核 target `83f73544ef1f612a97f99d384e9386eeecb78752`，base `a7fe54beb6ade00678e14655c0d385115eb950c8`，已审 target d93f0b6；PM/QA 工具均 exit=0，均无 gate finding。PM 明确通过；QA-001 主体闭合、残留仅 low，不记为 medium 未闭合。完整原文见 [增量复审原文](review-incremental-findings.md)。以下为执行作者独立判断，旧 [PM-106/109 判断与断言纠正证据](review-new-contract-disposition.md) 原样保留。

## PM 新增项

- PM-111（low）：成立。83f7354 中 migrationStructure 不论 changes 均返回相同 message key，三个 locale 都预设“迁移已中止”，无迁移结构失败也如此。最小修复为三语统一使用 init 已中止，不改结构判据、错误码或 check 指引。提交 `839f72119b22ce8b48811fc9819de9643a4028f8`。
- PM-112（low）：成立。locale 已含末尾换行，Fprintln 再追加换行。改用 Stderr.WriteString，与既有 LoadBoard 一致。提交 `839f72119b22ce8b48811fc9819de9643a4028f8`。
- PM-113（low）：成立。83f7354 的 SplitAfter 不识别围栏，会将 SIZE 插在围栏内 # sample 后。改用已有 Goldmark 解析，只取顶层 ATX H1 的原源码行末作为缺 TYPE 时的锚点，代码/列表/引用不作锚点；无合适 H1 仍文首补行，不重排原文。新增精确输出覆盖 LF、CRLF、无末尾换行、反引号/波浪围栏、缩进代码、引用及无真实标题。提交 `151c141b54c6c692592fb4d0a32dc889cfdf9b43`。不扩展为其他 Markdown 重写或补造 TYPE。
- PM-114（recommend）：成立。专用 guard-write 判定表遗漏同状态旧文件卡的目录内路径提示，已补表行，目录迁移文档同步。提交 `839f72119b22ce8b48811fc9819de9643a4028f8`。

## QA 新增项（按原顺序保留）

1. low，QA-001 残留“普通非卡片目录在零迁移时仍拒绝 init”：观察成立，接受现状，不改代码。migrationStructure 只将普通非卡片正规文件判为无害；attachments/、备份目录不会满足 RegularFileExists，因此明确失败并保留目录。当前文档已经限定“普通非卡片散落文件”；扩大目录分类不是当前 must-fix。恢复本身先执行、check 指引存在、无 gate finding，保留较窄边界不妨碍本轮交付。此项仅为已接受的 low 限制，不称原 medium 未闭合。
2. suggest，“警告重复空行”：成立，与 PM-112 共用修复，保留独立角色引用。
3. suggest，“addSize 回归未钉住位置且与 guard 混合”：成立。已拆为独立 guard 用例及 SIZE 用例；SIZE 每例比较精确全文，追加到文末将失败。与 PM-113 同提交 `151c141b54c6c692592fb4d0a32dc889cfdf9b43`。

## 历史证据校正

本轮 Reviewer 表述“恢复已前置/前移”不应理解为本轮才改变恢复相对 scan 的顺序。此前 d93f0b6 的 migration.go 已先 applyRecord 再 scan，旧作者处置明确拒绝了“后续结构门禁使所有 prepared 都不能重做”的断言，并有事务 committed 后仍报告无关缺 spec 的实测。该证据继续保留。共享核心确实是上一轮新增，以消除重复实现；两件事不混淆。

## 验证与交付

有代码变化。go test ./...、go build ./...、go vet ./...、board race、gofmt 与 git diff --check 通过。Windows board 测试程序和完整二进制交叉编译通过；原生 Windows 未执行，缺口保留。全部测试用临时目录，真实看板未迁移，未部署二进制。

最终交付 `839f72119b22ce8b48811fc9819de9643a4028f8`，已正常推送，含 `151c141b54c6c692592fb4d0a32dc889cfdf9b43`。组基线 `83f73544ef1f612a97f99d384e9386eeecb78752`；fetch/rebase 无变化，之后全量测试再次通过。本地/远端任务分支一致，工作树干净。未改用户契约、未触发审核、未集成或清理；记录后 move review。PM/QA 对 `83f73544ef1f612a97f99d384e9386eeecb78752` 的无 gate 结论如实记录，不冒充已审本次新提交。
