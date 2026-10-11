# Templates

Files in this directory are Go `text/template` sources that Kander renders itself and hands to an agent as part of a prompt, a task file, or a command's output. An agent never reads these files directly: the rendered text reaches it through Kander.

Writing rules:

- English only. The language the agent uses with the user comes from the language directive Kander renders into the prompt.
- Template data holds instance facts only (task ID, dispatch ID, epoch, paths, language, size, card list, plan notes). Rules and protocol text belong in the rule files or in `../snippets/`.
- Every field a template reads must be present in the data; rendering runs with `missingkey=error` and fails as a whole instead of emitting partial text.
- Card bodies, messages, and other user content are passed as data and are never parsed as template text.

Kander installs this directory next to the rule files and `kander doctor` tracks every file in it like a rule file: outdated files are replaced, locally modified files are backed up before replacement.
