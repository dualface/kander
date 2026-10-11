# Snippets

Files in this directory are static rule excerpts that an agent reads only when it reaches the phase they describe. Kander names the file to read in the prompt or output that starts that phase; the agent reads the file by its absolute path.

Writing rules:

- English only, plain Markdown, no template tags.
- One file per phase or per command. A snippet moves text out of a rule file; it never duplicates text that still lives in a rule file.
- The first line is a level-one heading. A snippet moved from a rule file keeps a `Source:` line naming the file and section it came from.
- Cross references use the same forms as the rule files: a rule file name with a quoted section, or a `snippets/...` path.

Kander installs this directory next to the rule files and `kander doctor` tracks every file in it like a rule file: outdated files are replaced, locally modified files are backed up before replacement.
