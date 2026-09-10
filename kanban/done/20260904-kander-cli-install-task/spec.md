# CLI entry point and installer

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 3a193741-a8c0-4428-858c-5932853a70d0
- 窗口: herdr:wG:t1R:wG:p1Z
- 开始时间: 2026-09-04 10:50
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-cli-install-task
- 结果: completed

## 任务目标

落地 `cmd/kander` 与 `internal/cli` 的完整子命令注册, 以及 POSIX `install.sh` / Windows `install.ps1`. 安装后用户能运行 `kander --help`; 尚未实现的子命令以稳定非零错误退出, 不 panic.

## 用户决策

单二进制, 无 Python、无 `.cmd` 业务包装. 安装器构建或安装已构建的 `kander`, 覆盖 `rules/*.md` 到规则根, 同步 `share/` 到资源目录. 全局安装结束用绝对路径跑 `kander welcome` (welcome 未实现时允许该步骤失败并写明, 但安装载荷必须已就位). `--project` 只写主 worktree `.kander/`, 不碰 HOME, 不跑 welcome. 退役旧 `onevoke`/`kanban`/`onevoke-review.*` 入口时先提示再删, 与 onevoke 退役旧 review 脚本同一交互.

子命令一次注册完毕, 后续卡只实现对应 `internal/*` 包, 禁止为接线冲突改 `main.go` 的同一段.

## 预期成果

`./install.sh` 与 `.\install.ps1` 可在临时 HOME 完成全局或项目安装; `kander --help` 列出全部子命令; 项目安装 stdout 给出项目本地 `kander` 绝对路径.

## 验收条件

- [x] `--help` 列出: `welcome` `doctor` `config` `review` `init` `rules` `list`/`ls` `show` `new` `move` `pick` `start` `resume` `notify` `dismiss` `check` `subscribe` `web` `tui` `merge-memory`; 全局 `--lang {cn,en}`.
- [x] 未实现子命令中文错误并非零退出; 已实现的 (本卡范围内的 help 与安装) 正常.
- [x] 全局安装: 二进制到 `~/.local/bin/kander`, 规则到 `~/.agents/` (含 `KANDER-AGENTS.md`), 资源到 `~/.local/share/kander`. `~/.agents/AGENTS.md` 不存在时 POSIX 相对符号链接, Windows 硬链接回落符号链接, 已有入口不覆盖. stdout 稳定句 `Kander 已安装` / `Kander installed`.
- [x] `--project <目录>`: 载荷只落主 worktree `.kander/`, 写入 `/.kander/` exclude, 不创建 HOME 路径, 不跑 welcome, 不改 PATH. 非 Git / 无效参数拒绝且不回落全局.
- [x] 同名目标是目录或 Windows reparse point 时, 写任何文件前拒绝.
- [x] Windows 安装器不修改 PATH, 必须提示把 `~/.local/bin` 加入 PATH; 不再探测 Python.
- [x] 安装测试覆盖全局与项目成功/拒绝路径, 对标 onevoke `install.sh`/`install.ps1` 与 `tests/test-install-windows.py` 中仍适用的边界 (Python 探测相关用例删除, 改为二进制存在性).

## 威胁模型

安装器向 HOME 与项目 `.kander/` 写可执行文件和规则. 必须拒绝把文件写入符号链接目标或错误边界. 不处理从网络下载二进制的供应链; 本阶段只安装本仓库构建产物.

## 不在本轮范围

- 既有问题: 排除自动迁移 `~/.config/onevoke` 与已有 onevoke 规则内容合并.
- 并发/跨平台/安全加固: 纳入安装路径的目录/reparse 拒绝; 排除 welcome 菜单、MemSearch 插件克隆.
- 共享契约与文档: 安装路径已在契约卡定义; 本卡不改发布规则正文, 除非发现与安装器行为不一致的笔误并仅改命令名/路径表述.
- 相邻功能: 排除各子命令业务实现 (board/start/review/...). 本卡可保留 `internal/cli` 到各包的调用点, 包内返回「未实现」.

## 讨论与决策

```text
前置任务: 20260904-kander-config-task
```

