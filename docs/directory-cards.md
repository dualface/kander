# 目录卡、SIZE 与显式迁移

所有新卡都保存在 `kanban/<state>/<task-id>/spec.md`. `new` 写 SIZE: small, `new --large` 写 SIZE: large, 该行紧随 TYPE. 规模与路径形态分离, 不改变 kanban_agents 或 models.kanban 配置键.

small 在 spec.md 内保留 IMPLEMENTATION/SUMMARY, done 要求完成 SUMMARY; large 使用非空 report.md. 两种规模都能保存普通附件. todo 均要求 SELF_REVIEW; large 或任务组成员另外要求 CARD_REVIEW. SIZE 与契约一起冻结, 从 todo 退回 backlog 也不解冻; 明确用户决定下复用受控 contract-decision 更新入口.

公开快照中的 Entry.Kind 和 TaskSummary.kind、list、TUI、启动/恢复/通知模型参数均表示 SIZE. Entry.IsDirectory() 只表示物理形态; 包内结构扫描的 Kind 留空, attachSize 后才填规模. 缺 SIZE 的文件按 small 读取, 缺 SIZE 的目录按 large 读取且 check 提示 init; 非法或重复 SIZE 阻止变更. check 的默认 done/archived 排除范围不变.

## 维护步骤

1. 暂停全部执行端、外部编辑器、在途通知和归档写入; 旧 Agent 和旧二进制也必须停写. 保留卡片与会话, 不必终止 Agent.
2. 执行 `kander init`. 需要迁移且看板有 working/review 卡时默认拒绝并列出 ID. 确认上述停写条件后执行 `kander init --maintenance`.
3. 命令取得排他看板锁, 恢复 prepared 事务, 校验卡片与未知暂存物, 然后迁移七状态. 受控 init/new/move/update/归档及读取均遵循同一锁; 不受控外部写进程无法由该锁约束.
4. 成功后以 show --json 重新取得路径和 revision, 再恢复执行端. 不沿用迁移前快照. 变更继续通过 update/move 等专用入口.

`--maintenance` 只是操作方声明, 工具不推断存活状态, 不证明 Agent 已停止, 不自动退出或关闭会话. 保持停写直到迁移和恢复成功; 失败后保留现场排查. 不能用“最终打印新路径”替代维护窗口.

## 事务与恢复

init 通过 MigrateCards 进入恢复; 兼容入口 RecoverTransactions 与其共用同一恢复核心. 待恢复记录先于普通结构扫描处理. 无迁移时普通非卡片散落文件只警告并提示 check, 不阻断 init 的目录/exclude 初始化; 有迁移时结构问题明确阻断并提示 check. 缺 spec、真实重复、未知迁移产物及 reparse 仍报错保留, 不因迁移计数为零而放行.

复用 [卡片事务](card-transactions.md) 的看板锁、revision、操作 ID 和 prepared/committed 日志. 文件转目录分阶段执行:

1. 预检全部卡片, 保存整批 from/to 映射及各文档原文/补 SIZE 与链接调整后正文到一个持久记录. 相互引用的卡片不会分成可独立提交的迁移.
2. 建立同卷 `.kander/migrations/<operation-id>/<task-id>/` 暂存目录.
3. 将源 `.md` 改名为暂存目录内的 spec.md. 此时状态目录中暂时没有该 ID 的入口.
4. 在日志登记的 `spec.write-<operation-id>` 写入补 SIZE 与链接调整后正文; 中断后只接受 After 的精确前缀, 从已写位置继续并同步. 原 spec.md 先改名为登记的 `spec.original-<operation-id>`, 再发布完整替换; 仅在匹配原文时移除备份. 不依赖进程 defer 清理随机临时文件.
5. 将完整目录发布到原状态的 `<task-id>/`.
6. 提交 revision 和 committed 日志.

这些操作不是一次原子 rename. 正常读者被排他锁隔离; 进程被终止后, 读者依据 prepared 记录诊断待恢复事务, 不把它当普通缺卡或自动修复. init 根据源、暂存、目标的存在性及准确正文匹配继续完成; 双入口、异常正文、无记录暂存物、symlink/junction/reparse 均报错并保留现场. 已提交记录和空暂存父目录保留作诊断, 不通过忽略状态目录点前缀隐藏半成品.

既有目录缺 SIZE 时补 large; 其 spec.md 及目录内 Markdown 附件引用旧文件卡时同批调整地址. 二进制等普通附件不改写. reviews/dispatches 等专用生产者子树不参与普通链接扫描或迁移写入, 原审核/派发记录作为历史证据保持字节不变; 日志回放同样拒绝写入这些受管路径. 已有合法 SIZE 保留, 每张发生形态或正文变化的卡片增加一次 revision, 迁移计数包含只调整链接的卡片. 再次 init 返回迁移数 0, 不重写卡片内容和 mtime. 底层仍经过 internal/fs 的 POSIX no-follow 和 Windows 固定句柄/reparse/DACL 边界. 测试针对进程中断与重启, 不宣称任意硬件掉电保障.

## 相对链接

按引用方原位置解析 URL 路径, 再使用整批旧文件至新 spec.md 的映射转换目标, 最后相对引用方新位置生成地址. 因此双方都迁移、既有目录卡引用旧文件、引用看板外 README 等场景均能保持目标; 不一律添加 ../. 源代码和恢复校验共用此算法, prepared 记录保留完整映射, 不从半迁移现场重新猜测.

复用 Goldmark 的 CommonMark 解析, 只替换目标地址的源字节片段. 支持普通链接、图片、引用定义 (含未使用/重复定义)、角括号地址、转义/百分号编码、查询与片段. 链接文字、标题、CRLF、正文其他字节以及代码行内/围栏/缩进示例不变. 网页 URL、根路径 URL、纯锚点和纯查询引用保持原样. 仅扫描看板卡片内的 Markdown 文档, 不扫描或修改看板外仓库文件.

不支持语法只在引用方移动或可能目标属于迁移映射时要求人工处理. 不移动且目标不变的历史文档中的 Wiki 散文、反斜线路径、无效旧 URL 保留; 全为绝对 URL 的 srcset 保留. 真正需要重定位的 Wiki/HTML href/src/srcset、无效 URL 和反斜线路径不猜测改写: 在发布前报错给出文档与原因, 保留原文, 由操作方先转换为支持的 Markdown 链接再重试. 本轮不声称能修复旧版本已经 committed 的损坏链接; 无 link_relocation 标记的旧日志按原 SIZE-only 计划恢复, 新迁移计划带该标记并校验完整映射.

## 过渡读取

list/show/check/TUI/subscribe 继续读取旧文件, 不触发批量迁移. new 始终创建目录. 对旧文件的 update/move/pick/start/resume/notify/dismiss 以及生命周期回写在副作用前要求 init, 不允许用旧二进制绕过限制. guard-write 对同状态旧 .md 拼写及跨状态旧路径给出提示; 它仍不是原子写入入口. 写目录内部文件而该 ID 仍为同状态旧 `.md` 时, 提示暂停写入并先运行 init.

迁移失败注入及子进程 kill/restart 覆盖已有目录每份链接正文发布、全部链接正文发布、prepared、暂存目录、源移走、临时替换创建、同步、原文备份、替换发布、备份移除、SIZE 完成、目标发布、revision 和 committed. 未登记的旧原子写入残留及非前缀内容仍报冲突并保留, 不按文件名模式猜测归属. 并发测试验证维护锁阻塞 init、快照、update、归档/move, 释放后只产生串行提交或 revision 冲突. 原生 Windows 用例需在 Windows 执行; 交叉编译不能替代实机结果.
