# Link the local project with a GitHub repository

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-github-issues-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 11:24
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

让 Kander 从当前 Git worktree 或显式参数可靠识别对应 GitHub 仓库，形成供后续 Issue 能力复用的 canonical repository identity 和安全 provider 基础。

## USER_DECISIONS

- 首版复用 GitHub CLI (`gh`) 的认证，不由 Kander 保存 GitHub token。
- 先建立 provider-neutral 接口，避免 board、launch 和 TUI 绑定 `gh`。

## EXPECTED_OUTCOME

用户可确认 Kander 当前关联的 canonical host、owner、repository 和 URL；fork、多 remote、GitHub Enterprise、缺少 `gh`、未认证及无权限均得到确定且不泄密的结果。

## ACCEPTANCE_CRITERIA

- [ ] 定义 provider-neutral repository identity、resolver 接口和结构化错误；不得依赖 board、launch 或 TUI。
- [ ] `kander issue repo [--repo HOST/OWNER/REPO] [--json]` 支持显式覆盖和当前 worktree 解析；多候选不静默猜测，提示 `--repo` 或 `gh repo set-default`。
- [ ] canonical identity 经 GitHub 响应确认，不从目录名猜测；支持 github.com、SSH/HTTPS remote 和可配置 GitHub Enterprise host。
- [ ] `gh` 使用直接 argv、安全且已验证的工作目录、deadline、stdout/stderr 上限及严格 UTF-8/JSON 解码；不使用 shell。显式 repo 在非 worktree 中可运行，不假定存在 Git 根目录。
- [ ] Kander 不调用、读取或保存 `gh auth token`，不打印 token；安全区分缺少 CLI、认证、授权、SSO、404、限流和损坏响应。
- [ ] `doctor` 只读报告 `gh` 可用性、版本和 host 认证状态，不修改 remote、active account 或凭据。
- [ ] fake `gh` 测试覆盖 remote 歧义、环境覆盖、恶意输出、超时/超限、POSIX/Windows；`go test ./...` 通过。
- [ ] 同步维护中央 CLI registry/tests、AGENTS.md 包图和子命令、三语 i18n catalog，以及 README.md、README-CN.md、README-JA.md 的关联/配置说明。

## THREAT_MODEL

资产为 GitHub 凭据、私有仓库身份和终端完整性。攻击者可控制 remote URL、环境变量及 `gh` 输出，尝试命令注入、凭据泄露、错误仓库关联、终端转义和资源耗尽。实现须验证组成部分、禁用 shell、限制输出并脱敏错误。

## OUT_OF_SCOPE

- 现有问题：不修复与仓库识别无关的 CLI、Git 或配置缺陷。
- 额外加固：不实现通用 secret scanner。
- 共享契约：本卡只拥有 repository identity、resolver 和 `gh` 进程边界；Issue 查询类型由下一卡拥有。
- 相邻功能：不查询、导入或启动 Issue；不实现 GitHub App 登录、OAuth App、webhook、写回或其他 forge。
- 文档：更新本目标所需的 AGENTS.md、命令帮助、三语 README 和相关设计文档；不改无关章节。

## DISCUSSION

```text
PREREQUISITES: N/A
```

- 四卡组对应四个用户可独立验收目标；本组为完整线性依赖链，故按新版规则保留一个四成员任务组。
- 设计依据：`docs/github-integration-research.md`。
- 实现采用 `gh repo view` 解析本地上下文；未来原生认证只考虑细粒度 GitHub App，传统 OAuth App 因权限过宽不采用。这是研究结论，不是用户输入字段。
- 任务组编排见本卡 `group-plan.md`；本次授权只建卡，不授权启动或集成。
- SELF_REVIEW: 已按新版目标计数复核；本卡只覆盖本地项目关联，未混入查询、保存或交接，provider 基础均服务该目标，验收可判定。
- CARD_REVIEW: PASS；独立 Agent 最终复审确认单一关联目标、gh/provider 决策、非 worktree 解析、凭据边界、doctor、CLI/i18n/docs 及 plan 均完整一致。
