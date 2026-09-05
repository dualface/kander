# Git 工作流规则

- 操作 Git 前检查当前工作树.
- 建任务 worktree 前还须检查主树. 保留用户改动.

## 分支与 worktree

- `main` 只从 `develop` 前进, 须用户明确确认; Agent 不自动推 `main`.
- 固定 `main` + `develop`: `main` 稳定, `develop` 唯一集成分支.

**初始化分支**

- 初始化: 有 `origin` 且用户未要求仅本地时先 fetch, 要求 `origin/main` 存在.
  - 仅缺 `origin/develop` 时创建远端分支:
    - 本地无 `develop` 则从最新 `origin/main` 创建.
    - 本地已有 `develop` 时, 须确认 `origin/main` 是其祖先.
    - 满足后普通 push 到 `origin/develop`; 不满足则停止并报告.
- 无 `origin` 或用户要求仅本地时, 要求本地 `main` 存在, 缺 `develop` 则从 `main` 创建.
- 缺 `main` 时停止并报告, 不猜替代分支或重写历史.

- 改文件任务使用独立任务分支及 `<仓库根目录>/worktrees/<task-name>/` 专用 worktree.

  `<task-name>` 同分支名, 短 kebab-case.

  禁用稳定分支、`develop` 或 detached `HEAD` 承载任务.

  已在本任务专用分支和 worktree 时复用.

- 有 `origin` 且用户未要求仅本地集成时, fetch 后从最新 `origin/develop` 建任务分支.

  fetch 失败则停止创建并报告.

  无 `origin` 或明确仅本地时从本地 `develop` 创建, 报告未同步远端.

- 上述基点适用于单卡. 已启用任务组模块时, 组分支从 `develop` 创建, 组内任务分支从组分支创建; 两层 worktree、交付与清理分工按 `KANDER-TASK-GROUP-RULES.md`「组集成分支」执行. 不通过此引用加载关闭的任务组模块.

## 本地改动保护

- 未提交改动均为用户资产, 含已暂存、未暂存、未跟踪文件.

  禁用 `git restore`, `git checkout --`, `git reset --hard`, `git clean` 或等效操作丢弃、覆盖、删除.

  无关改动也不例外.

- 主树有未提交改动而必须执行 rebase, merge, fast-forward, 切分支等要求干净工作树的操作时, 先 `git stash push --include-untracked`, 确认新 stash 已保存全部改动再操作.

  不混入任务提交.

- 操作后立即 `git stash pop --index`. 冲突时保留操作结果和原改动, 逐项解决并恢复原暂存状态. 确认全部恢复前禁删 stash、清理文件、宣告完成或离场; 无法无损恢复时停止并报告.

## 提交与 push

- 独立关注点完成并通过验证后单独提交, 不混无关改动.
- 任务分支有可写 `origin` 且用户未要求仅本地时, 每次提交后普通 push; 首次用 `git push -u origin <branch>`.
- 用户要求 push 时检查全部未提交、未 push 状态; 只提交本任务授权改动, 保留并报告其他改动.
- 无 `origin` 或明确仅本地时保留本地提交, 跳过 push 并报告. 有 `origin` 但不可访问、不可写或用户禁 push, 且未授权仅本地时, 保留任务分支和 worktree, 报告并停止集成.
- 不得将任务分支自动集成到源分支, 除非用户明确要求集成, 或用户已确认的任务计划明确包含该集成步骤. 已有授权不重复询问; 仅启用 Git 模块、建卡或启动 Agent 不构成集成授权.

## 集成与清理

- 集成前核对上述授权及用户、项目已有的 PR、验收和暂停要求. 未获授权或门禁未满足时保留分支和 worktree, 报告具体待处理事项; 看板单卡留在 `working/`, 任务组按组规则保留实际状态.
- 源分支是创建任务分支时明确记录的交付目标: 单卡为 `develop`, 组内任务分支为对应组分支, 组分支为 `develop`. 只能集成到该目标, 不因当前检出分支或临时 rebase 改变目标.

- 集成前在远端路径 fetch 源分支, 本地路径读取本地源分支, 检查是否需要 rebase 待交付分支并重新验证. fetch 失败时停止并报告.

  rebase 冲突时优先保留任务分支里的改动, 但不得直接替换全文件, 需按照功能点逐个解决冲突. 如果会重大变更, 要求用户确认.

  rebase 成功后, 仅专属任务分支可用 `--force-with-lease` 更新其已 push 的历史; lease 失败时停止并报告, 不覆盖远端改动.

**一次性审核门**

- 审核仅在审核模块启用或用户明确要求本次完整审核时, 按 `KANDER-REVIEW-RULES.md` 判断触发与执行. 其他情况记 N/A, 不加载关闭的审核模块.
- 适用审核是集成前的一次性门; 审核期间 base 冻结. 审核完成后因源分支前进而 rebase, 只重做验证; 仅用户明确要求或手动解决实质代码冲突时重审. 无冲突或冲突仅在 Markdown 文档时不重审. 任务组批次的 base 链与修复轮次遵从组级审核规则.
- 对任务组, 此门约束组分支合回 `develop`; 任务分支交付到组分支是组级审核的准备步骤, 不要求先完成组级审核.

**直接集成与 PR**

- 远端直接集成先普通推送完整提交: `git push origin <最终交付 commit>:refs/heads/<源分支>`, 成功前不推进本地源分支. 远端因并发前进而拒绝时, fetch 后将待交付分支 rebase 到最新 `origin/<源分支>`、重验并重试; 组内任务分支由原执行 Agent 处理, 主控只派回和接收.
- 推送成功后 fetch, 在源分支所属 worktree 执行 `git merge --ff-only origin/<源分支>`. 远端已包含记录的最终交付 commit 时也只做此同步, 不再次 rebase 或重复集成. 本地分支缺失时从该远端分支创建; 本地分叉、工作树或其他同步问题导致失败时保留现场并报告“远端已集成, 本地未同步”, 后续只处理同步问题, 不 reset 或丢弃本地提交.
- 无 `origin` 或明确仅本地时, 在源分支所属 worktree 执行 `git merge --ff-only <最终交付 commit>`. 若本地源分支已前进导致失败, 待交付分支 rebase 到最新本地源分支并重验后重试, 不 push.
- 直接集成和本地同步不生成 merge commit. `main`、`develop` 和组分支不得使用 `--force` 或 `--force-with-lease` 改写远端历史. 操作主树前后按「本地改动保护」保存和恢复用户改动.
- 用户或项目要求 PR 时遵从其审核、CI、合并方式和授权要求, 不以直接集成绕过. PR 已 merged、目标为记录的源分支且最终交付确实包含在合并结果中, 才算集成完成; squash/rebase 合并以这些证据确认, 不要求合并前 commit 仍为目标分支祖先.

**清理前置**

- 集成、验证、适用 push 和本地同步均成功后, 再核对最终交付已进入实际目标. 直接集成用 `git merge-base --is-ancestor <最终交付 commit> <origin/源分支或本地源分支>`, 使用集成 rebase 后的完整 SHA; PR 使用上述 merged 判据. 无法确认时保留现场并报告.
- 单卡满足前置后清理本卡任务分支与 worktree, 仅非本地路径清理远端任务分支. 组内卡交付到组分支后仍保留现场, 直到主控确认整组已进 `develop` 并发出收尾通知; 组 worktree 与组分支由主控在全组收尾后清理.
