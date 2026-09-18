# 任务持久化派发协议

持久化派发（Durable Dispatch）协议为多 Agent 异步协作提供至多一次交付保证、确定性的接收追踪以及可溯源的完成收据。

派发身份完全由 `dispatch_id` 唯一标识，绝不通过终端回显或列变化进行模糊推断。

---

## 1. 派发生命周期与状态机

```text
[kander notify / dispatch prepare]
               |
               v
         +-----------+
         |  prepared | (意图已持久化落盘)
         +-----------+
               |
               v
     +------------------+
     | delivery-unknown | (指令已尝试投递至终端)
     +------------------+
               |
      (kander move working --dispatch-id --execution-epoch)
               |
               v
         +-----------+
         |  accepted | (执行者确认接收；120秒接收窗口有效)
         +-----------+
               |
      (kander move review / done --delivery-commit)
               |
               v
         +-----------+
         | completed | (原子业务收据随卡片状态及版本号一起提交)
         +-----------+
```

1. **`prepared`**：意图持久化写入 `dispatches/<id>/intent.json`。
2. **`delivery-unknown`**：指令尝试送入终端。即使进程崩溃或网络中断，Kander 仍安全保留指令文件，避免盲目重置。
3. **`accepted`**：**仅当**执行 Agent 主动运行 `kander move <id> working --dispatch-id <id> --execution-epoch <epoch>` 时生成。
4. **`completed`**：执行 Agent 交付完成，移入 `review` 或 `done`，生成不可篡改的最终收据。

---

## 2. 派发分类与证据绑定

每次派发必须声明其类别（`kind`）并附带语义证据：

```sh
# 发起修复派发
kander notify <task-id> --kind fix --base <40位-SHA> \
  --evidence-file <证据文件.json> --message-file <UTF8-消息文件>

# 恢复未确认接收的派发
kander resume <task-id> --dispatch-id <id> --message-file <UTF8-消息文件>

# 检查派发状态
kander dispatch show <task-id> <id>
```

| 派发类别 | 目标流转 | 必须绑定的语义证据 |
|---|---|---|
| `fix` | `working` $\to$ `review` | 绑定对应批次 ID、缺陷 ID 以及此前已存在的作者处置记录。 |
| `sync` | `working` $\to$ `review` | 用于分支变基与环境同步。不接受审查缺陷绑定。 |
| `wrap-up` | `working` $\to$ `done` | 绑定已封存的审查终态 Commit、Git 集成 Commit 与 develop 分支引用。 |

---

## 3. `fix` 审查缺陷证据绑定

修复派发强制约束执行者必须严格针对指定的结构化缺陷进行修复：

```json
{
  "fix": {
    "batch_id": "batch-one",
    "findings": [
      {
        "run_id": "pm-round-two",
        "finding_id": "PM-02",
        "previous_run_id": "pm-round-one"
      }
    ],
    "authors": [
      {
        "finding": {"run_id": "pm-round-one", "finding_id": "PM-01"},
        "record_id": "author-one",
        "author": "codex",
        "artifact": {
          "task_id": "20260908-example-task",
          "path": "reviews/pm-round-one/dispositions/author-one.json"
        }
      }
    ]
  }
}
```

- **血统完整性**：系统自动校验 `findings` 必须来自指定批次且已显式分配给本卡片。
- **幂等性保障**：使用相同 `dispatch_id` 重试将继承原有绑定；在同一 ID 下篡改绑定将直接触发冲突拒绝。

---

## 4. `wrap-up` 集成绑定与代行收尾（Wrap-up on Behalf）

### 标准收尾派发

将已审查通过的最终 Commit 与已集成进 develop 分支的 Commit 建立绑定：

```json
{
  "wrap_up": {
    "git": {
      "cwd": "/absolute/worktree",
      "source_commit": "<40位审查终态-SHA>",
      "target_commit": "<40位集成到develop的-SHA>",
      "target_ref": "refs/remotes/origin/develop",
      "author": "coordinator",
      "basis": "用户确认推送及本地同步完成"
    }
  }
}
```

系统校验：
1. `source_commit` 严格等于封存审查计划的最终目标 Commit。
2. `source_commit` 确实是 `target_commit` 的直接祖先。
3. `target_commit` 包含在指定的 `target_ref` 分支中。

### 代行收尾例外（Executor Exit Exception）

当原执行 Agent 进程意外退出无法收尾时，协调者可申请专用纪元代行收尾：

```sh
kander dispatch authorize-wrap-up <申请文件.json>
```

- **进程验证**：必须通过进程探针证实原 Agent 确实已完全停止。
- **权限严格收敛**：专用纪元**仅限**清理工作区并追加 `## WRAP_UP_RECORDS` 记录，严禁修改业务代码或伪造作者处置。

---

## 5. 执行者侧原子收据机制

执行 Agent 必须通过原子收据命令确认接收与完成：

```sh
# 1. 确认接收派发
kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>

# 2. 更新任务 spec（同时验证执行授权）
kander update <task-id> --document spec.md --file <文件> \
  --expect-revision <版本号> --dispatch-id <id> --execution-epoch <epoch>

# 3. 提交修复完成
kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> \
  --delivery-commit <修复-sha> --disposition <附件相对路径>

# 4. 提交收尾完成
kander move <task-id> done --result completed --dispatch-id <id> \
  --execution-epoch <epoch> --delivery-commit <集成-sha>
```

- **重放安全（Replay Safety）**：重复执行接收命令将返回 `replayed: true` 与原始收据，不会重复触发作业，版本号不发生自增。
- **授权拦截**：过期的纪元或不匹配的派发 ID 无法提交卡片状态流转。
