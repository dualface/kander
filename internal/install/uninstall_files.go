package install

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/rules"
)

// planReferenceRemoval plans dropping the Kander load commands from one global rules file. A
// file left with only blank lines, or a .mdc rule left with only the alwaysApply frontmatter
// Kander wrote, is deleted; an AGENTS.override.md sibling and a symlinked rules file are only
// edited, and a .mdc rule the user changed keeps its other content. A rules file that is a
// symlink to the Kander rules entry, current or previous, is removed as a link.
func planReferenceRemoval(target string, override bool, paths config.InstallPaths) (UninstallItem, bool) {
	keep := func(reason string) (UninstallItem, bool) {
		return UninstallItem{Action: UninstallKeep, Path: target, Reason: reason}, true
	}
	fail := func(err error) (UninstallItem, bool) {
		return UninstallItem{Action: UninstallKeep, Path: target, Reason: err.Error(), failed: true}, true
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return UninstallItem{}, false
	}
	if err != nil {
		return fail(err)
	}
	linked := info.Mode()&os.ModeSymlink != 0
	if linked && linksToRulesEntry(target, paths) {
		// The link itself goes, never its target; it is removed below the resolved directory
		// so the removal does not walk through a symlinked config directory.
		dir, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return fail(err)
		}
		return UninstallItem{Action: UninstallRemove, Path: filepath.Join(dir, filepath.Base(target))}, true
	}
	format := fileFormatFor(target)
	if key := format.SymlinkRefusal(); linked && key != "" {
		return keep(config.Text(key, target))
	}
	// The write goes to the real path, so a config directory the user keeps behind a symlink
	// is edited in place.
	resolved, err := realRulesPath(target, linked)
	if err != nil {
		return fail(err)
	}
	if info, err := os.Stat(resolved); err != nil {
		return fail(err)
	} else if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("%s", config.Text("menu.cannot_read", target)))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fail(err)
	}
	stripped, removed := RemoveRulesReferences(string(data))
	if removed == 0 {
		return UninstallItem{}, false
	}
	rest := strings.TrimSpace(stripped)
	kanderOnly := rest == "" || format.OnlyKander(rest)
	// Deletion never follows a symlink: a file whose directory is reached through one is
	// emptied of the Kander lines instead.
	if kanderOnly && !linked && !override && resolved == filepath.Clean(target) {
		return UninstallItem{Action: UninstallRemove, Path: target}, true
	}
	item := UninstallItem{Action: UninstallEdit, Path: resolved, content: []byte(stripped)}
	if format.Prefix() != "" && !kanderOnly {
		// A rule file Kander created and the user changed afterwards stays without the
		// Kander load command; it is reported as kept.
		item.Reason = config.Text("install.global_rules_file_modified_kept")
	}
	return item, true
}

// linksToRulesEntry reports whether target is a symlink to the Kander rules entry: the current
// one, or the previous global entry, whose link may already dangle.
func linksToRulesEntry(target string, paths config.InstallPaths) bool {
	link, err := os.Readlink(target)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(target), link)
	}
	link = filepath.Clean(link)
	entries := []string{RulesEntry(paths)}
	if legacy := legacyRulesEntry(paths); legacy != "" {
		entries = append(entries, legacy)
	}
	for _, entry := range entries {
		if link == filepath.Clean(entry) || sameResolved(target, entry) {
			return true
		}
	}
	return false
}

// realRulesPath resolves a rules file to the path without symlinks: the link target for a
// symlinked file, otherwise the file below its resolved directory.
func realRulesPath(target string, linked bool) (string, error) {
	if linked {
		return filepath.EvalSymlinks(target)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(target)), nil
}

// planExtensionRemoval plans deleting one agent's rules extension when its digest is a released
// version; a locally modified copy stays.
func planExtensionRemoval(agent string, paths config.InstallPaths) (UninstallItem, bool) {
	outcome, err := InspectExtension(agent, paths)
	if outcome.Target == "" && err == nil {
		return UninstallItem{}, false
	}
	path := cmp.Or(outcome.Target, agent)
	if err != nil {
		return UninstallItem{Action: UninstallKeep, Path: path, Reason: err.Error()}, true
	}
	switch outcome.Status {
	case ExtensionInstalled, ExtensionOutdated:
		return UninstallItem{Action: UninstallRemove, Path: path}, true
	case ExtensionModified:
		return UninstallItem{Action: UninstallKeep, Path: path, Reason: config.Text("install.uninstall_extension_modified")}, true
	default:
		return UninstallItem{}, false
	}
}

