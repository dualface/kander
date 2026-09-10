# 第一组集成与本卡收尾

## 授权和独立核验

本轮 [编排端收尾通知](wrapup-notice.md) 明确授权本卡在组集成后自行清理并 done，无需重复确认。主控集成证据：`/home/dualf/.local/share/kander/orchestration/20260907-integrated-reliability/group1-integration.md`；闭批证据：`/home/dualf/.local/share/kander/orchestration/20260907-integrated-reliability/d-batch2-closed.md`。

执行端已独立 fetch，并分别运行 git merge-base --is-ancestor，全部 exit=0：

- `251f5d89186730ea372053a401136030710efe84` 包含于 origin/develop 和本地 develop。
- 本卡 `839f72119b22ce8b48811fc9819de9643a4028f8` 包含于最终组 HEAD、origin/develop 和本地 develop。
- 本地与远端 develop 均精确为 `251f5d89186730ea372053a401136030710efe84`；本卡删除前本地/远端任务分支均为 `839f72119b22ce8b48811fc9819de9643a4028f8`。

无重写映射：`839f72119b22ce8b48811fc9819de9643a4028f8 -> 839f72119b22ce8b48811fc9819de9643a4028f8`。未重新集成、未改 main 或组分支。主控报告在最终组 HEAD 执行 go test ./... 全部通过；该组级验证归主控证据，不冒称本轮由执行端重跑。

## 清理结果

清理前主工作树与任务工作树均干净，任务工作树连 ignored/untracked 检查也为空。所有删除命令从主工作树执行：git worktree remove 删除本卡工作树成功，git branch -d 删除本地 directory-card-form 成功，git push origin --delete directory-card-form 成功。随后确认工作树路径不存在、本地 ref 不存在、远端 ls-remote 无该分支。

主工作树保持 develop。组工作树与 group/20260907-review-archive-group 由主控暂留，本执行端未删除或修改；其他卡片和分支未动。交互 CLI 与终端保持，不自行 dismiss、退出或关闭。

## 审核批次与历史

第二批 base `a7fe54beb6ade00678e14655c0d385115eb950c8`。旧契约 Codex 首轮 70da00e 因用户明确 SIZE+相对链接授权改约而失效，原文仍保留。新契约 Claude/opus/high 首轮 d93f0b6 提出 finding，83f7354 增量复审 PM/QA 均无 gate，839f721 最后非阻断修复由主控按相同契约承接已通过结论。不能声称 Reviewer 实际审过 839f721。早期 Codex+opus 400 为参数失败，无语义结论。CSA/Hacker 按仓库 N/A。

所有原文与原作者逐项判断继续保留于 review-round-1*、review-new-contract*、review-incremental*。关于旧恢复顺序、旧 RecoverTransactions 已有校验的部分 Reviewer 断言被作者拒绝，调用链与回归反证见 review-new-contract-disposition.md，未将其改写成“本轮才前移恢复”。

## 保留限制和验证缺口

- [QA][low，接受] 零迁移普通散落目录仍拒绝 init；有效 prepared 先恢复、目录保留并指向 check。主体 medium 已闭合。
- [PM-106][suggest，保留] InitBoard 兼容包装直接委托统一入口，无重复恢复核心。
- [PM-109][suggest，保留] spec 叶模式遵循 umask，私有父目录保留 0700；未额外 chmod，未声称有跨用户可读漏洞。
- [验证][未执行] Windows 原生句柄、锁、junction、路径大小写与恢复测试缺环境；已有交叉编译证据不等同原生通过。

执行端交付阶段 go test ./...、build/vet、board race、格式/差异检查及 Windows 交叉编译已通过，详见 report-pre-wrapup.md。自检 10/11 完整，第 11 条原生执行保留缺口；按本轮已授权收尾完成。未迁移真实用户看板、未部署二进制，继续使用既有受控 S 写卡入口，不为新工具补造历史审核机器记录。

## 最终卡片状态

已通过受控 move 设置 RESULT completed 并进入 done；定向 kander check exit=0（ok: 1 tasks）。自身 Git 清理均完成，原文证据保留，交互终端保持。
