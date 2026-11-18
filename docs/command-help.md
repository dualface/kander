# Command help

The help of every command is generated from its contract file, `rules/snippets/commands/<command>.md`. Nothing in Go code repeats a command's summary, usage, arguments, or examples. The help is English only, has no color, and does not follow `--lang`.

## Content source

| Help part | Contract source |
| --- | --- |
| Summary | The first sentence of `## Purpose and users`: the text up to the first period outside inline code that is followed by a space or ends the paragraph. It ends with a period and has at most 25 words. |
| Usage | The code blocks of `## Syntax` and `## Additional Syntax` |
| Description | The whole of `## Purpose and users` |
| Arguments | The top-level list items of `## Arguments`, written `` - `name`, `name`: text ``. A command without arguments writes the single line `None.` |
| Examples | The code block of `## Examples`: pairs of a `# description` line and the `kander ...` line below it, with a blank line between pairs |
| Contract | The path of the contract below the rules root of the current scope, or `<rules root>` when no installation resolves |

- The help reads the contract copy embedded in the binary, so it always matches the command tree of that binary. The `Contract:` line names the installed copy, because an agent reads that file.
- When the installed copy is missing, or differs from the embedded one, the help says to run `kander doctor`: on stderr in text mode, as a notice in `notices` with `--json`.
- `request` is documented in `notify.md`. Its summary and description come from the `## Request command` section. The usage lines, examples, and argument entries of `notify` and `request` are separated by the first word of the line or of the entry name: lines that start with `kander notify` belong to `notify`, lines that start with `kander request` to `request`, and an argument entry belongs to a command when the first word of one of its names is a word of that command's usage lines.
- A subcommand shows the help of its top-level command: `kander server start --help` prints the `server` help. The `subcommands` field of the JSON help groups the usage lines by subcommand.
- `worker` is an internal command without a contract. `kander worker --help` prints one line that says so.
- The command order of the index is the order of the command table of `rules/snippets/commands/README.md`, with `request` after `notify`.

## Requests

| Request | Result |
| --- | --- |
| `kander <command> [<subcommand>] --help`, `-h`, `kander help <command> [<subcommand>]` | The text help of the command on stdout, exit 0 |
| `kander --help`, `kander help` | The text index of the commands on stdout, exit 0 |
| `kander <command> --help --json`, `kander help --json <command>` | The JSON object of the command |
| `kander --help --json`, `kander help --json` | The JSON index of the commands |

An argument error prints one short usage, the contract Syntax lines and the `Contract:` path, on stderr and exits 2.

## Text format

Description and Arguments are wrapped at 80 columns. Usage lines, examples, and the flag list are not wrapped.

```text
<summary>

Usage:
  <syntax line>

Description:
  <Purpose and users>

Arguments:
  <name>, <name>
      <text>

Examples:
  # <description>
  kander ...

Global flags:
  --lang {cn,en,ja}   Interface language for command output (help is always English)
  --json              Print structured JSON output
  -h, --help          Show help for the command

Contract: <rules root>/snippets/commands/<command>.md
<read sentence>
```

The index starts with `Kander: kanban orchestration for coding agents.`, then the usage of the bare command, a `Commands:` list of every visible command with its summary (an alias after a comma: `list, ls`), the global flags, and the hint that names `kander <command> --help` and `kander help --json [<command>]`. The hidden `worker` is not listed.

## JSON format

All field names are snake_case. `schema_version` is 1.

A command:

```json
{
  "schema_version": 1,
  "command": "move",
  "aliases": [],
  "summary": "...",
  "description": "...",
  "usage": ["kander move ..."],
  "arguments": [{"names": ["--owner <agent>"], "text": "..."}],
  "examples": [{"description": "...", "command": "kander move ..."}],
  "subcommands": [{"name": "start", "usage": ["kander server start ..."]}],
  "global_flags": [{"name": "--json", "text": "..."}],
  "contract": "<rules root>/snippets/commands/move.md",
  "read_sentence": "...",
  "notices": []
}
```

- `command` is the top-level command that owns the contract, also for a subcommand. `aliases`, `usage`, `arguments`, `examples`, `subcommands`, `global_flags`, and `notices` are always arrays, empty when there is nothing to list.
- A notice is `{"code": "...", "message": "..."}` with the fixed English message of help. The codes are `contract_missing` and `contract_outdated`.

The index:

```json
{
  "schema_version": 1,
  "commands": [{"name": "list", "aliases": ["ls"], "summary": "...", "contract": "<path>"}],
  "global_flags": [{"name": "--json", "text": "..."}],
  "notices": []
}
```

## Tests

- `rules/commands_test.go` checks the contract parts: the fixed section order with `## Examples` after `## Arguments`, the summary (one sentence, at most 25 words), the argument entries, `None.`, that every long flag of a Syntax line is the name of an argument entry, the examples, and the `## Request command` section.
- `cmd/kander/help_contract_e2e_test.go` checks the output for every command path: the text format and its section order, the JSON schema, the equality of the forms of one request, that `--lang` changes nothing, the notices, and that every example parses against the command tree and every command path has one.
- `cmd/kander/cobra_tree_test.go` checks that the flags a command registers are the flags its Syntax lines name.
