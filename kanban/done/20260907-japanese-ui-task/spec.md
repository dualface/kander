# Add Japanese (ja) interface language

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- CREATED_AT: 2026-09-07 10:22
- OWNER: cursor
- SESSION: cursor 281b1c38-72c0-46c1-b4da-a3bbd346a784
- WINDOW: herdr:wX:tM:wX:p14
- STARTED_AT: 2026-09-07 10:23
- FINISHED_AT: 2026-09-07 10:50
- TASK_BRANCH: japanese-ui
- RESULT: completed

## GOAL

Add Japanese as a third interface language of the `kander` command, selectable as `--lang ja`, in the install wizard, and in the options panel "Default language" row, with a complete Japanese message catalog so every prompt, error, wizard step, doctor line, TUI label and launch prompt renders in Japanese.

## USER_DECISIONS

- Language code is `ja`, matching the existing two-letter style (`cn`, `en`).
- The catalog is a full translation of every key; no key may fall back to Chinese.
- Launch/resume prompts sent to agents (`launch.prompt.*`) keep following the interface language, as they do today for cn/en. Making them English-only and relying on `agent_language` is a separate later change, not this card.
- The `language` to `agent_language` derivation gains `ja -> ja`.

## EXPECTED_OUTCOME

- `config.Languages` is `cn, en, ja`; `--lang ja`, `KANDER_LANG=ja` and `"language": "ja"` are accepted; an invalid value error lists all three.
- `internal/i18n/locales/ja.json` exists with exactly the same key set and template arguments as `en.json` and `zh-CN.json`; the loader registers `ja`; `i18n.Text("ja", ...)` returns Japanese.
- Locale auto-detection maps a locale starting with `ja` to `ja` (as `en*` maps to `en`); everything else keeps the current behavior.
- The install wizard language step offers 日本語; after choosing it the remaining wizard steps, the handoff `--lang`, doctor and the options panel are in Japanese.
- The options panel "Default language" and `kander config` show a Japanese label for `ja`.
- Usage text `--lang {cn,en,ja}` in both existing catalogs and the new one; `AGENTS.md` and `README.md` mention `ja`.
- `launch` prompt-prefix matching (session reverse lookup for Codex) includes the Japanese heads so sessions started under `--lang ja` can be found by `resume` and `notify`.

## ACCEPTANCE_CRITERIA

- [x] `config.Languages`, `languageLabels`, `agentLanguageDefaults`, `ResolveLanguage` locale detection, `SetLanguageIfPresent`, CLI `--lang` validation and the wizard option list all accept and present `ja`.
- [x] `locales/ja.json` has the same keys as `en.json`; the catalog parity test compares all three catalogs (key set, template arguments, non-empty, parseable) and passes.
- [x] Commands, flags, paths, config keys, field names and status tokens inside messages stay verbatim in the Japanese text; only prose is translated.
- [x] `promptPrefixes` in `internal/launch/session.go` iterates `cn, en, ja`; a test proves a Japanese start head is matched.
- [x] `cli.usage` and `cli.global_options` in all catalogs read `--lang {cn,en,ja}`; `AGENTS.md` and `README.md` updated.
- [x] Tests for `ResolveLanguage` with a `ja_JP` locale, for `DefaultAgentLanguage("ja")`, and for the wizard default when the resolved language is `ja`.
- [x] `go build ./...`, `go vet ./...`, `go test ./...` pass.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- Pre-existing problems: existing wording issues in the cn/en catalogs are excluded; fixing them widens the task.
- Concurrency, cross-platform and security hardening: excluded; no Windows-specific path changes.
- Shared contracts and docs: the rules under `rules/` stay English-only by earlier decision; `agent_language` semantics are unchanged apart from the `ja` derivation. The options-panel agent-language select is the separate card `20260907-agent-language-choices-task`, which may run in parallel; do not edit `internal/tui/options_form.go` or `internal/menu/options_helpers.go` beyond what `config.Languages` growth requires (they iterate the slice, so likely nothing).
- Adjacent features: other interface languages (ko, zh-TW, ...) and making launch prompts language-independent are excluded.

## DISCUSSION

SELF_REVIEW: Goal and outcome match the request for Japanese prompts and UI. The seven hard-coded language sites found in the code (enum, usage text, locale detection, wizard, config labels, agent_language derivation, launch prompt-prefix loop) are each covered by an acceptance criterion so none is missed. The parity test criterion prevents partial catalogs. The boundary with the parallel options-panel card is explicit. No user decision was invented: the code, the prompt-language decision and the derivation were confirmed by the user in the plan.

## IMPLEMENTATION

- Branch/worktree: `japanese-ui` at `worktrees/japanese-ui`; rebased onto latest `origin/develop` before integration.
- Commits (final delivery `c643f9364392b8dc1ff328cf75c0f68a63ad3910` on `develop`):
  - `feat: add Japanese (ja) as a third interface language`
  - `fix(i18n): translate remaining Japanese UI labels` (QA-001 round 1)
  - `fix(i18n): finish Japanese effort and launcher labels` (QA-001 round 2)
  - `fix(i18n): add Japanese string for new board option key` (post-rebase catalog parity)
- Verification: `go build ./...`, `go vet ./...`, `go test ./...` passed on the delivery tip after rebase.
- Review: PM PASS on first commit; QA blocking then PASS after two fix rounds; CSA/Hacker N/A (repo special-case).
- Integration: pushed to `origin/develop`, main worktree ff-only synced; task branch/worktree removed.


## SUMMARY

Japanese is a third interface language (`--lang ja` / `language: ja` / wizard 日本語 / locale `ja*`). Full `locales/ja.json` (786 keys) stays in parity with en/zh-CN; config/menu/doctor/TUI/launch prompts render Japanese without Chinese fallback. `agent_language` derives `ja -> ja`. Codex session reverse lookup matches Japanese start/resume/takeover heads. Delivered on `develop` at `c643f9364392b8dc1ff328cf75c0f68a63ad3910`. Acceptance criteria all met.
