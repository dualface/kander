package install

import (
	"path"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// maxTreeLinks bounds the symlink hops followed inside a commit tree.
const maxTreeLinks = 8

// TreeFile is one path of a commit tree as Git stores it: a regular file with its content, or
// a symlink whose content is the link target. Found is false for a missing path or one that is
// not a file.
type TreeFile struct {
	Data    []byte
	Symlink bool
	Found   bool
}

// TreeReader reads one slash-separated path, relative to the root, from a commit tree.
type TreeReader func(rel string) (TreeFile, error)

// ProjectRulesApplicable reports whether kander init writes the agent's project rules file in
// the main worktree root under the switches, and returns that file relative to the root. An
// agent without an embedded definition or without a project rules file is never applicable,
// and neither is one whose file a reader with install_rules off shares (DisabledReaders).
func ProjectRulesApplicable(agent, root string, switches config.InstallRules) (string, bool) {
	rel := ProjectRulesRelative(agent)
	if rel == "" {
		return "", false
	}
	if len(DisabledReaders(ProjectRulesTarget(agent, root), root, switches)) > 0 {
		return rel, false
	}
	return rel, true
}

// MissingProjectReferenceInTree checks, in a commit tree, the project rules files through which
// the agent loads the Kander rules, with the same rules as ProjectReferenceStatus, and returns
// the first one, relative to the root, that does not reference the global rules entry; "" means
// every file does. A symlink resolves inside the tree, and for a file in the claude-import style
// a whole-line `@` import of another file of the tree (CLAUDE.md importing AGENTS.md) counts
// when that file carries the reference. An AGENTS.override.md next to an AGENTS.md shadows it
// for the agents that read it, so when the tree holds one, it must reference the entry too.
// baseDir is the main worktree the tree belongs to.
func MissingProjectReferenceInTree(agent string, paths config.InstallPaths, baseDir string, read TreeReader) (string, error) {
	rel := ProjectRulesRelative(agent)
	if rel == "" {
		return "", nil
	}
	present, err := treeFileReferences(agent, rel, paths, baseDir, read)
	if err != nil || !present {
		return rel, err
	}
	if path.Base(rel) != "AGENTS.md" {
		return "", nil
	}
	override := path.Join(path.Dir(rel), "AGENTS.override.md")
	file, err := read(override)
	if err != nil {
		return override, err
	}
	if !file.Found {
		return "", nil
	}
	_, text, found, err := readTreeFollowing(override, read)
	if err != nil || !found || !referenceStyleFor(agent).Present(text, RulesEntry(paths), baseDir) {
		return override, err
	}
	return "", nil
}

// treeFileReferences reports whether rel, read from the tree, references the global rules
// entry, following an import of the claude-import style.
func treeFileReferences(agent, rel string, paths config.InstallPaths, baseDir string, read TreeReader) (bool, error) {
	resolved, text, found, err := readTreeFollowing(rel, read)
	if err != nil || !found {
		return false, err
	}
	entry := RulesEntry(paths)
	style := referenceStyleFor(agent)
	if style.Present(text, entry, baseDir) {
		return true, nil
	}
	if style.Name() != (claudeImport{}).Name() {
		return false, nil
	}
	for _, line := range liveLines(text) {
		match := claudeImportRE.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		imported, ok := treeRelative(path.Dir(resolved), match[1])
		if !ok {
			continue
		}
		_, importedText, found, err := readTreeFollowing(imported, read)
		if err != nil {
			return false, err
		}
		if found && (markdownReference{}.Present(importedText, entry, baseDir) || claudeImport{}.Present(importedText, entry, baseDir)) {
			return true, nil
		}
	}
	return false, nil
}

// readTreeFollowing reads rel, following symlinks that stay inside the tree, and returns the
// path of the regular file it reached with its text.
func readTreeFollowing(rel string, read TreeReader) (string, string, bool, error) {
	current := rel
	for range maxTreeLinks {
		file, err := read(current)
		if err != nil || !file.Found {
			return current, "", false, err
		}
		if !file.Symlink {
			return current, string(file.Data), true, nil
		}
		next, ok := treeRelative(path.Dir(current), string(file.Data))
		if !ok {
			return current, "", false, nil
		}
		current = next
	}
	return current, "", false, nil
}

// treeRelative resolves target against dir inside the tree. An absolute target, a ~ path, or
// one that climbs above the root leaves the tree.
func treeRelative(dir, target string) (string, bool) {
	target = strings.ReplaceAll(target, "\\", "/")
	if target == "" || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.Contains(target, ":") {
		return "", false
	}
	joined := path.Clean(path.Join(dir, target))
	if joined == ".." || strings.HasPrefix(joined, "../") || joined == "." {
		return "", false
	}
	return joined, true
}
