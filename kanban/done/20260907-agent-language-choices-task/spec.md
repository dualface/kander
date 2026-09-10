# Options panel: agent_language as a fixed choice list

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- CREATED_AT: 2026-09-07 10:22
- OWNER: cursor
- SESSION: cursor 5f770a6c-1ed2-4ce7-8274-4a37d9ee8bf1
- WINDOW: herdr:wX:tK:wX:p13
- STARTED_AT: 2026-09-07 10:23
- FINISHED_AT: 2026-09-07 10:29
- TASK_BRANCH: agent-language-choices
- RESULT: completed

## GOAL

Replace the free-text "Agent language" input in the TUI options panel (interface section, directly below "Default language") with a select of fixed choices, so the user picks the language the agent uses to communicate with them instead of typing a code. The stored config key stays `agent_language` (added in commit 4a292fb, refined in d5ec4bb and eb4fb29 on `develop`).

## USER_DECISIONS

- The choice list is exactly these eight values, in this order: `en` English, `zh-CN` 简体中文, `zh-TW` 繁體中文, `ja` 日本語, `ko` 한국어, `es` Español, `fr` Français, `de` Deutsch.
- Labels are each language's own name (endonym) and are not translated per interface language, so every user can find their own language at a glance.
- The value is unrelated to the interface language `language`; it only tells the agent which language to use with the user.
- Config validation stays free-form (non-empty, single line, at most 64 characters) so a hand-edited value outside the list, such as `pt`, remains valid. The panel must not silently replace such a value: it appears as an extra option so the user keeps it unless they choose another.
- The `language` to `agent_language` derivation (cn -> zh-CN, en -> en) stays as it is; adding derivations for new interface languages belongs to the card that adds those languages.

## EXPECTED_OUTCOME

- In the options panel interface section the "Agent language" row is a `huh.Select` with the eight labeled choices; left/right cycle them, the selected value is written to the session immediately and saved with the section like the other interface fields.
- A stored `agent_language` outside the eight values shows as a ninth choice labeled with the raw value and is preselected; it is preserved unless the user picks another choice.
- The choice list lives in `internal/menu` (session-side, like `LanguageChoices`) so the TUI stays a thin binding and other front ends can reuse it.
- `kander config` output and config validation are unchanged.

## ACCEPTANCE_CRITERIA

- [x] `menu.Session` exposes an agent-language choice method returning the eight `Choice` values with codes and endonym labels in the specified order, plus the current stored value appended when it is not in the list.
- [x] `internal/tui/options_form.go` renders "Agent language" as an inline `huh.Select` bound to that list, replacing the free-text input; the existing `menu.agent_language` title and `tui.agent_language_hint` description keys are reused so no catalog file changes (the parallel Japanese card owns catalog edits).
- [x] Choosing a different value calls `Session.SetAgentLanguage`, marks the panel dirty, and persists via the existing save path; reopening the section shows the saved value.
- [x] Tests: a `internal/menu` test for the choice list including the out-of-list preservation case; a `internal/tui` test that the interface section still orders language, agent language, theme, refresh and that selecting a choice updates the session; existing `TestThemeChangeKeepsInterfaceState` and PTY test keep passing.
- [x] `go build ./...`, `go vet ./...`, `go test ./...` pass.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- Pre-existing problems: any options-panel behavior unrelated to the agent language row that exists on `develop` at eb4fb29 is excluded; fixing it widens the task.
- Concurrency, cross-platform and security hardening: not required by the acceptance criteria; Windows TUI behavior is untouched and excluded.
- Shared contracts and docs: `agent_language` validation, derivation and the `kander config` line are unchanged by decision; `AGENTS.md` only needs a one-line note that the panel offers a fixed list while the schema stays free-form. Adding `ja` as an interface language and its `agent_language` derivation is the separate card `20260907-japanese-ui-task`, which may run in parallel; do not touch `config.Languages`, `locales/*.json`, or the install wizard.
- Adjacent features: per-role or per-task language overrides, and translating labels, are excluded.

## DISCUSSION

SELF_REVIEW: Goal and outcome match the user's request (fixed list of eight, unrelated to UI language). Boundary is clear: only the panel row and its menu-side choice list change; validation stays free-form by recorded decision, with out-of-list preservation so nothing the user typed is lost. Constraints are executable against the current code (`Session.LanguageChoices` pattern, `interfaceFocusKey("agent_language")` already exists). Acceptance criteria cover the list, the widget, persistence, tests and build; no out-of-scope requirement was added. Parallel card boundary with the Japanese UI card is spelled out to avoid edit conflicts: this card makes no catalog edits by reusing existing keys.

## IMPLEMENTATION

- Branch/worktree: `agent-language-choices` at `worktrees/agent-language-choices` (base `eb4fb29`).
- Changes: `menu.AgentLanguageChoices` / `agentLanguageChoices` (8 endonyms + append out-of-list); TUI interface row uses `huh.Select`; `AGENTS.md` one-line note; menu + tui tests.
- Commit: `9306fb99431910dd83338a4aa6c70022efa67399` on `develop`.
- Verify: `go build ./...`, `go vet ./...`, `go test ./...` all pass (before review and again after rebase).
- Review: PM pass (codex, no gate findings); QA pass (codex, no gate findings); CSA/Hacker N/A (repo policy + stages skip).
- Integrate: pushed to `origin/develop`; local ff; worktree and task branch cleaned up.

## SUMMARY

Options panel Agent language is a fixed eight-choice `huh.Select` with endonym labels; out-of-list stored values appear as an extra option and are kept until the user picks another. Schema/validation and `kander config` unchanged. Delivered as `9306fb9` on `develop`. Acceptance 5/5.
