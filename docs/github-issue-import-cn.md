# GitHub Issue 导入与结果协议

`kander issue import NUMBER` 命令用于将远端 GitHub Issue 转换为本地 `backlog/` 下的标准任务卡，并将未经修改的原始文本妥善存放在专属附件中。

导入操作具备**幂等性**、**有界性**，并在**单次独占板锁事务**中原子发布，确保卡片永远不会脱离其来源数据孤立出现。

---

## 1. 导入命令与看板快捷键

```sh
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments]
                           [--type feature|bug|chore|research] [--large]
                           [--language 语言] [--json]
```

- `--repo`：目标仓库标识（如 `github.com/owner/repo` 或 `owner/repo`）。直接复用本地 `gh` CLI 登录凭证，绝不索要 Token。
- `--comments`：同时拉取 Issue 评论讨论串。默认不拉取以节省流量与开销。
- `--type`：覆盖根据标签自动推断的 TYPE 类型。
- `--large`：显式指定 `SIZE: large`。所有导入卡片均以目录形式创建。
- `--language`：设定卡片 `LANGUAGE`（默认继承当前配置的 `agent_language`）。
- `--json`：输出机器可读 JSON，包含 `task_id`、`state`、`path`、`existing`、`source_key`、`source_url`、`comments_loaded`。

### 终端看板浮层快捷键（`g`）

在终端看板中按 `g` 键唤起 GitHub Issues 浮层：
- `i` / `I`：导入选中的 Issue（`I` 包含评论）。
- `g`：对于已导入的 Issue，一键跳转到对应本地任务卡。
- `s`：对未绑定的 Issue 发起交互式接管会话（Triage），或对已完成卡片发起[结果回写对齐](github-issue-results.md)。

---

## 2. 来源键（Source Key）与幂等性

导入卡片唯一的身份绑定锚点是其规范化的 Source Key：

```text
github://HOST/OWNER/REPO/issues/NUMBER
```

- **原子唯一性检查**：在 `board.lock` 独占事务内部校验。
- **并发导入防护**：并发执行或重复导入相同 Issue 均安全返回同一张卡片（`existing: true`），绝不发起重复网络抓取。
- **ID 冲突处理**：若可读任务 ID 发生重名碰撞，自动追加 source_key 哈希的前 8 位十六进制字符。

---

## 3. 卡片附件与安全清洗边界

每个导入的任务卡目录在 `spec.md` 旁包含两个专用附件：

```text
kanban/backlog/<task-id>/
  ├── spec.md
  └── source/
      ├── github-issue.json    (机器可读的带版本快照)
      └── github-issue.md      (便于人类阅读的 Markdown 镜像)
```

### 安全边界：不信任远端输入

为防止 Prompt 注入、权限提升及看板状态被污染：
1. **杜绝元数据注入**：远端正文**绝不**拼入任务卡的头部元数据或审查章节。外部人员绝无法通过编辑 Issue 伪造 `CARD_REVIEW` 或 `SELF_REVIEW` 记录来绕过代码审查门禁。
2. **标题净化**：Issue 标题仅作为卡片正文的 `H1` 标题，且被强制清洗压制为单行纯文本。
3. **字符清洗**：终端转义控制码、C0/C1 字符、双向文本覆写（bidi）以及无效 UTF-8 在落盘前均被剔除。

### 尺寸边界上限

| 检查项 | 最大上限 | 超限处理 |
|---|---|---|
| Issue 正文 | 512 KiB | 拒绝并给出错误建议 |
| 单条评论 | 64 KiB | 拒绝并给出错误建议 |
| 评论总条数 | 50 条 | 截断保留最新 50 条 |
| 单卡总快照体积 | 1 MiB | 拒绝并给出错误建议 |

---

## 4. Issue 快照本地缓存

看板 Issues 浮层在本地维护一份快照缓存：

```text
<看板根目录>/.kander/caches/issues/v1/<sha256(source_key)>.json
```

- **极速首屏绘制**：选中行时立即自缓存绘制正文与评论，网络请求在后台静默刷新。
- **权限与配额**：以 `0600` 私有权限保存，最大上限 200 条记录或 16 MiB 总容量，LRU 淘汰。

---

## 5. 锚点卡片与关联兄弟卡片（Anchor & Siblings）

当一个庞大复杂的 Issue 需要拆解到多张任务卡分别实施时：

```text
[GitHub Issue #42]
        |
        v (kander issue import)
+-------------------------------+
| 锚点卡片 Anchor (持有 source)  |
+-------------------------------+
        |
        +---> 兄弟卡片 Sibling 1 (引用锚点)
        +---> 兄弟卡片 Sibling 2 (引用锚点)
```

- **锚点卡片（Anchor）**：唯一持有 `source/github-issue.*` 附件并绑定 `source_key` 的主卡。
- **兄弟卡片（Sibling）**：不重复导入，而是在自身 `DISCUSSION` 块中声明引用：
  ```text
  SOURCE_ISSUE: github://HOST/OWNER/REPO/issues/42 (anchor: <anchor-task-id>)
  ```
