# Review gate

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 029c53b7-ef0e-474e-ad5b-bca704a9ea3e
- 窗口: herdr:wG:t1X:wG:p24
- 开始时间: 2026-09-04 11:08
- 完成时间: 2026-09-04 15:08
- 任务分支: 20260904-kander-review-task
- 结果: completed

## 任务目标

实现 `internal/review` 与 `kander review`, 对标 `onevoke_review.py` / `onevoke_review_roles.py`: 四 Agent 共用门禁, commit 校验, evidence, prompt 骨架, 超时监督, worktree 篡改检测, 增量 `reviewed-commit`.

## 用户决策

公开入口只有 `kander review`, 不再提供 `.sh`/`.cmd`/Python 旁路. 隔离参数不得放宽: Codex `--sandbox read-only --ephemeral`; Claude `--permission-mode plan --tools Read,Grep,Glob --safe-mode --no-session-persistence`; Grok `--sandbox read-only --no-memory --no-subagents`; Cursor `--print --output-format json --trust` 且 `CURSOR_*_DIR` 指向 runtime. 进程组收尽与 Cursor 父链放宽判定保持 onevoke 两档. 模型档位 环境变量 > 配置 > 内置默认, 配置失败回落默认不阻塞审核.

## 预期成果

主控与单卡执行 Agent 都能用 `kander review <CWD> <base> <commit> <role> ...` 跑完一轮门禁, 输出与 onevoke 审核契约兼容, 供 `REVIEW-RULES.md` 流程消费.

## 验收条件

- [x] 参数: `<agent>` 在已明确 reviewer 时由调用方传入, 未明确时 `kander review` 读配置选 Agent; 其余位置参数与 onevoke 一致, 含可选 review-context 与 reviewed-commit.
- [x] reviewed-commit 必须是完整 SHA, 在 base 之后且在 commit 之前; evidence 追加 FIX RANGE; prompt 改为只核实上轮 finding + 只审修复范围.
- [x] Reviewer 退出后对进程组 SIGKILL 并等到无非 zombie 成员; 默认档组非空即拒绝; Cursor 仅 detached 后代拒绝; 采样失败 fail-closed.
- [x] Windows runtime: 相对固定临时根 `CREATE_NEW` + 创建时 DACL; 根句柄不共享 WRITE/DELETE 持有到清理完成; 清理失败则审核失败.
- [x] worktree 闸门只看 Git 可见状态; `.memsearch/` 等忽略路径不参与.
- [x] 测试对标 `tests/test-codex-review.py` `test-claude-review.py` `test-grok-review.py` `test-cursor-review.py` `test-windows-review.py`, 用假二进制, 不打真 CLI.

## 威胁模型

Reviewer 是不可信进程, 必须只读隔离 (Cursor 以工作树闸门兜底). runtime 含 prompt 与 spec 快照, 仅当前用户可访问. 禁为统一 Windows 实现而交换各 Agent 隔离参数.

## 不在本轮范围

- 既有问题: 排除审核白名单是否触发 (那是 Agent 读 `REVIEW-RULES.md` 的决策, 不是本命令的门禁参数).
- 并发/跨平台/安全加固: 纳入本卡全部隔离、收尽与 runtime 句柄租约.
- 共享契约与文档: `REVIEW-RULES.md` 已由契约卡改为 `kander review`; 本卡不改审核档案与角色重点.
- 相邻功能: 排除 welcome 里的 reviewer 菜单; 排除看板 notify 派回. 使用 `internal/process` 与 `internal/fs`, 不复制.

## 讨论与决策

```text
前置任务: 20260904-kander-process-task, 20260904-kander-cli-install-task
```

