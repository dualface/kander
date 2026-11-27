package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/rules"
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
	if filepath.Base(paths.RulesDir) != legacyRulesDirname {
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
// is silent by design; the result exists for tests and diagnostics. While any agent's global
// rules target cannot be resolved, the migration waits: the global injection cleanup could not
// remove a link aimed at the previous entry there, and deleting the entry would leave the link
// dangling. The cleanup reports the resolution error, and the next successful run migrates.
func migrateLegacyRules(paths config.InstallPaths) LegacyRulesMigration {
	var out LegacyRulesMigration
	dir := legacyRulesDir(paths)
	if dir == "" || !agentRulesTargetsResolve(paths) {
		return out
	}
	names := append(rules.Names(), stateFileName)
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
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

func isPathChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("._-/\\~:", c) >= 0
}

// agentRulesTargetsResolve reports whether every embedded agent's global rules target resolves.
func agentRulesTargetsResolve(paths config.InstallPaths) bool {
	for _, agent := range config.ExecutionAgents {
		if _, err := GlobalRulesTarget(agent, paths); err != nil {
			return false
		}
	}
	return true
}
