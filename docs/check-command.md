# Check command (`kander check`)

`kander check` has three forms. The no-mode form is the existing board structural validator plus, in the full binary, the liveness section. `delivery` and `overlap` are board-free, read-only Git analyzers. They do not locate `kanban/`, do not fetch, and do not write refs, the index, or the worktree.

```sh
kander check [--all] [task-id ...]
kander check delivery --base <ref> [--commit <ref>] [--json]
kander check overlap --source <ref> [--head <ref>] [--json]
```

Git is invoked with `exec.CommandContext` and a direct argv. There is no shell, and refs are resolved to commit IDs with `--end-of-options` before any diff. Paths are read from `git diff --name-status -z --find-copies-harder`. Every Git subprocess sets `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`, and a C locale so missing objects fail instead of fetching and so error JSON stays stable. Output never includes host-absolute paths or timestamps.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | The check completed and needs no action (`pass`) |
| 1 | The check completed with findings (`fail`, `review-required`, or `action-required`) |
| 2 | Argument error |
| 3 | Git, repository, ref/ancestry, output-limit, or internal error |

An incomplete check never reports PASS. `--json` writes one object and a trailing newline on stdout for completed results and for recognized argument, Git, output-limit, and internal errors. Successful JSON runs leave stderr empty.

## Delivery

`--commit` defaults to `HEAD`. Base must be an ancestor of the target. `diff_check` is equivalent to `git diff --check` with `core.quotePath=true`. Physical line counts come from commit blobs: a last line without a trailing newline still counts as one line.

- Added or copied files with more than 1000 lines go in `added_over_limit`.
- Modified, renamed, or type-changed files whose base count is at most 1000 and whose target count is more than 1000 go in `crossed_limit`.
- Deletes are ignored. Rename and copy keep `base_path` as the old path.
- Kander does not classify source versus generated files. Any line-count candidate yields overall `review-required` (unless `diff_check` failed, which is `fail`). Agents record a disposition per candidate.

Status precedence: `error > fail > review-required > pass`.

JSON top-level fields, and only these: `schema_version` (1), `check` (`delivery`), `status`, `base_commit`, `target_commit`, `diff_check`, `added_over_limit`, `crossed_limit`, `error`. `diff_check` has `status` (`pass` or `fail`) and `diagnostics`. Each candidate has `status`, `path`, `base_path`, `base_lines`, `target_lines`. Empty arrays encode as `[]`. `error` is `null` when the check completed.

## Overlap

`--head` defaults to `HEAD`. The command resolves source and head, computes the merge base, and intersects the changed paths of both sides. Rename and copy contribute both the old path and the new path. Status precedence: `error > action-required > pass`.

JSON top-level fields, and only these: `schema_version` (1), `check` (`overlap`), `status`, `merge_base`, `head_commit`, `source_commit`, `paths`, `error`. Empty `paths` encode as `[]`.

## GitPath

Every `path`, `base_path`, and `paths` element is `{"utf8": <string-or-null>, "base64": <string-or-null>}`. Valid UTF-8 uses `utf8` and `base64: null`. Invalid UTF-8 uses RFC 4648 standard base64 and `utf8: null`. A missing `base_path` is JSON `null`. Sorting and set comparison use the raw Git path bytes before encoding.

## Human output

Without `--json`, en / zh-CN / ja catalogs print a short report. Untrusted paths and diagnostics are escaped onto one terminal line: newlines, ANSI ESC, C1 controls, and other non-printing runes cannot inject extra rows or sequences. `--json` is honored even when it appears after another argument error, so recognized usage failures still emit one JSON object.
