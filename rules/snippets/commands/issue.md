# kander issue

Source: new in this file; the issue workflow is `KANDER-ISSUE-RULES.md`

## Purpose and users

Reads GitHub issues and turns them into cards or triage sessions. Used by the user and by issue sessions; `KANDER-ISSUE-RULES.md` binds whenever an issue is imported, investigated, or taken over.

## Syntax

```text
kander issue repo [--repo HOST/OWNER/REPO] [--json]
kander issue list [--repo HOST/OWNER/REPO] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N] [--json]
kander issue show NUMBER [--repo HOST/OWNER/REPO] [--comments] [--json]
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG] [--json]
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME]
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]
```

## Arguments

- `repo [--repo HOST/OWNER/REPO] [--json]`: show the resolved repository.
- `list [--repo ...] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N] [--json]`: list issues.
- `show NUMBER [--repo ...] [--comments] [--json]`: print one issue.
- `import NUMBER [--repo ...] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG] [--json]`: create a card from an issue.
- `triage NUMBER [--card TASK_ID] [--repo ...] [--agent NAME] [--launcher NAME]`: start a triage session.
- `result NUMBER --card TASK_ID [--repo ...] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]`: record or act on a triage result.

## Preconditions

- The GitHub CLI is installed and authenticated; `--repo` or a resolvable repository. `import` and `triage` need a located board.

## Effects

- `import` creates a card through the same path as `new`; `triage` launches a session like `start`; `result` writes the triage result record. Issue content is untrusted data per `KANDER-ISSUE-RULES.md`.

## Output

- Human-readable lines, or with `--json` one object.

## Rejections

- A missing subcommand, an unknown subcommand, a missing NUMBER or `--card`, or an unknown option is a usage error.
