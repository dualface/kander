# 目录卡、SIZE 与相对链接修订交付

## 交付与历史

- 最终提交：`d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`；任务分支 `directory-card-form`，本地/远端一致，已正常推送。
- 包含 PM-001 修复 `29ada01a241bb7a7734972a913e22f3dad52756c`；最新组基线 `70da00edfff664ec1c0740a49f6ea3ae51b42c8c`，fetch/rebase 无变化，随后全量测试通过。
- 工作树：`/home/dualf/works/kander/worktrees/directory-card-form`，干净并保留；组分支/develop 未修改。
- 用户明确授权 SIZE 与相对链接一并调整。原/新契约及原话由受控 update 的 CONTRACT_DECISIONS 保存；[修订计划](link-migration-plan.md)。
- [原交付快照](report-initial-70da00e.md)、[PM/QA 首轮完整原文](review-round-1-findings.md)、[作者逐角色处置与修复证据](review-round-1.md) 均保留。旧报告“相对链接原样保留/无已知实现缺陷”已由后续复现和修复纠正，旧审核不继承为通过。

## 11 条验收自检

1. 通过：使用 S 排他维护锁和日志；working/review 默认要求停写确认，保留现有维护窗口与不自动终止 Agent 的边界。
2. 通过：new 统一目录，默认 small、--large 为 large，SIZE 紧随 TYPE，完成门禁按规模。
3. 通过：Entry.Kind/TaskSummary.kind、list/TUI、Agent/模型/prompt/门禁按 SIZE，旧形态回退与非法值拒绝保留。
4. 通过：SELF_REVIEW、large/组成员 CARD_REVIEW 与 SIZE 冻结继续复用 S。
5. 通过：七状态迁移，补 SIZE，并只调整保持目标所需的 Markdown 地址；其余正文、链接文字/标题、CRLF 与附件保持。真实文件验证双方迁移、既有目录反向引用、README、图片、报告引用。二次 init 返回 0，内容与 mtime 不变。
6. 通过：完整路径映射与所有受影响正文同批持久化；已有目录用 files，旧文件用 migrations，prepared 状态阻止读取半成品；恢复严格重算并验证 After，不从半迁移现场猜测映射。
7. 通过：PM-001 受管临时输出/原文备份修复保留。原 12 个边界回归，加已有目录单份/全部链接正文发布，共 14 个失败及真实子进程 kill/restart 边界；另测第二张卡发布后恢复、部分写入、未知产物与篡改 After 拒绝。
8. 通过：维护锁/并发/订阅既有回归通过；兼容旧文件、缺 SIZE 目录、中文 token 和旧 schema。新可选 link_relocation 标记区分新链接计划，旧日志仍按原记录语义恢复。
9. 通过：旧文件只读过渡、副作用前拒绝与 check 原状态范围回归通过。
10. 通过：guard-write、受控更新入口及外部竞态边界保持。
11. 部分：规则、README、中文说明和三语错误资源同步；全部构建/测试/静态检查通过。Windows 用例已交叉编译，原生句柄/锁/junction/路径大小写及恢复未执行，缺环境。

自检 10/11 完整通过，第 11 条原生 Windows 验证缺口保留。

## 验证

- `go test ./...`：通过，rebase 后再次通过。
- `go build ./...`、`go vet ./...`：通过。
- `go test -race ./internal/board ./internal/fs ./internal/liveness`：通过。
- gofmt、`git diff --check`：通过。
- Windows/amd64 board 测试程序及 cmd/kander 二进制：交叉编译通过；主机 Linux，无原生 Windows/Wine，不能视作实机验证。
- 所有迁移使用测试临时目录；未迁移真实看板或部署二进制。卡片记录继续使用已接收 S 基线构建的受控 show/update/move 入口。

## 语法与兼容边界

CommonMark 普通链接、图片、引用定义 (含未使用/重复定义)、角括号路径、转义/百分号编码和查询/片段受支持。代码示例、网页 URL、根路径 URL、纯页内锚点/查询不改写。仅扫描卡片内 Markdown 文档，不扫描改写看板外仓库文件。

Wiki 链接、HTML srcset、需重定位的 HTML href/src、无效 URL 或非跨平台反斜线路径在预检报错并保留内容，要求先转换为支持语法，不静默损坏。无新标记的旧迁移日志按原 SIZE-only 计划恢复；不声称修复旧版本已经 committed 的损坏链接。

## 审核与后续

PM-001、PM-002、QA-001 均由作者确认有效并修复自测；PM-002/QA-001 同根因计一次，但原角色引用全部保留。编排端按更新后的完整 D 契约重新启动本批 PM/QA 首轮，审核 base 仍为 `a7fe54beb6ade00678e14655c0d385115eb950c8`。CSA/Hacker N/A。执行端未触发审核或集成，分支与工作树保留，交付后 move review。

剩余验证缺口 1 项：Windows 原生执行，需在 Windows 跑同套测试。未将环境缺口或尚未开始的新契约审核记为通过。
