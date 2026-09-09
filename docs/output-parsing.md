# Declarative Output Parsing

This document is the published contract for the output parser and placeholder
rules in `internal/process`. Review templates and terminal definitions link
here; they do not restate the field set.

The contract is not expandable on these points: there are exactly three parse
primitives; `format` / `select` / `join` describe stream shape, not a fourth
primitive; placeholders are replaced per element or per text template; there
is no shell interpolation.

## Structure

```text
{
  "source":  "stdout" | "stderr" | "file",
  "format":  "json" | "ndjson",
  "select":  [<line condition>, ...],
  "parse":   "raw" | "json_field:<dotted.path>" | "regex:<pattern>",
  "join":    "<string>",
  "success": [<line condition>, ...]
}
```

- `source` names where the caller already obtained the bytes: standard output,
  standard error, or a result file the caller read. This package does not
  start processes or open those files.
- `format` defaults to `json`. `json` treats the whole buffer as one document
  and evaluates `parse` once. `select` (including an empty array) or `join`
  on `json` is rejected.
- `parse` is one of three primitives: the raw buffer, a dotted JSON object
  path that must yield a string, or a regular expression with exactly one
  capturing group. The regex is compiled at validation time.
- Callers restrict `source` through dedicated entries: review accepts
  `stdout` | `file` and rejects `stderr`; terminal accepts `stdout` | `stderr`
  and rejects `file`.

## Line conditions

`select` and `success` share one vocabulary and one evaluator:

```text
{"json_field": "<dotted.path>", "equals": <JSON value>}
{"json_field": "<dotted.path>", "absent": true}
```

A condition has `json_field` plus exactly one of `equals` or `absent: true`.
`equals` may be any JSON value, including `null`. Invalid JSON in a Go
`LineCondition.Equals` value is rejected by validation, before evaluation.
`absent: true` is true only
when that path is missing on the same document; a sibling line without the
path does not make it vacuously true.

## Evaluation

Success is decided before text is extracted. Extracted text is rejected when
declared success conditions fail.

### `format: json`

All `success` conditions are evaluated on the single document. If the buffer
is not JSON and `success` is declared, success fails (it is not skipped and
not vacuously true).

### `format: ndjson`

Order is split, then select, then extract, then join:

1. Split on lines. Empty lines are ignored. A line that is not JSON,
   including a truncated last line, is skipped and is not a failure.
2. When `select` is present, a line enters extraction only if it satisfies
   every `select` condition. Without `select`, every JSON line enters.
   Path-absent skips are not a substitute for `select`: tool and meta rows
   often carry the same field names as report rows.
3. Each selected line applies `parse`. A missing path or a regex miss skips
   that line and is not a failure.
4. Hits are concatenated in order with `join`, which defaults to `"\n"`.
   A missing separator is not the contract: an ndjson line is usually a whole
   message, and gluing them breaks Markdown structure.

`parse: raw` with `ndjson` is rejected. Joining raw lines would replay the
input and make `select` meaningless.

`success` on ndjson is existential conjunction: some one line satisfies every
condition. Conditions split across lines fail. `success` sees every JSON
line; it is not limited by `select`, because the success signal often lives
on a meta or result row that select drops.

When `success` is omitted, the caller falls back to the process exit code and
to the extracted text being non-empty.

## Placeholders and escaping

The same rules apply to argv elements and to multiline text templates. The
caller supplies the whitelist.

- `{name}` is a placeholder only when `name` is on the whitelist.
- An unknown `{name}` is rejected. The error includes that name.
- Literal braces are written `{{` and `}}`.
- Replacement is not recursive. Values inserted at runtime are not scanned
  for braces.
- Empty argv elements are rejected.
- Control characters in the template:
  - argv: reject every Unicode control character (including TAB, LF, CR, NUL)
  - argv that must carry a TAB (terminal `-F` format strings): allow TAB only
  - text templates: allow TAB, LF, and CR; reject other controls, including
    NUL. A newline is ordinary Markdown / YAML content.

Runtime values are not re-checked against these template rules. Callers apply
their own value checks (for example rejecting LF or NUL in a command).
