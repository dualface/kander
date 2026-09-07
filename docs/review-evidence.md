# 审核证据归档

不带 `--task` 的独立 review 保持原行为，不定位看板。带该选项时从目标 CWD 定位主看板，`KANBAN_DIR` 显式覆盖仍优先；不修改进程 CWD。

```text
kander review [agent] [--task <id>]...
  [--run-id <id>] --batch-id <id>
  [--previous-run-id <id>]
  [--requirements-file <absolute-json-path>]
  [--advance-file <absolute-json-path>]
  <CWD> <base> <commit> <role> <task-goal|absolute-spec-path>
  [review-context] [reviewed-commit]
```

选项全部在 CWD 前。重复 task ID 归一、排序并去重；其他选项不允许重复。run/batch ID 为 1–64 位小写 ASCII 字母、数字或连字符，首位只能字母或数字。run ID 可省略，随机生成并输出到 stderr；batch ID 由调用方提供。时间戳不承担身份或因果顺序。

创建意图及向未发布卡发布时均要求 working/review 目录卡；多卡属于同一非空任务组，语言相同。旧文件卡须按 init 维护协议迁移。长审核不占卡片锁，发布时按 ID 重定位。

## 批次与前驱

新 batch 的 requirements 文件须列出全部角色，值为 required 或带理由的 N/A：

```json
{
  "PM": "required",
  "QA": "required",
  "CSA": "N/A: repository rule",
  "Hacker": "N/A: repository rule"
}
```

调用方依照用户、项目规则和配置解析角色要求；该文件不代表工具能证明用户授权。成员、base、要求、任务上下文哈希及语言固定。PM/QA 同目标使用同 batch、不同 run。后续可省略 requirements；提供时须与既有要求一致。

修复不另开 batch。推进目标需要 advance 文件：

```json
{
  "previous_target": "<old-full-sha>",
  "target": "<new-full-sha>",
  "reason": "本批任务修复交付",
  "deliveries": {
    "<every-new-full-sha>": "20260907-example-task"
  }
}
```

review 验证 old 是 new 的祖先，old..new 每个提交都在映射中且归属本批成员；board 在组控制锁内 CAS 当前 target，保存旧/新目标、依据及版本。归属是调用方提供的事实，不承诺从任意代码推导业务归属；不得伪称组外交付为本批修复。存在执行中或未完整发布的本批 run 时拒绝推进。

增量轮同时传 previous-run-id 和 reviewed-commit；前驱须同 batch/base/role，commit 等于 reviewed-commit，且已完整发布。finding/disposition/闭批门禁归后续任务；本协议不根据报告中出现 PASS 自动放行。

## 原件与 schema

每卡保存：

```text
reviews/<run_id>/
  task-context.md
  review-context.md
  prompt.txt          # 准备成功时存在
  evidence.txt        # 准备成功时存在
  output.raw
  stdout.log
  error.log
  report.md           # 输出解码有效时存在
  sidecar.json
  manifest.json
```

raw/log 原样保存，包括非法 UTF-8。report.md 保存有效结果文本，不改写换行或内容；JSON Reviewer 原 JSON 单独保存为 output.raw。无有效报告时不造 report.md，索引指向 output.raw。失败证据可能包含空文件，sidecar 明确未启动或失败，空文件不表示成功。

sidecar schema 1 包含：

- run_id、batch_id、previous_run_id、task_ids、task_group、role。
- reviewer/model/effort、cwd/base/commit/reviewed_commit、适用的 advance。
- report_language、输入及所有原件 SHA-256、kander_version。
- phase、launch_status、execution_status、semantic_status、exit_code、failure_reason。
- created_at、finished_at、duration_ms。

launch_status 为 not_started、unknown 或 started。启动前先持久化 launching/unknown，启动成功后写 running/started；启动失败则最终记录 not_started。execution_status 准备时为 incomplete，最终为 ok、failed、not_started 或 interrupted。semantic_status 始终 unassessed；ok 只代表工具执行验证成功，不等于语义 PASS。

