package install

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/rules"
)

// legacyRulesDirname is the last element of the current global rules root; earlier releases
// wrote the rule files one level up, directly into the agents directory.
const legacyRulesDirname = "kander"

// LegacyRulesMigration records what the installer did with rule files found at the previous
// global rules location.
type LegacyRulesMigration struct {
	// Removed lists the files deleted from the old root.
	Removed []string
	// Kept lists paths left in place because they could not be removed: directories and
	// files whose removal failed.
	Kept []string
}

// legacyRulesDir returns the flat rules root earlier global installs used, or "" when the
// scope has no previous location to migrate from.
func legacyRulesDir(paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject || filepath.Base(paths.RulesDir) != legacyRulesDirname {
		return ""
	}
	return filepath.Dir(paths.RulesDir)
}

// legacyRulesEntry returns the previous location of the rules entry, or "" when there is none.
func legacyRulesEntry(paths config.InstallPaths) string {
	dir := legacyRulesDir(paths)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "KANDER-AGENTS.md")
}

// migrateLegacyRules deletes the rule files and the installer state file from the previous
// global rules root after the current root has been written. Every file with a rule name goes,
// whatever its content or link status: the old location is retired, and a stale copy there
// would only be read by mistake. Directories are left alone. It never fails the install and
// is silent by design; the result exists for tests and diagnostics.
func migrateLegacyRules(paths config.InstallPaths) LegacyRulesMigration {
	var out LegacyRulesMigration
	dir := legacyRulesDir(paths)
	if dir == "" {
		return out
	}
	names := append(rules.Names(), stateFileName)
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			out.Kept = append(out.Kept, path)
			continue
		}
		anchor, err := fileAnchor(path)
		if err != nil {
			out.Kept = append(out.Kept, path)
			continue
		}
		if _, err := fs.RemoveNonDirectoryIfExists(anchor, path); err != nil {
			out.Kept = append(out.Kept, path)
			continue
		}
		out.Removed = append(out.Removed, path)
	}
	cleanupAgentsEntryLinkIn(dir)
	return out
}

// removeLegacyEntrySymlinks drops agent rules files that are symlinks aimed at the previous
// rules entry once that entry is gone. The following integration pass recreates them as
// reference files pointing at the current entry, so no agent is left reading a dangling link.
// Returns the targets it removed.
func removeLegacyEntrySymlinks(paths config.InstallPaths) []string {
	legacy := legacyRulesEntry(paths)
	if legacy == "" || legacyEntryExists(legacy) {
		return nil
	}
	var removed []string
	seen := map[string]struct{}{}
	for _, agent := range config.ExecutionAgents {
		target := AgentRulesTarget(agent, paths)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		info, err := os.Lstat(target)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		link, err := os.Readlink(target)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(target), link)
		}
		if filepath.Clean(link) != filepath.Clean(legacy) {
			continue
		}
		if err := os.Remove(target); err != nil {
			continue
		}
		removed = append(removed, target)
	}
	return removed
}

// legacyEntryExists reports whether anything still sits at the previous entry path.
func legacyEntryExists(legacy string) bool {
	_, err := os.Lstat(legacy)
	return err == nil
}

// legacyEntrySpellings is every spelling of the previous entry a rules file may carry: the
// accepted spellings of entrySpellings, the base-directory-relative form even when it climbs
// through "..", and the absolute form under a resolved (symlinked) home directory.
func legacyEntrySpellings(legacy, baseDir string) map[string]struct{} {
	spellings := entrySpellings(legacy, baseDir)
	if rel, err := filepath.Rel(baseDir, legacy); err == nil {
		spellings[rel] = struct{}{}
		spellings[filepath.ToSlash(rel)] = struct{}{}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, legacy); err == nil && !strings.HasPrefix(rel, "..") {
			if resolvedHome, err := filepath.EvalSymlinks(home); err == nil && resolvedHome != home {
				abs := filepath.Join(resolvedHome, rel)
				spellings[abs] = struct{}{}
				spellings[filepath.ToSlash(abs)] = struct{}{}
			}
		}
	}
	return spellings
}

// legacyEntryReplacements maps every spelling of the previous rules entry to the same spelling
// of the current one, so a reference written by an earlier installer can be rewritten in place.
// Both slash styles are covered because references always use forward slashes on disk.
func legacyEntryReplacements(paths config.InstallPaths, baseDir string) map[string]string {
	legacy := legacyRulesEntry(paths)
	if legacy == "" {
		return nil
	}
	out := map[string]string{}
	for spelling := range legacyEntrySpellings(legacy, baseDir) {
		for _, sep := range []string{"/", string(os.PathSeparator)} {
			suffix := sep + "KANDER-AGENTS.md"
			stem, ok := strings.CutSuffix(spelling, suffix)
			if !ok || stem == "" {
				continue
			}
			out[spelling] = stem + sep + legacyRulesDirname + suffix
			break
		}
	}
	return out
}

// rewriteLegacyReference replaces references to the previous rules entry with the current one.
// It reports whether anything changed. Nothing is rewritten while something still exists at the
// previous entry path (a directory the migration could not remove), because that reference may
// still be meant.
func rewriteLegacyReference(text string, paths config.InstallPaths, baseDir string) (string, bool) {
	legacy := legacyRulesEntry(paths)
	if legacy == "" {
		return text, false
	}
	if legacyEntryExists(legacy) {
		return text, false
	}
	replacements := legacyEntryReplacements(paths, baseDir)
	if len(replacements) == 0 {
		return text, false
	}
	// Longer spellings first so an absolute path is not partially rewritten by a shorter suffix.
	keys := slices.SortedFunc(maps.Keys(replacements), func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), cmp.Compare(a, b))
	})
	changed := false
	for _, key := range keys {
		var rewritten bool
		text, rewritten = replaceWholePath(text, key, replacements[key])
		changed = changed || rewritten
	}
	return text, changed
}

// replaceWholePath replaces every occurrence of old that is not embedded in a longer path:
// the characters on both sides must not be path characters, so "~/.agents/KANDER-AGENTS.md.bak"
// and "/x/home/u/.agents/KANDER-AGENTS.md" are left alone.
func replaceWholePath(text, old, replacement string) (string, bool) {
	var out strings.Builder
	changed := false
	offset := 0
	for {
		index := strings.Index(text[offset:], old)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(old)
		out.WriteString(text[offset:start])
		if (start == 0 || !isPathChar(text[start-1])) && (end == len(text) || !isPathChar(text[end])) {
			out.WriteString(replacement)
			changed = true
		} else {
			out.WriteString(old)
		}
		offset = end
	}
	if !changed {
		return text, false
	}
	out.WriteString(text[offset:])
	return out.String(), true
}

func isPathChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("._-/\\~:", c) >= 0
}