- cli-install 已注册 `review` 子命令; 本卡实现 `internal/review`.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 工作区: `/home/dualf/works/kander/worktrees/20260904-kander-review-task`, 分支 `20260904-kander-review-task` 基于组集成分支创建.
- 实现 `internal/review` 并在 `cmd/kander/review.go` 空白导入注册 `kander review`; 未改冻结的 CLI 命令表.
- 隔离参数按 Agent 原样转发, 使用 `internal/process` 启动与 `internal/fs.CreatePrivateTempDir` 作为 runtime 租约; POSIX 进程组 SIGKILL 收尽, Cursor 用父链采样只拒 detached 后代; Windows Job Object + bootstrap 事件.
- 模型: env > 配置 > 内置默认; 配置损坏回落默认不阻塞. 未传 agent 时按角色读 `reviewers`.
- 验证: `go test ./...` 在 rebase 到组头后通过. POSIX 用假二进制覆盖 Codex/Claude/Grok/Cursor 拒绝/放行/增量/忽略路径. Windows 包 `GOOS=windows` 交叉编译通过; 无 origin, 未 push.
- 组集成分支: 修复提交 `4b8407c` 已 `git merge --ff-only` 进入 `group/20260904-migrate-to-go-group`.
- 上轮 finding (批次 8, base `931d251` / 当时 HEAD `83f97dc`):
  - QA-1 high / PM-3 (同根因): 成立. `Run` 曾在 SIGINT/SIGTERM 上直接返回, 跳过 `stopProcessTree` 与 runtime `Close`; Setsid 审核进程会留下. 现改为 `executeReview` 同步跑完再返回 `128+sig`; 单一 `Wait` 协程, 收尽前先收割 leader, 避免僵尸组误判. POSIX 用假 Claude sleep + SIGINT 断言 exit 130 且无 runtime 残留.
  - PM-1 medium: 成立. 增加 `windows_review_test.go` (`TestMain` 承接 Job bootstrap + 假 reviewer): 隔离 argv、runtime 租约、超时杀树、延迟篡改收尽、USERPROFILE 审核 home、argv 元字符经 bootstrap. 本机非 Windows, `GOOS=windows go test -c ./internal/review` 编译通过; 未跑真 Windows.
  - PM-2 medium [mechanical]: 成立. 删除未用的 `gitOutput`、`homeErrorName`、`eventKernel`、`var _ = time.Second`; unix `windowsTempRoot` 并入带 build tag 的 `reviewTempRoot`.
- 本轮 finding (增量, reviewed-commit `83f97dc` / 当时 HEAD `6d8fcc3`):
  - QA-2 medium: 成立. `TestWindowsUserProfileReviewHome` 只建了空 `%USERPROFILE%`, 未建 `.codex`; `validateContext` 对 Codex 要求 home 可读写, Windows `Stat` 失败即拒绝. 已 `Mkdir(profile/.codex)` 且保持 `CODEX_HOME` 为空. 本机仍只交叉编译测试包.

## 完成总结

- 交付: `kander review [agent] <CWD> <base> <commit> <role> <task-goal|spec> [review-context] [reviewed-commit]` 可用; 输出契约与 onevoke 兼容.
- 验收: 6/6 自检通过 (参数与配置选 Agent, 增量 reviewed-commit/FIX RANGE, POSIX 两档收尽, Windows runtime 租约走 fs, Git 可见闸门, 假二进制测试).
- 验证: 本轮 `go test ./internal/review` 与 `GOOS=windows go test -c ./internal/review` 通过. 无 origin 故任务分支未 push.
- 审核: 组级批次 8, 首轮 + 两轮增量. PM/QA 均通过; CSA/Hacker 按仓库 AGENTS.md 为 N/A. 修复轮次: QA-1/PM-3 中断收尽 (`6d8fcc3`), PM-1 Windows 假二进制测试与 PM-2 死代码, QA-2 USERPROFILE `.codex` 目录 (`4b8407c`). 归属本卡未处理项无.
- 收尾: 最终 commit `4b8407c20f47685d0502945a7d12d8a473e97257` 已是 `develop` (`bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`) 祖先. 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回本地 `develop`; 无 origin 未 push. 记忆合并空操作 (源无 `.memsearch/memory`). 已删本卡 worktree 与本地任务分支; 组分支未删.
