Role: QA  
Commit: `1d10371b763cad83ebf0aba295b985151ef71c50`  
Task Context: 两卡联合契约；评审范围 `aac6f2bf..1d10371b`。  
Reviewed Scope: 已核对提交树、完整相关实现、配置读写、主题切换、五处渲染消费点及测试。只读沙箱阻止 Go 测试重跑；采信交付提交的全量测试、vet 通过记录。本轮 `make fmt-check`、`git diff --check` 通过。

| 行为/质量 | 结论与证据 |
|---|---|
| 调色板、兼容接缝 | Observed：默认两套全部色值保持前置提交；六套对比度独立复算达标。`theme_test.go:20–183` |
| 配置、切换、持久化 | Observed：七值校验、循环、即时预览与写回路径贯通；未知值回落有覆盖。`config/tui_theme_test.go:9`、`options_test.go:266` |
| i18n、架构、文件规模 | Observed：三份语言包键集一致；沿用既有单向依赖；变更代码文件均未超过 1000 行。 |
| Markdown | Observed：主题分类、自身背景正确；Inferred：256 色输出仍有降级缺口，见 QA-01。 |
| 人工验收 | Observed：交付以序列断言替代人工检查；实际终端观感 Unverifiable，见 QA-02。 |

**QA-01 — medium — Inferred，置信度高：详情正文绕过 256 色降级。**

[detail_view.go:62](/home/dualf/works/kander/worktrees/20260909-tui-theme-group/internal/tui/detail_view.go:62) 未给 Glamour 传入 `ColorProfile`；锁定版本 Glamour v1.0.0 的 `glamour.go:83` 默认使用 `termenv.TrueColor`。本轮将背景从索引改为十六进制后，`markdownCanvasStyle` 在第 97–98 行直接传入该值。

触发：仅支持 256 色的终端，选择 `light` 后打开普通 Markdown 详情。正文仍输出 `48;2;250;250;250`，看板及外围填充则已降级。终端无法正确处理正文背景，破坏“详情与看板背景一致”的契约。这是本轮新增十六进制背景造成的兼容退化。

最小修复：传入 `glamour.WithColorProfile(lipgloss.ColorProfile())`；在 ANSI256 下直接测试普通 Markdown 正文的背景序列。现有 `theme_test.go:219–244` 只覆盖 Lip Gloss，未覆盖此路径。

**QA-02 — medium — Observed，置信度高：明确要求的人工验收尚未完成。**

[task-spec.md:39](/tmp/codex-review.c723bce6a40b0186bf5f1292a8c1b407/task-spec.md:39)、第 119 行分别要求 Solarized 四组合、六主题五界面的人工验证。[report.md:50](/home/dualf/works/kander/kanban/review/20260909-tui-theme-presets-task/report.md:50) 明确承认未进行真实交互检查；palette 的 `spec.md:94` 也仅记录 PTY 模拟。

替代证据不足：`theme_test.go:412–415` 只检查输出是否出现背景序列，不能确认整幅画面、前景与实际切换效果；PTY 回答 OSC 11 也不等于真实 Solarized 终端显示验收。

影响：两卡的显式验收条件未闭合。最小修复：在交付提交补做契约列出的人工检查并记录终端/profile、组合与结果；完成前保留未验收状态。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-01",
      "tier": "medium",
      "text": "Inferred，置信度高：本轮将主题背景改为十六进制，但 renderMarkdown 未向 Glamour 传入当前 ColorProfile。仅支持 256 色的终端打开 light 主题普通 Markdown 详情时，正文仍输出 truecolor 背景序列 48;2;250;250;250，而看板及外围填充已经降级，导致正文背景无法按终端能力正确呈现，违反详情与看板背景一致及 256 色支持契约。最小修复：添加 glamour.WithColorProfile(lipgloss.ColorProfile())，并在 ANSI256 下对普通 Markdown 正文背景输出增加确定性断言。",
      "evidence": "internal/tui/theme.go:40,109 将默认背景改为十六进制；internal/tui/detail_view.go:62-66 创建 renderer 时未设置 profile，97-98 将主题背景直接交给 Glamour；go.mod:8 锁定 Glamour v1.0.0；/home/dualf/go/pkg/mod/github.com/charmbracelet/glamour@v1.0.0/glamour.go:81-84 默认 ColorProfile 为 termenv.TrueColor，ansi/baseelement.go:56-57 按该 profile 转换背景。internal/tui/overlay.go:204-215 保留正文序列，仅追加填充。internal/tui/theme_test.go:219-244 的降级测试未调用 Markdown renderer。背景从原有 ANSI 索引变为 RGB，使此缺口在本轮影响普通正文。"
    },
    {
      "id": "QA-02",
      "tier": "medium",
      "text": "Observed，置信度高：两卡明确要求的人工验收被 PTY 和渲染序列断言替代，尚未完成。该替代只能证明输出包含目标背景及调色板数值达标，不能证明真实终端中四种 Solarized 组合、六主题五界面的前景背景与切换效果符合设计。最小修复：在交付提交补做规定组合的人工验证并记录终端、profile 与结果；完成前保留未验收状态。",
      "evidence": "/tmp/codex-review.c723bce6a40b0186bf5f1292a8c1b407/task-spec.md:39,119 明确要求人工验证；/home/dualf/works/kander/kanban/review/20260909-tui-theme-presets-task/report.md:35-50 用序列断言列为通过，并明确承认未在真实交互终端检查；/home/dualf/works/kander/kanban/review/20260909-tui-truecolor-palette-task/spec.md:94 仅记录 OSC 11 的 PTY 模拟。internal/tui/theme_test.go:412-415 仅断言背景序列存在；internal/tui/options_test.go:250-262 的表面检查同样仅检查背景序列。"
    }
  ],
  "NON_BLOCKING": []
}
```

任务文件删除已尝试；只读文件系统拒绝，不影响评审结果。