manifest schema 1 保存完整输入身份、sidecar 哈希和原件哈希。正文 `## REVIEWS` 每行是 `- {JSON}`，字段为 run_id、batch_id、role、execution_status、base、commit、previous_run_id、report。该区和 reviews 附件由专用发布器管理，不能通过 update 修改。清单和索引不从报告正文推导。

卡片 LANGUAGE 只在意图创建时解析，缺失才回落当时配置。冻结语言不随之后配置漂移；有 LANGUAGE 的卡与冻结值不一致时报告冲突。

## 持久化与恢复

board 复用 S 的事务、revision、锁和恢复日志。稳定控制记录位于：

```text
kanban/.kander/groups/00000000-review-archive-group/
  batches/<batch_id>.json
  runs/<run_id>/
    run.json
    inputs/
    staging/
    originals/
    sidecar.json
```

这是工具保留命名空间，不是看板任务组，不创建组卡。run.json 保存逐卡发布回执；每张卡独立持有完整原件和不可覆盖清单。哈希用于完整性检测，不防御任意同用户篡改。

按 run ID 的 OS 执行锁位于稳定 locks 目录，独立于卡片锁。持有期间只短暂进入 S 的看板、组、任务事务；没有代码反向取得执行锁，不形成循环。不同角色可并行，同一 run 的并发重试等待前次释放后读取结果。普通 update 及 working/review 之间的 move 可在长审核期间执行。终态迁移应等待发布完成；本归档协议不新增 done 门禁。若提前进入 done 等终态，原件仍在控制目录，发布失败且不重建旧路径；默认 check 跳过延后检查状态，须用 `kander check <task-id>` 或 `kander check --all` 定位未完成发布。done 卡不可移回复用，也不可绕过受控入口修补；后续处理须另行确认，不能以同 run 重试承诺自动修复终态。

输入与意图先事务落盘，输出写受控 staging。进程回收、worktree 检查、runtime 清理都有结论后，冻结 originals/sidecar；随后逐卡原子发布原件、清单、索引及回执。跨卡并非一个大事务，部分失败保留成功卡、逐卡报告、退出非零。重试验证成功卡且不重写，只补缺项。

同 run ID 不同输入或哈希冲突。同输入重试不再启动 Reviewer；可在 CLI 已移除或工作树已有新修改时重放原报告。首次仍要求 HEAD/clean/祖先关系。恢复调用仍要求原 Git 对象、输入字节和卡片绑定可验证。若原 spec 路径随卡移动或正文后来变化，可改传已归档 task-context.md 的绝对路径；身份比较基于原件字节，不要求复用失效路径。零卡发布时原输入仍在控制目录 inputs 中。

门禁被 kill 后 OS 执行锁释放；相同调用将尚未 finalized 的意图标记 interrupted，保留暂存输出，不伪造进程回收、清理或最终报告。恢复未确认遗留 Reviewer 是否仍在运行；不依据旧 PID 擅自杀进程，也不删除未知 runtime。异常 runtime 遗留应由操作者诊断处理。

S 多文件事务中断时读者先报告待恢复。暂停写入、满足维护条件后运行 init，再以同 run ID 重试。不得手工补索引、删除成功卡、重建旧路径或换 ID 绕过未完成发布。

所有写入经过 internal/fs，沿用 POSIX 私有权限与 Windows 创建时保护 DACL/reparse 拒绝。二进制原件通过日志 base64 字节字段无损恢复，普通文本日志格式兼容。

## 消费接口与检查

- `board.PrepareReviewRun` / `UpdateReviewRun` / `FinalizeReviewRun`：意图与执行事实。
- `board.PublishReviewRun`：逐卡发布并返回失败，不抹去成功回执。
- `board.ParseReviewIndexes` 和 ReviewInput/ReviewRun/ReviewBatch/ReviewManifest：共用类型；board 不依赖 review。
- `board.ReadReviewRun` / `ReadReviewOriginal`：读取已提交事实与校验原件。
- `board.ReviewPublicationComplete`：验证跨卡原件、清单、索引及前驱；不判断语义 PASS。
- `kander check`：同时检查意图和索引，零卡发布成功也不会消失。报告不完整发布、重复/缺失/冲突、哈希、语言、成员及前驱错误；保持原状态范围。

证据位于本机 kanban，不进入 Git，也不随临时报告清理而删除。