// planRulesDirRemoval plans deleting the rule files and state files Kander installs into the
// rules directory; every other entry stays and is reported. Symlinks are never followed.
func planRulesDirRemoval(dir string) ([]UninstallItem, []string) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return []UninstallItem{{Action: UninstallKeep, Path: dir, Reason: err.Error()}}, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || kfs.IsReparsePoint(dir) || !info.IsDir() {
		return []UninstallItem{{Action: UninstallKeep, Path: dir, Reason: config.Text("install.target_is_symlink", dir)}}, nil
	}
	known := map[string]struct{}{stateFileName: {}, startupStateFileName: {}, globalCleanupFileName: {}, globalCleanupLockName: {}}
	for _, name := range rules.Names() {
		known[name] = struct{}{}
	}
	var items []UninstallItem
	dirs := []string{dir}
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			items = append(items, UninstallItem{Action: UninstallKeep, Path: path, Reason: err.Error()})
			return nil
		}
		if path == dir {
			return nil
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := known[filepath.ToSlash(rel)]; ok && entry.Type().IsRegular() {
			items = append(items, UninstallItem{Action: UninstallRemove, Path: path, rules: true})
			return nil
		}
		items = append(items, UninstallItem{Action: UninstallKeep, Path: path, Reason: config.Text("install.uninstall_not_kander_file")})
		return nil
	})
	if walkErr != nil {
		items = append(items, UninstallItem{Action: UninstallKeep, Path: dir, Reason: walkErr.Error()})
	}
	// Deepest first, so a parent is only tried after its children are gone.
	slices.SortFunc(dirs, func(a, b string) int {
		return cmp.Compare(strings.Count(b, string(os.PathSeparator)), strings.Count(a, string(os.PathSeparator)))
	})
	return items, dirs
}

// errSkillRemoval marks a failed removal of a bundled skill file, which stops the uninstall so
// the receipt stays and a later run can finish the bundle.
var errSkillRemoval = errors.New("skill removal failed")

// applyUninstallItems edits and removes the planned paths, reporting each to out, and returns
// the number of removed rules directory files, the paths it changed, and every failure joined.
func applyUninstallItems(items []UninstallItem, out io.Writer) (int, []string, error) {
	var failed error
	var changed []string
	removedRules := 0
	for _, item := range items {
		var err error
		switch item.Action {
		case UninstallEdit:
			err = writeUninstallEdit(item)
			if err == nil {
				fmt.Fprintln(out, config.Text("install.uninstall_edited", item.Path))
			}
		case UninstallRemove:
			if item.skillDigest != "" {
				if err = removeSkillFile(item); err != nil {
					// Keep the receipt so a later run can finish this bundle.
					return removedRules, changed, errors.Join(failed, errSkillRemoval, err)
				}
			} else {
				err = removeUninstallFile(item.Path)
			}
			if err == nil && item.rules {
				removedRules++
			} else if err == nil {
				fmt.Fprintln(out, config.Text("install.uninstall_removed", item.Path))
			}
		default:
			continue
		}
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", item.Path, err))
			continue
		}
		changed = append(changed, item.Path)
	}
	return removedRules, changed, failed
}

func writeUninstallEdit(item UninstallItem) error {
	anchor, err := fileAnchor(item.Path)
	if err != nil {
		return err
	}
	observeUninstallWrite(item.Path)
	return kfs.WriteBytesAtomicInherited(anchor, item.Path, item.content, true)
}

func removeUninstallFile(path string) error {
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	observeUninstallWrite(path)
	_, err = kfs.RemoveNonDirectoryIfExists(anchor, path)
	return err
}

// uninstallWriteObserver sees every path the uninstall file layer is about to change; tests
// record it to prove what uninstall itself writes and deletes.
var uninstallWriteObserver func(path string)

func observeUninstallWrite(path string) {
	if uninstallWriteObserver != nil {
		uninstallWriteObserver(path)
	}
}

// removeEmptyDirs deletes each listed directory that is empty, children first.
func removeEmptyDirs(dirs []string) error {
	var failed error
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			continue
		}
		anchor, err := fileAnchor(dir)
		if err == nil {
			observeUninstallWrite(dir)
			_, err = kfs.RemoveEmptyDirectoryIfExists(anchor, dir)
		}
		if err != nil {
			failed = joinFailure(failed, dir, err)
		}
	}
	return failed
}

// joinFailure adds the failure of one path to the failures so far.
func joinFailure(failed error, path string, err error) error {
	return errors.Join(failed, fmt.Errorf("%s: %w", path, err))
}
