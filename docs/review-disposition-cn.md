# 代码审查处置与完成门禁

本文档描述 Kander 的审查生命周期、结构化审查结论（Findings）管理、作者处置（Author Disposition）跟踪以及完成门禁（Completion Gate）验证机制。

`internal/board` 负责定义数据模型、持久化解析、受控发布与纯结构校验；`internal/review` 提供 CLI 命令、Reviewer 提示词组装与 Git 验证。`board` 绝不反向依赖 `review`。

---

## 1. 概览与工作流流水线

任何处于活跃状态的任务卡在移入 `done` 之前，必须具备明确的审查计划。即便审查被跳过或不适用，也必须封存明确的评估记录。

```text
+-------------+      +---------------+      +-------------------+
|  审查计划   | ---> | 运行审查代理  | ---> |   提取结构化结论  |
| Review Plan |      | Reviewer Run  |      | Extract Findings  |
+-------------+      +---------------+      +-------------------+
                                                      |
                                                      v
+-------------+      +---------------+      +-------------------+
|  批次关闭   | <--- | 推进目标提交  | <--- |   作者修复/处置   |
| Batch Close |      | Advance Target|      | Author Disposition|
+-------------+      +---------------+      +-------------------+
      |
      v
+-------------+
| 移入 "done" |
+-------------+
```

1. **审查计划（Review Plan）**：指定审查角色（`PMQA`、`Security`）与批次代码基线。
2. **审查运行（Reviewer Run）**：独立的审查 Agent 运行并输出结构化审查结论块。
3. **分配（Assignment）**：将审查结论明确指派给具体的任务卡。
4. **作者处置（Author Disposition）**：任务所有者（OWNER）修复或解释结论，记录 Commit SHA 与验证证据。
5. **目标推进（Target Advance）**：批次目标 Commit 随着修复补丁前移。
6. **批次关闭（Batch Close）**：所有审查要求均通过或完成机械验证后，关闭批次，解锁 `move done`。

---

## 2. 执行周期与计划

卡片移入 `done` 必须持有已封存（sealed）的审查计划。空的审查索引绝不等于审查通过。

### 计划管理命令

```sh
kander review plan <绝对工作目录> <绝对路径-plan.json>
kander review extend-plan <绝对工作目录> <绝对路径-extension.json>
kander review progress <绝对工作目录> <task-id>
```

### 单批次计划示例

```json
{
  "schema": 1,
  "sealed": true,
  "plan_id": "implementation-cycle",
  "author": "coordinator",
  "basis": "已确认的任务与本项目 AGENTS.md 规范",
  "cwd": "/absolute/group-worktree",
  "report_language": "zh-CN",
  "task_ids": ["20260907-example-task"],
  "batches": [{
    "batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<完整-base-sha>",
    "target_commit": "<完整-target-sha>",
    "requirements": {
      "PMQA": "required",
      "Security": "N/A: 本仓库 AGENTS.md 规定 Security 角色免除"
    }
  }]
}
```

- **审查角色**：标准计划使用两角色：`PMQA` 与 `Security`。历史的四角色与六角色计划仍可读且可正常关闭。
- **N/A 处理**：如果某角色免除，必须显式记录规则依据（`N/A: <原因与规则依据>`）。
- **非 Git 项目**：当所有角色均为 `N/A` 时，`base` 与 `target_commit` 可填写 `"N/A"`。只要有任何角色为 `required`，必须提供完整的 40 位 SHA。

### 周期重新绑定（Rebinding）

当 `kander move <id> working --owner <agent>` 修改了 `STARTED_AT` 时，原计划状态会变为 `requirements-needed`。
- `kander review progress` 会输出所有变更成员的 `rebind_cycles`。
- 调用 `extend-plan` 携带 `rebind_cycles` 原子重新绑定周期。历史运行、失败记录与作者处置均完整保留。

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "用户指定了新 OWNER；整个任务组的审查义务全部保留",
  "rebind_cycles": {"20260907-example-task": "<progress 返回的当前 cycle 摘要>"}
}
```

### 递增多批次计划（Multi-Batch Planning）

对于分阶段交付的任务组：
1. 初始化时设置 `"sealed": false`，仅包含第一批次。
2. `batch-one` 关闭后，调用 `extend-plan` 追加 `batch-two`。
3. 在最后一批次指定 `"seal": true` 封存计划。

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "前一批次已关闭；接收下一批次交付",
  "seal": true,
  "batch": {
    "batch_id": "batch-two",
    "previous_batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<上一批次已关闭的 target-sha>",
    "target_commit": "<下一批次的 target-sha>",
    "requirements": {
      "PMQA": "required",
      "Security": "N/A: 项目规则"
    }
  }
}
```

---

## 3. 结构化结论与历史报告映射

每个审查 Agent 的报告末尾必须输出且仅输出一个独立的围栏代码块：

````text
```kander-findings
{
  "FINDINGS": [{
    "id": "PM-01",
    "tier": "medium",
    "text": "结论的完整原始文本",
    "evidence": "file.go:42; 具体触发条件与影响"
  }],
  "NON_BLOCKING": []
}
```
````

### 校验规则

- **数组完备性**：`FINDINGS` 和 `NON_BLOCKING` 两个数组必须同时存在。
- **严重度分级**：
  - `FINDINGS`（阻塞性缺陷）：仅接受 `blocking`、`high`、`medium`。
  - `NON_BLOCKING`（非阻塞建议）：仅接受 `low`、`recommend`、`suggest`。
