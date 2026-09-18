# 卡片事务、受控更新与崩溃恢复

本文档定义 Kander 的任务卡存储模型、文件级事务协议、锁排序层级以及崩溃恢复保证。

任务的唯一身份标识为 `task_id`；文件路径仅为运行时寻址结果。`internal/board` 始终在持锁状态下重定位路径，绝不在异步操作后使用过期的缓存路径进行写入。

---

## 1. 核心原则与事务命令

所有任务卡变更必须通过受控 CLI 命令或 `board.WithTransaction` Go API 执行：

```sh
# 查看详情与文档更新
kander show --json <task-id>
kander update <task-id> --document spec.md --file <UTF8-文件> --expect-revision <版本号>
kander update <task-id> --document plan.md --file <UTF8-文件> --expect-revision <版本号>

# 生命周期状态流转
kander move <task-id> working --owner <agent>
kander move <task-id> done --result completed
kander move <task-id> archived --result cancelled --reason <原因> --decision <决策引用>
kander move <task-id> trash --result trashed --reason <原因> --decision <决策引用>
```

### 核心约束

1. **目录卡片结构**：所有卡片均以目录形式存在，内含 `spec.md` 及可选附件。
2. **乐观并发控制（CAS）**：修改卡片必须传入 `--expect-revision`。每次成功写入将版本号严格自增 1。
3. **状态唯一事实源**：任务状态完全且仅由其所在的物理目录决定（`backlog/`、`todo/`、`working/`、`review/`、`done/`、`archived/`、`trash/`）。
4. **合约冻结机制**：卡片移出 `todo` 之后，任务规模（SIZE）、任务组归属及核心契约被严格冻结，如需修改必须通过 `--contract-decision-file` 记录显式决策。

---

## 2. 锁层级结构与隔离性

为杜绝并发多任务操作下的死锁，锁必须按严格、确定的顺序依次获取：

```text
1. board.lock          (全局板锁：读写共享，新建/移动/恢复时独占)
       |
       v
2. group locks         (组锁：按 group ID 字典序依次获取)
       |
       v
3. task locks          (任务锁：按 task ID 字典序依次获取)
       |
       v
4. journal.lock        (日志锁：极短时间独占，保护 pending->committed 重命名与清理)
```

### 控制文件布局（`kanban/.kander/`）

| 路径 | 用途 |
|---|---|
| `locks/board.lock` | 全局看板互斥锁（POSIX `flock` / Windows `LockFileEx`）。 |
| `locks/<task-id>.lock` | 每个任务独立的句柄；读共享，写独占。 |
| `locks/journal.lock` | 保护预写日志原子发布与日志目录分区。 |
| `versions/<task-id>.json` | 记录 `{revision, operation_id, contract_frozen}`。 |
| `operations/pending/` | 存放未提交的持久化事务预写记录。 |
| `operations/committed/` | 存放已成功提交的历史事务记录。 |

---

## 3. 两阶段提交（2PC）与崩溃恢复

多文件变更（如更新任务正文、跨目录移动、写入审查附件）均通过两阶段预写日志（WAL）原子提交：

```text
[1. 准备阶段 - Prepare]
   将操作记录写入 -> operations/pending/<operation-id>.json (phase: "prepared")
   文件持久化同步 (通过 internal/fs 原子替换)

[2. 执行阶段 - Apply]
   创建附件目录 -> 写入文件 -> 重命名卡片目录 -> 更新版本号

[3. 提交阶段 - Commit]
   在 journal.lock 独占保护下：
   将记录 marker 原子更新为 phase: "committed"
   原子重命名：pending/<id>.json -> committed/<id>.json
```

### 崩溃场景与 `kander init` 恢复

若进程意外中断或被 kill：
- **处于 `"prepared"` 阶段的 pending 记录**：后续普通读命令会立即抛出明确的 `pending-recovery` 错误并阻断，读者绝不自动修复。
- **恢复命令**：运行 `kander init` 获取独占的 `board.lock`，解析 pending 记录并重放未完成的文件与目录操作，直到恢复完毕。
- **处于 `"committed"` 阶段的 pending 记录**：说明实际数据与版本号已提交，仅重命名未完成。`kander init` 仅补齐重命名，不再重复执行修改。

---

## 4. 日志保留与分区管理

- **已提交日志保留**：每次提交与恢复后，系统在 `journal.lock` 下执行最佳努力清理。常量 `committedJournalRetention = 100` 保留最近的 100 条已提交记录，避免过度占用存储与扫描开销。
- **迁移证据保护**：任何关联活跃迁移暂存区（`.kander/migrations/<operation-id>`）的记录，不受 100 条上限限制，始终保留用于验证。
- **分区布局**：新版 Kander 严格将记录分为 `pending/` 和 `committed/` 两个子目录。检测到旧版扁平 `operations/<id>.json` 时会提示运行 `kander init --maintenance` 进行归类。

---

## 5. 审查与持久化派发扩展

1. **审查二进制产物**：`Transaction.PutBytes` API 支持无损存储二进制附件。日志中通过 `"binary": true` 与 base64 记录非 UTF-8 数据。
2. **持久化派发授权**：来自持久化派发（Dispatch）的更新操作必须同时附带 `--dispatch-id` 和 `--execution-epoch`，写入时同时校验卡片版本与执行授权。