- 需要 config 的 `install_paths` / `project_install_paths` / exclude 写入.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-cli-install-task`, worktree `worktrees/20260904-kander-cli-install-task/`, 基于组分支当时头后 rebase 到含 `internal/process` 的组头.
- `cmd/kander` 只调用 `internal/cli.Run`; 命令表一次注册完毕, 未实现子命令返回稳定中文/英文错误且非零, 不 panic.
- `install.sh` / `install.ps1` 安装已构建二进制或 `go build ./cmd/kander`, 覆盖 `rules/*.md`, 同步 `share/` 到资源目录; 全局结束用绝对路径跑 `kander welcome` (未实现允许失败); `--project` 只写主 worktree `.kander/` 与 `/.kander/` exclude.
- 退役 `onevoke`/`kanban`/`onevoke-review.*` 先提示再删. Windows 不改 PATH, 不探测 Python, 缺 PATH 时提示加入 `~/.local/bin`.
- 提交: `bee6bd09f8721e050d38c71962f851bf473f2f38` 注册 CLI; `81fd336ff0f59fcfe3afa0feb14a123056e5bcf3` 安装器与测试.
- 验证: 在任务 worktree 对 `group/20260904-migrate-to-go-group` rebase 后执行 `go test ./...` 通过 (`internal/cli` 含 POSIX 安装器全局/项目成功与拒绝路径; Windows 安装器测试 `//go:build windows` 本机未跑).
- 无 `origin`, 跳过 push, 本地 `git merge --ff-only` 已把任务分支头 ff 进 `group/20260904-migrate-to-go-group`.

### 组级审核批次 5 finding

- PM-1 / QA-1 (medium, 同根因): **成立**. `install.ps1` 的 `Assert-PayloadTargets` 原先只预检 share 根与叶子; 嵌套 `kanban-web` 为文件或 reparse 时会先写入 bin/rules (项目还会写 exclude). 已对每个 share 相对路径的中间分量调用 `Assert-DirectoryTarget`, 且该循环仍在 `Add-GitExclude` / `Install-Payloads` 之前. 提交 `cc09104e254e950c444ae6f6603f9209e111fd7c`.
- PM-2 (medium): **成立**. Windows 测试未覆盖仍适用的 onevoke 边界. 已补 AGENTS.md 硬链接/samefile、确认后删除 legacy、拒绝路径不写 `.local/bin`/`.agents`、保留已有 AGENTS.md 文件与目录、绝对路径 welcome `--lang`、非法 `--lang` 不写文件、无 `kander.exe`/无 go 拒绝、项目成功路径检查载荷与 `/.kander/` exclude, 以及嵌套 share 父路径拒绝. Python 探测用例不移植.
- PM-3 (suggest): **不成立/超出契约**. README 安装菜单不在本卡验收与提交范围; 本卡明确允许 welcome 失败. 不改 README.
- PM-4 (suggest): **成立但非阻塞**. POSIX 已实现保留 AGENTS.md 目录/悬空符号链接, 缺对应测试. 本轮不修, 建议后续卡补测.

修复后 rebase 到组头并 `go test ./...` 通过; Windows 用例仍为 `//go:build windows`, 本机未跑 PowerShell. 已 ff 进 `group/20260904-migrate-to-go-group`. 无 origin, 未 push.

### 组级审核批次 5 增量 finding

- PM-5 / QA-2 (medium, mechanical, 同根因): **成立**. `cc09104` 增加的 `initGitRepo` 在 Windows 测试文件中无调用点, 项目测试仍内联 `git init`. 已改为 `TestWindowsProjectInstallSkipsWelcomeAndHome` 调用该 helper. 提交 `522650839930b05a9610635be8215b80511fe2fd`.
- PM-3 / PM-4 (suggest): 维持上轮结论, 本轮不修.

`go test ./...` 通过; 已 ff 进组分支. 无 origin, 未 push.

## 完成总结

- 交付: `kander --help` 列出全部子命令与 `--lang {cn,en}`; `./install.sh` 与 `.\install.ps1` 可在临时 HOME 完成全局或项目安装, 项目安装 stdout 给出本地 `kander` 绝对路径.
- 验收: 7/7; 上列验收条件均已在 POSIX 测试中自检. Windows 专项路径有测试代码, 本环境未执行 PowerShell.
- 验证: `go test ./...` 通过; 安装器用例覆盖全局复制、legacy 确认删除、目录目标拒绝、项目安装不碰 HOME、非 Git/符号链接/无效参数拒绝、二进制缺失拒绝.
- 审核: 批次 5 首轮 + 增量 + 增量2; PM/QA 通过; CSA/Hacker N/A (仓库 AGENTS.md). 修复轮次: share 预检与 Windows 测试 (cc09104), initGitRepo 死代码 (5226508).
- 收尾: 最终 commit `522650839930b05a9610635be8215b80511fe2fd`; 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回本地 `develop` (`bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`); 无 origin 未 push. 记忆合并空操作 (源无 `.memsearch/memory`); 已删本卡 worktree 与本地任务分支. 未处理项: PM-3 README 安装菜单表述; PM-4 POSIX 缺 AGENTS.md 目录/悬空链接测试.
