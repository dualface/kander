# kander doctor

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Checks the installation and repairs what it can. The check covers the binary, rules root, agent rules integration, terminal tools, and the background worker of the client; missing or outdated rule files, templates, and snippets are rewritten from the embedded copies; locally modified files are listed and, in an interactive session, backed up and replaced on request. Run by the user, also after every upgrade.

## Syntax

```text
kander doctor
```

## Arguments

None.

## Examples

```text
# Check the installation and repair it
kander doctor
```

## Preconditions

- An installed Kander.

## Effects

- Rewrites missing and outdated files below the rules root and stamps `kander-rules-state.json`.
- Removes what earlier releases wrote into the agents' global rules files, on every run, whatever the cleanup marker says (`snippets/commands/install.md`); it never writes a global rules file otherwise. A failed cleanup fails the check.
- Installs or updates the covered agents' rules extensions, except for an agent whose `install_rules` switch is off (`agents.<name>.install_rules`, else the top-level `install_rules`; read only from the scope `config.json`). For such an agent it neither reads nor writes the file and prints one "turned off" line; that line does not fail the check.
- Installs the bundled `kander-leader` skill where it is missing and updates a managed copy, at the same Codex and Claude destinations and with the same `install_rules` gate as `kander install`. A locally edited skill file, a skill directory or link without the install's receipt, and a destination below a linked ancestor are reported and kept.
- In a checkout bound to a project (the overlay sets `project.name`), reports for each project rules file `kander init` writes whether it references the Kander rules entry; a missing reference, one that appears only inside an HTML comment, a code fence, or a negated sentence, and a reference to the retired `.kander/rules/KANDER-AGENTS.md` fail the check with the hint to run `kander init` and commit and push. Doctor never writes a project rules file.
- In a Git checkout with what a retired project install left in the main worktree, reports each item, and the repair removes what Kander wrote there: a `.kander/bin/kander` (`kander.exe` on Windows) whose `version` output starts with `kander `, the files of `.kander/rules/` whose digest is a released one and Kander's state files there, the project-level `kander-leader` skill with its install receipt and the project `.pi/extensions/kander-rules.ts` whose digest is a released one, the `/.kander/` line of `.git/info/exclude`, and the directories left empty. It moves the top-level settings of `.kander/config.json` the project overlay allows into `.kander-config.json`; a key the overlay already sets keeps the overlay value and is reported, and a setting the overlay cannot hold (`install_rules`, `agents.<name>.install_rules`, `agents.<name>.config_dir`) that differs from the default is reported and keeps the file. The file is removed when nothing stays in it. Terminal definitions below `.kander/share/` stay, with the hint to move them into the global share. Nothing is removed through a symlink, junction, or other reparse point; a modified copy stays and is reported.
- `install_rules` keys in the project overlay are ignored with one warning each.
- Interactive sessions may back up and replace locally modified files.
- Reads the background worker of the client directory without creating or changing anything (no client directory, lock file or record is created) and stops no process. It compares the version in the worker's record (`worker.json`) with its own: a worker that runs another version, or runs without a usable version record, is a warning that fails the check and names what to stop; an absent worker is reported as not running and does not fail it.
- Never changes the board.

## Output

- One line per check, then the overall result. The report lists each setting this version ignores (an unknown key, a `models.*` entry of an unknown agent), each `agents` entry it cannot use with its reason, and each role that names an agent it does not know; they are reports, never make the check fail, and the repair keeps them. When the cached project policy applies (revision above 0), the report lists each value of `config.json` or `.kander-config.json` that the policy replaces with another value, one line each with the JSON path, the file, the file value, and the policy value; these lines change no file and never make the check fail. With `--json` each of them is also a `project_policy_override` notice. The worker check prints one line: not running, running with its version and pid, or a warning (another version, or no version record) with the way out. With no client directory it prints none. With `--json`: `healthy` (boolean) and `lines` (array of `level`, one of `ok`, `warn`, `info`, `note`, and `message`). `doctor --json` never asks a question. When an interactive doctor would ask something before it repairs (the backup and replacement of a modified rule file, the replacement of an unusable agent, the storing of an agent session file, the herdr install, the Homebrew upgrade, the choice of the kander executable to keep when several are on PATH), the run is refused as an interactive form (`interactive_required`, exit 2) before any repair, and nothing is repaired; run `kander doctor` in a terminal. The version guard comes first: when the running binary is older than one on PATH, doctor stops with that diagnostic as the result, never with this refusal. Otherwise it repairs and reports as usual. An unhealthy installation is the result with `healthy` false and exit status 1.

## Rejections

- Any argument is a usage error.
