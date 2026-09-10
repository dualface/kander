# Configuration and install scope

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 80aa7c89-9652-4e9d-8170-45b76147e4ca
- 窗口: herdr:wG:t1Q:wG:p1Y
- 开始时间: 2026-09-04 10:40
- 完成时间: 2026-09-04 15:08
- 任务分支: 20260904-kander-config-task
- 结果: completed

## 任务目标

实现 `internal/config`, 对标 `~/works/onevoke/bin/onevoke_config.py`: 按入口解析全局/项目作用域, 读写 schema 校验后的 `config.json`, 提供语言、launcher、执行 Agent、审核角色与模型档位的读取口.

## 用户决策

配置键保持 onevoke schema (`kanban_agent`, `kanban_agents`, `models.kanban`, `review_stages` 等). 仅路径与环境变量改为 kander: 全局 `~/.config/kander/config.json`, 项目 `<主 worktree>/.kander/config.json`, 覆盖 `KANDER_CONFIG`, 语言 `KANDER_LANG`/`KANDER_LANG_CLI`. 入口位于 `.kander/bin/` 时为项目模式; 源码树 `cmd/` 不得判为项目安装.

## 预期成果

`kander` 各子命令可通过本包得到当前作用域的路径与生效配置, 测试可用 `KANDER_CONFIG` 隔离.

## 验收条件

- [x] `install_paths()` 按入口解析: 项目模式路径落在 Git 主 worktree `.kander/` (config, rules, bin, share); 全局模式为 `~/.config/kander/config.json`, `~/.agents`, `~/.local/bin`, `~/.local/share/kander`.
- [x] `project_install_paths` 把目标归一到主 worktree; `ensure_project_git_exclude` 幂等写入 `/.kander/` 到本地 `info/exclude`, 经 `internal/fs` 追加与锁.
- [x] load/save 校验 schema; POSIX 同目录临时文件原子替换权限 `0600`; Windows 从 anchor 逐分量 no-follow, load 在不共享 WRITE/DELETE 的固定句柄上完成, 无效配置不迁移 ACL.
- [x] `kanban_agent_for(config, kind)` 与 `execution_agents_in_use(config)` 行为与 onevoke 一致; 未知规模或非法 Agent 拒绝.
- [x] 语言优先级 `--lang` (经 `KANDER_LANG_CLI`) > 配置 > 环境变量; 默认 `cn`.
- [x] launcher 枚举与平台默认不变: POSIX `auto`, Windows `console`; `console` 仅 Windows; `auto`/`herdr` 仅 POSIX.
- [x] 测试对标 `tests/test-onevoke-config.py` 的成功与拒绝路径.

## 威胁模型

配置文件含本机 Agent 选择, 必须仅当前用户可读. Windows 须防止经由 junction 把配置写到他人可控路径. 不处理网络与凭据存储.

## 不在本轮范围

- 既有问题: 排除从 `~/.config/onevoke` 自动迁移; 发现旧配置至多留给 welcome 卡提示, 本卡不导入.
- 并发/跨平台/安全加固: 纳入配置文件自身的 fs 边界; 排除 welcome 菜单与 MemSearch 对账.
- 共享契约与文档: 排除改 `rules/`. schema 与 onevoke 对齐, 字段不改名.
- 相邻功能: 排除 CLI 子命令 `config`/`welcome`/`doctor` 的用户界面 (welcome 卡); 本卡提供库与供 `kander config` 使用的只读格式化函数即可, 接线由 cli-install 预留.

## 讨论与决策

```text
前置任务: 20260904-kander-fs-task
```

- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 工作区: `/home/dualf/works/kander/worktrees/20260904-kander-config-task`, 基于组分支 `group/20260904-migrate-to-go-group` (`7245b86`).
- 实现 `internal/config`: 入口解析全局/项目作用域 (仅 `.kander/bin/` 为项目, `cmd/` 保持全局); `KANDER_CONFIG` / `KANDER_LANG` / `KANDER_LANG_CLI`; schema 校验与默认值对齐 onevoke; POSIX 同目录临时文件 `0600` 原子替换; Windows load/save 走 `internal/fs` 锚点句柄, 无效配置不收紧 ACL; Git exclude 经 `AppendUniqueLine` 加锁去重写入 `/.kander/`.
- 只读格式化: `FormatConfigLines` / `ReviewModelLines` / `ReviewStageLines`. 未接 CLI, 未改 `rules/`.
- 验证: 模块根 `go test ./...` 通过 (`internal/config`, `internal/fs`). 仓库无 `origin`, 跳过 push, 已本地 ff 进组分支.
- commit: `128ccd3024f48a5aa85e59109893debfa69b375c` (`实现安装作用域与配置读写 internal/config`).

## 上轮审核 finding (批次 3, base `7245b86`, reviewed `128ccd3`)

- QA-1 medium 成立: `Validate` 把缺失 `language` 与 JSON `null` 都当成默认 `cn`. 已改为仅缺键时默认, 显式 `null` 走 `validateChoice` 拒绝; 测试 `TestLanguageNullIsRejectedWhileMissingDefaults`.
- QA-2 / PM-1 medium [mechanical] 成立 (同根因一次处理): `TestConfigRejectsInvalidModelsSection` 与 `TestCursorRejectsUnknownModelFields` 重复断言 cursor 未知字段. 已删除表中两行, 保留后者作为 onevoke 对标测试.
- PM-2 / QA-4 low/suggest 成立, 本轮一并修: `decodeJSON` 忽略后续 token. 已在成功 Decode 后检查 `dec.More()`, 测试 `TestValidateJSONRejectsTrailingTokens`.
- PM-3 / QA-3 suggest/low 成立, 本轮一并修: `Save` 写路径误报「读取配置失败」. 已改为「写入配置失败」.
- PM-4 suggest [out-of-contract] 不修: POSIX `posixSave` 按验收条件实现 onevoke 同目录临时文件 + `0600` + `rename`, 不改用 `WriteTextAtomic` (其 replace 会保留旧 mode).

修复 commit: `413c0e66cca7e42a0dc4d4066e6fada25f0307c3`. 已 rebase 到组分支头并 `go test ./...` 通过; 无 origin, 跳过 push, 本地 ff 进 `group/20260904-migrate-to-go-group`.

## 完成总结

- 交付: `internal/config` 提供安装路径, schema 校验的 load/save, 语言/launcher/Agent/模型读取口, 以及 `kander config` 可用的只读格式化函数. 配置键未改名.
- 验收: 7/7; 上列验收条件均由 `internal/config` 测试覆盖.
- 验证: `go test ./...` 在任务 worktree 通过; Windows 专项在非 Windows 上 skip, 与 `internal/fs` 相同.
- 审核: 批次 3 首轮 + 增量; PM/QA 通过. CSA/Hacker N/A (仓库 AGENTS.md). 修复轮次 1 (commit `413c0e66cca7e42a0dc4d4066e6fada25f0307c3`). 未处理项: PM-4 POSIX posixSave 未走 WriteTextAtomic; QA-5 suggest Decoder.More() 剩余 ]/}.
- 收尾: 最终 commit `413c0e66cca7e42a0dc4d4066e6fada25f0307c3` 已是 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 的祖先. 组分支已 ff 合回本地 `develop` (无 origin, 未 push). 记忆合并空操作 (源无 `.memsearch/memory`). 已删本卡 worktree 与本地任务分支; 未删组分支.

