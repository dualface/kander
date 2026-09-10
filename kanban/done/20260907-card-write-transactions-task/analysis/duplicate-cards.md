# 重复卡与事件订阅的关系

本补充针对同一任务 ID 同时存在于多个状态目录的实体副本。若重复只发生在界面，或两张卡的 ID 不同，需要另行定位。

2026-09-06 的项目记忆记录过 quicktui-mono 中卡片迁移后被按旧路径重新写出的情况。当时的一个诱因是 notify 在投递前迁移卡片。当前代码已经改为 notify 不迁卡，执行 Agent 自己进入 working，并增加 show 路径输出与 guard-write。因此不能把历史版本的 notify 预迁移逻辑直接当作当前事故根因。

## 当前可以证明的关系

1. Subscribe 不创建任务卡。它读取状态、探测存活并输出事件，不会因漏事件本身生成磁盘副本。
2. 旧路径写入可以直接产生副本。卡片迁移后原状态目录仍在，允许创建文件的写入会在旧位置重建同 ID 小卡。
3. 恢复流程也存在这一写入风险。window.WriteDocument 和 RestoreWindowText 传入缓存的 entry.Document，调用 fs.WriteTextAtomic(..., true)。这里 true 表示允许替换，目标缺失时仍可创建，并非“仅替换现存文件”。
4. NotifyViaResume 在启动或存活验证失败后，使用最初的 entry 和 originalText 回滚。如果执行端此前已经移动卡片，回滚可能把小卡重建到旧目录。是否为某次实际事故的触发点，还需该事故日志。
5. 重复副本反过来使 ScanTargets 报冲突，groupStateSnapshot 返回错误，订阅退出。这是错误数据导致订阅中断，不是订阅复制了卡片。

## 隔离复现

- TestAuditRollbackRecreatesMovedCard：先将 review 下的小卡迁移至 working，再以旧 entry 调用 RestoreWindowText。调用成功，两个目录均出现同 ID 卡片；ScanTargets 检出重复。
- TestAuditGuardCheckDoesNotCoverLaterMove：guard-write 在旧路径仍存在时允许操作；随后迁移卡片，最后编辑工具写旧路径，仍产生重复。这证明检查与写入之间存在时间窗口。

两个测试通过 race 检查，仅操作临时看板。源码：[重复卡复现附件](duplicate-reproductions.go.txt)。

## 护栏的边界

guard-write 是供宿主项目选择接入的写前检查；样例脚本只提取 tool_input.file_path，缺工具、字段或解析失败时放行，不覆盖任意 shell 写入。它也不会自动包住 Kander 内部的 Go 写入。

即使已接入，检查与实际写入之间仍可能发生迁移。因此“每次写前重新定位”和写前 hook 可以减少误写，但不能单独提供完整并发保证。

## 修复方向

修订卡片写入接口，区分创建新卡与更新现存卡。迁移、元数据回写、失败回滚应按任务 ID 进行统一并发控制，并校验预期状态/版本；过期操作必须报冲突，不能重建旧路径或覆盖新内容。所有参与写入的入口必须遵守这一协议；仅加一次 exists 检查仍保留竞态。

订阅/派回协议修复可减少错误恢复和过期判断；卡片唯一性仍需写入层独立保证。

证据：

- [历史记录](/home/dualf/works/kander/.memsearch/memory/2026-09-06.md:419)
- [内部写入与回滚](/home/dualf/works/kander/internal/window/window.go:59)
- [恢复失败回滚](/home/dualf/works/kander/internal/launch/notify_resume.go:99)
- [POSIX 原子写入](/home/dualf/works/kander/internal/fs/posix.go:368)
- [写前护栏](/home/dualf/works/kander/internal/board/guard.go:21)
