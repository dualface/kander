# 看板写入护栏 (kander guard-write)

看板重复卡的产生方式只有一种: 在状态目录 (`backlog/`, `todo/`, `working/`, `review/`, `done/`, `archived/`, `trash/`) 下新建直接子项. 合法的新建只有 `kander new` 一个入口; Agent 拿着已迁移卡片的旧路径写入时, 编辑工具会静默重建文件, 在原位置复活一份跨状态副本.

`kander guard-write` 把这种写入变成显式报错, 供宿主项目的写入前 hook (PreToolUse 等) 调用.

## 命令

```sh
kander guard-write <path>
```

- 退出码 `0`: 放行.
- 退出码 `1`: 拒绝, stderr 说明原因; 同 ID 已在其他状态目录时会指出卡片当前状态.
- 退出码 `2`: 用法错误.

判定规则:

| 目标路径 | 结果 |
| --- | --- |
| 不在当前看板 `kanban/` 下 | 放行 |
| 看板内但不是状态目录内容 | 放行 |
| 状态目录直接子项, 文件或目录已存在 | 放行 (正常编辑) |
| 状态目录直接子项, 当前不存在 | 拒绝 (新卡只能经 `kander new`; 旧路径写入会复活副本) |
| 目录卡内部文件 (`spec.md`, `plan.md`, `report.md` 等), 目录卡入口存在 | 放行 |
| 目录卡内部文件, 目录卡入口不存在 | 拒绝 (写入会静默重建整个目录卡) |

看板定位沿用 `KANBAN_DIR` -> 当前 Git 仓库主 worktree 的 `kanban/` -> 向上查找的既有顺序. 定位不到看板 (非 Git 项目且未设 `KANBAN_DIR`) 时放行, 不阻塞非看板项目; 其他定位错误仍然失败.

护栏只防同用户 Agent 误写, 不是抵御恶意进程的安全边界.

## Claude Code 接入

仓库提供样例脚本 [`scripts/guard-kanban-write.sh`](../scripts/guard-kanban-write.sh) (依赖 `jq`; 缺 `jq` 或解析失败时放行). 在项目 `.claude/settings.json` 中注册 PreToolUse hook:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {"type": "command", "command": "sh scripts/guard-kanban-write.sh"}
        ]
      }
    ]
  }
}
```

脚本从 stdin 读取 hook JSON, 取 `tool_input.file_path` 交给 `kander guard-write`; 拒绝时以退出码 2 阻止本次工具调用并把原因回显给 Agent.

`Bash` 里的重定向、heredoc、`sed -i` 等写入手段无法从工具参数可靠还原目标路径, hook 不覆盖; 对应约束由看板规则的「写卡前必须以任务 ID 重新定位」条款承担.

## Codex 接入

Codex 的 PreToolUse hook (`.codex/hooks.json`) 可用同一脚本, matcher 建议覆盖 `apply_patch|Edit|Write`. 传入 JSON 的字段名以所用 Codex 版本为准, 必要时在脚本里补充相应的路径提取逻辑.

## Windows

`kander guard-write` 子命令本身跨平台可用. 样例 hook 脚本为 POSIX shell; 原生 Windows 环境需要按所用 Agent 的 hook 机制自行封装调用.
