# 编码工作流的多模型路由

[English](multi-model-routing.md) | **简体中文**

Kander 可以给 `small` 卡和 `large` 卡配不同的执行 Agent, 给 `PMQA` 和 `Security` 配不同的审核员. 这样不必为整个工作流只选一个模型, 而是让几个模型的失误类型互相补位.

本页介绍路由原则和一份具体的配置示例. 模型能力和厂商名称变化很快, 示例只是参考, 不是 Kander 的默认值.

## 路由原则

### 实现与评判分开

PMQA 审核员最好与实现模型来自不同的模型家族. 看的不只是基准分数: 训练、后训练、工具使用和推理方式各不相同, 盲区也就不同.

不要让作者自己做唯一的最终正确性评判. Kander 仍要求执行 Agent 核实审核意见, 但审核员的第一轮判断应当独立形成, 不受作者理由的影响.

### 最强的实现模型用在 `large` 上

Kander 的 `SIZE` 表示风险, 不是行数. `large` 卡包括跨模块契约、状态转换、并发与重试、安全边界, 以及设计上有较大不确定性的改动. 把仓库级实现和验证能力最强的模型分给 `large`.

只有卡片确实边界清楚时, 才给 `small` 用更快的模型. PMQA 仍是质量关口.

### Security 作为专项审核

Security 审核的目标与 PMQA 不同: 追踪不可信输入如何跨越信任边界, 并确认现实可行的利用链. 即使通用实现或 PMQA 首选别的模型, 一个安全方向编码能力强的模型在这里也很有用.

### 保留原生 harness 的优势

模型的编码表现取决于它的 Agent harness、工具、上下文管理和验证循环. 条件允许时, 优先用该模型设计和评测时所用的 CLI 或 harness. Kander 支持自定义 Agent, 正是为了不必把执行统一到一个通用 CLI 上.

### 优化工作流, 不是排行榜

真正要看的是最终合并的改动正确的概率. 一个单独实现能力稍弱的模型, 只要能抓到不同的错误, 当审核员仍然有价值. 反过来, 一个很快的实现模型不一定是好的最终审核员.

## 配置示例: 当前生产配置 (2026 年 10 月)

这是 Kander 作者截至 2026-10-07 在 QuickTUI、Kander 自身和其他项目上使用的配置:

| Kander 角色 | Agent | 模型 | 推理档位 |
| --- | --- | --- | --- |
| `small` 执行 | Claude Code | Opus (1M 上下文) | medium |
| `large` 执行 | Claude Code | Opus (1M 上下文) | medium |
| `PMQA` 审核, `small` | Devin | SWE-2-max | Agent 默认 |
| `PMQA` 审核, `large` | Cursor | Grok-4.7-xhigh | Agent 默认 |
| `Security` 审核, `small` | Pi | DeepSeek-V4.1-Flash (经 OpenCode Go) | max |
| `Security` 审核, `large` | Devin | SWE-2-max | Agent 默认 |

预期流程:

```text
small 卡 -> Claude Code (Opus) -> Devin SWE-2-max PMQA  -> 触发时 Pi DeepSeek-V4.1-Flash Security -> 收尾
large 卡 -> Claude Code (Opus) -> Cursor Grok-4.7 PMQA  -> 触发时 Devin SWE-2-max Security       -> 收尾
```

它如何落实上面的原则:

- **实现与评判分开.** 每个审核环节都用与执行者不同的模型家族: xAI Grok、Cognition SWE 和 DeepSeek 审 Anthropic Opus 写的代码.
- **一个执行者, 两种审核力度.** 一个强执行者同时负责两种规模, 规模决定审核有多重. `large` 卡由 Grok-4.7-xhigh 做 PMQA, 由 SWE-2-max 做 Security.
- **Security 单独选.** 在 `large` 卡上, PMQA 和 Security 审核员来自不同家族, 两个环节都触发的卡会得到两份独立判断.

对应的配置:

```json
{
  "kanban_agents": { "large": "claude", "small": "claude" },
  "reviewers": {
    "large": { "PMQA": "cursor", "Security": "devin" },
    "small": { "PMQA": "devin", "Security": "pi" }
  },
  "models": {
    "kanban": {
      "claude": { "large_model": "opus[1m]", "large_effort": "medium",
                  "small_model": "opus[1m]", "small_effort": "medium" }
    },
    "review_roles": {
      "PMQA": {
        "large_agent": "cursor", "large_model": "grok-4.7-xhigh",
        "small_agent": "devin", "small_model": "swe-2-max"
      },
      "Security": {
        "large_agent": "devin", "large_model": "swe-2-max",
        "small_agent": "pi", "small_model": "opencode-go/deepseek-v4.1-flash", "small_effort": "max"
      }
    }
  }
}
```