- **ID 唯一性**：ID 在两个数组间全局唯一。
- **血统追踪（Lineage）**：增量复审中继承自上一轮的缺陷必须明确指向其直接前驱：
  ```json
  "lineage": {"run_id": "上一轮的 run_id", "finding_id": "PM-01"}
  ```
- **机械性缺陷**：支持可选分类标签：`mechanical: "documentation" | "dead-code" | "redundant-test"`。

### 历史非结构化报告映射

对于历史无格式报告（`findings_schema = 0`），使用如下命令手动映射，不重写原始文件：

```sh
kander review map-legacy <绝对工作目录> <映射文件.json>
```

映射文件中引用的代码行与原文引用必须与原报告逐字一致。

---

## 4. 指派与作者处置记录

```sh
kander review assign <绝对工作目录> <指派文件.json>
kander review disposition <绝对工作目录> <处置记录.json> <期望卡片版本号>
kander review aggregate <绝对工作目录> <batch-id>
```

### 结论指派

审查产出的缺陷必须显式指派到具体卡片：

```json
{
  "run_id": "pm-first",
  "batch_id": "batch-one",
  "author": "coordinator",
  "basis": "按各任务修改范围指派",
  "items": {
    "PM-01": ["20260907-example-task"]
  }
}
```

### 作者处置记录

任务卡当前的 `OWNER` 对指派给自己的缺陷提交处置记录：

```json
{
  "record_id": "pm01-author-first",
  "run_id": "pm-first",
  "finding_id": "PM-01",
  "batch_id": "batch-one",
  "task_id": "20260907-example-task",
  "author": "codex",
  "report_hash": "<report.md 的 SHA-256>",
  "original": "与结论项完全一致的原始文本",
  "status": "fixed",
  "basis": "针对目标源码与真实调用链路已验证",
  "fix_commit": "<完整修复-sha>",
  "verification": "go test ./internal/auth -v"
}
```

### 状态流转规则

| 处置状态 | 允许范围 | 是否阻塞批次关闭？ | 说明 |
|---|---|---|---|
| `confirmed` | 阻塞性缺陷 | **是** | 已确认，等待修复。 |
| `unverifiable` | 阻塞性缺陷 | **是** | 无法复现。 |
| `fixed` | 阻塞 / 非阻塞 | 否 | 必须提供 `fix_commit`（严格晚于 target）和 `verification` 测试验证记录。 |
| `rejected` | 阻塞 / 非阻塞 | 进入未决列表 | 必须陈述客观事实依据。 |
| `waived` | 仅限 Security | 否 | 需包含 `accepted-risk` 明确决策或满 15 分钟的 `timed-out` 通知记录。 |
| `deferred` | 仅限非阻塞项 | 否 | 延期处理，需记录原因。 |

---

## 5. 目标推进、增量复审与批次关闭

### 1. 推进批次目标（Advance）

提交修复补丁后，推进批次的目标 Commit：

```sh
kander review advance <绝对工作目录> <推进请求.json>
```

请求包含 `{batch_id, expected_revision, advance: {previous_target, target, reason, deliveries}}`。要求 Git 工作区干净。

### 2. 增量复审（Incremental Review）

启动增量审查并关联前置轮次：

```sh
kander review run --task <task-id> --previous-run-id <run-id>
```

系统会自动将上一轮报告、不可篡改的作者处置记录以及汇总视图载入审查 Agent 上下文。

### 3. 机械性修复关闭门禁（Mechanical Fix Closure）

如果**所有**待决修复均为机械性（如文档拼写修正、无用代码清理），门禁允许将 `passed_at` 前移至修复 Commit，无需重新调用审查 Agent。

关闭请求中必须附带 `mechanical` 校验数组：
- `diff_hash`：针对变更文件的原始 Git diff 的 SHA-256 哈希。
- `facts`：说明无逻辑变更的事实验证证据（逐句比对、测试覆盖等）。

```sh
git diff --no-ext-diff --no-textconv --no-renames --binary --full-index --no-color <run-commit> <fix-commit> -- ':(literal)<path>'
```

### 4. 关闭批次（Close Batch）

汇总所有轮次与作者处置记录并关闭：

```sh
kander review aggregate <工作目录> <batch-id> > /tmp/batch-view.json
kander review close <工作目录> <关闭请求.json>
```

```json
{
  "batch_id": "batch-one",
  "expected_revision": 2,
  "view_hash": "<aggregate 输出的 SHA-256>",
  "author": "coordinator",
  "roles": {
    "PMQA": {
      "run_id": "pmqa-fixed",
      "passed_at": "<完整-sha>",
      "basis": "逐项核对合约与质量结论"
    }
  },
  "resolved_failures": {},
  "opinions": []
}
```

---

## 6. 完成门禁验证要求

执行 `kander move <task-id> done` 时会严格验证：
1. **计划封存**：审查计划为 `"sealed": true`。
2. **批次全关**：所有批次均已关闭且结论有效。
3. **无未决阻碍**：所有阻塞性缺陷均处于 `fixed` 或合规 `waived` 状态。
4. **Git 祖先链**：所有通过 Commit 和修复 Commit 均为最终交付目标的直接祖先。
5. **产物完整性**：本地审查账本哈希与副本完全吻合，无断链或缺失。