这是 `kander config --json` 的节选, 只列出与路由相关的键.

### 10 月实际运行情况

配置会随模型和 harness 变化. 2026-10-01 至 2026-10-07 五个项目记录的审核运行 (共 359 次):

| 角色 | 审核员 | 模型 | 次数 |
| --- | --- | --- | ---: |
| PMQA | Grok CLI | grok-4.7 | 106 |
| PMQA | Grok CLI | grok-4.7-build-fast | 89 |
| PMQA | Devin | swe-2-max | 35 |
| PMQA | Cursor | grok-4.7-xhigh | 28 |
| PMQA | Cursor | gemini-3.8-flash-high | 10 |
| PMQA | Grok CLI / 其他 | 其他 Grok 变体 | 5 |
| Security | Devin | swe-2-max | 85 |
| Security | Pi | opencode-go/deepseek-v4.1-flash | 1 |

10 月的 249 张卡中, Claude Code 执行了 229 张. 2026-10-05 之前, PMQA 主要在 Grok CLI 上运行; 从 2026-10-06 起, 大部分 PMQA 改由 Cursor 运行, 与上面按规模拆分的路由一致.

本页原来的 2026 年 9 月示例 (`large` 用 SWE-2, `small` 用 DeepSeek-V4.1-Flash, PMQA 用 Grok-4.6) 已被这份配置取代. 各 Agent 的总数见 [Kander 实战数据](production-stats-cn.md).

## 卡片审核与规划

执行前需要独立 CARD_REVIEW 时, 尽量用与计划中 large 卡执行者不同的模型. 在上面的配置里, Claude Code 执行两种规模, 用 Grok 或 SWE-2 做卡片审核, 就能与执行者保持独立.

CARD_REVIEW 应关注需求是否完整、验收标准的质量、范围是否自相矛盾、依赖关系, 以及拆出的卡能否各自独立验证. 它不应替后面的 PMQA 审核员预先论证实现方案.

## 作者与审核员的分歧

把分歧当作信息而不是噪音, 多模型路由才最有用.

作者驳回一条必须处理的审核意见时, 要有事实依据, 证明该意见的某个关键前提不成立. 如果双方都无法确认起决定作用的运行时事实, 优先标为 `unverifiable`, 而不是给出没有依据的驳回.

Kander 目前没有仲裁环节: 标为 `unverifiable` 的必须处理项交给用户决定. 未来可能的方向 (尚未实现) 是只把有争议的意见交给第三个模型, 而不是再跑一次完整审核. 这个模型要与作者和 PMQA 审核员都来自不同家族, 拿到的是任务契约、相关代码与 diff、审核意见及其证据、作者的驳回依据和客观验证证据, 绝不包括任何一方隐藏的思维链.

## 单卡覆盖

按规模路由适合大多数卡. 某张卡无论配置如何都需要特定 harness 或模型时, 在卡头里固定它: 执行者用 `EXEC_AGENT` / `EXEC_MODEL` / `EXEC_EFFORT`, 审核员用 `REVIEW_PMQA_*` / `REVIEW_SECURITY_*` (见 `rules/KANDER-KANBAN-RULES.md`「Card Pins」). 固定过的卡会在 `EXEC_RESOLVED` 和 `REVIEW_<ROLE>_RESOLVED` 中记录每个环节实际的选择方式, 每个值标为 `forced`、`cli` 或 `config:<scale>`, 以后的读者能分清是有意覆盖还是路由默认. 固定值随卡片契约冻结; 它用于用户对某一张卡的决定, 不能代替路由配置.

## 不要写死的东西

不要把示例中的模型名变成永久的协议规则. 厂商模型、推理档位和 harness 行为的变化比 Kander 的审核语义快得多.

Kander 中稳定的概念是:

- 按任务风险路由 (`small` / `large`);
- 条件允许时, 让实现与 PMQA 保持独立;
- 单独选择 Security;
- 把模型与推理档位绑定到所选的 Agent 和规模;
- 为每条审核意见和处置保留持久证据;
- 用客观验证解决模型之间的分歧.

具体的 Agent 和模型名通过 Kander 现有的 `kanban_agents`、`reviewers`、`models.kanban`、`models.review` 和 `models.review_roles` 设置配置. 配置方式和审核模板契约见 [自定义执行 Agent](custom-agents.md) (英文).
