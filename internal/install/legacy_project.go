package install

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/rules"
)

// binaryProbeTimeout bounds the `version` call that confirms a leftover binary is kander.
const binaryProbeTimeout = 10 * time.Second

// LegacyProjectInstall is what a retired project install left in one main worktree, planned
// before anything changes: the .kander/ payload, the project-level leader skill and pi
// extension, and the info/exclude line. Items carry the planned action of every path; Dirs
// lists the directories to remove once empty, deepest first.
type LegacyProjectInstall struct {
	Root   string
	Items  []UninstallItem
	Dirs   []string
	Config *LegacyProjectConfig
}

// Found reports whether the checkout has anything of a project install.
func (l LegacyProjectInstall) Found() bool { return len(l.Items) > 0 || l.Config != nil }

// LegacyProjectConfig is the migration plan of .kander/config.json into the project overlay.
type LegacyProjectConfig struct {
	Path    string
	Overlay string
	// Migrate lists the top-level keys the overlay allows and does not set yet.
	Migrate []string
	// Conflicts lists the keys the overlay already sets; its value stays.
	Conflicts []string
	// Retained lists the settings the overlay does not allow and whose value differs from the
	// default, so the file stays for the user to decide.
	Retained []string
	// Err is a file that cannot be read as a configuration object; nothing is migrated.
	Err    error
	values map[string]any
}

// Removable reports whether the file goes once the migration is written.
func (c LegacyProjectConfig) Removable() bool {
	return c.Err == nil && len(c.Conflicts) == 0 && len(c.Retained) == 0
}

// legacyBinaryCheck marks a planned binary removal that still needs the version check.
const legacyBinaryCheck = "binary"

// InspectLegacyProjectInstall plans the cleanup of a retired project install in the main
// worktree root without writing. Nothing below a symlink or another reparse point is read.
func InspectLegacyProjectInstall(root string) LegacyProjectInstall {
	plan := LegacyProjectInstall{Root: root}
	install := filepath.Join(root, config.LegacyProjectInstallDirname)
	info, err := os.Lstat(install)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		plan.keep(install, err.Error())
	case !info.IsDir() || kfs.IsReparsePoint(install):
		plan.keep(install, config.Text("install.target_is_symlink", install))
	default:
		plan.planBinaries(filepath.Join(install, "bin"))
		plan.planRules(filepath.Join(install, "rules"))
		plan.planShare(filepath.Join(install, "share"))
		plan.planConfig(filepath.Join(install, "config.json"))
		plan.planOther(install)
		plan.Dirs = append(plan.Dirs, install)
	}
	legacyScope := config.InstallPaths{RulesDir: filepath.Join(install, "rules")}
	for _, dir := range []string{filepath.Join(root, ".agents", "skills", leaderName), filepath.Join(root, ".claude", "skills", leaderName)} {
		items, dirs := planLeaderRemoval(dir, legacyScope)
		plan.Items = append(plan.Items, items...)
		plan.Dirs = append(plan.Dirs, dirs...)
		if len(dirs) > 0 {
			// skills/ and its parent go too once nothing else is left in them.
			plan.Dirs = append(plan.Dirs, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
		}
	}
	plan.planExtension(root)
	plan.planExclude(root)
	slices.SortStableFunc(plan.Dirs, func(a, b string) int {
		return cmp.Compare(strings.Count(b, string(os.PathSeparator)), strings.Count(a, string(os.PathSeparator)))
	})
	return plan
}

func (l *LegacyProjectInstall) keep(path, reason string) {
	l.Items = append(l.Items, UninstallItem{Action: UninstallKeep, Path: path, Reason: reason})
}

func (l *LegacyProjectInstall) remove(path string) {
	l.Items = append(l.Items, UninstallItem{Action: UninstallRemove, Path: path})
}

// entries lists a directory of the payload; a missing one is empty, and one that is a link is
// kept and not read.
func (l *LegacyProjectInstall) entries(dir string) []os.DirEntry {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		l.keep(dir, err.Error())
		return nil
	}
	if !info.IsDir() || kfs.IsReparsePoint(dir) {
		l.keep(dir, config.Text("install.target_is_symlink", dir))
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		l.keep(dir, err.Error())
	}
	return entries
}

// planBinaries plans the kander binary of .kander/bin/; the repair runs it with `version`
// before removing it, and keeps it unless the output starts with "kander ".
func (l *LegacyProjectInstall) planBinaries(dir string) {
	entries := l.entries(dir)
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.Name() == binaryName() && entry.Type().IsRegular() {
			l.Items = append(l.Items, UninstallItem{Action: UninstallRemove, Path: path, Reason: legacyBinaryCheck})
			continue
		}
		l.keep(path, config.Text("install.uninstall_not_kander_file"))
	}
	if entries != nil {
		l.Dirs = append(l.Dirs, dir)
	}
}

// planRules plans the rule files whose digest is a released one and Kander's state files of
// .kander/rules/; every other file stays.
func (l *LegacyProjectInstall) planRules(dir string) {
	if l.entries(dir) == nil {
		return
	}
	state := map[string]struct{}{stateFileName: {}, startupStateFileName: {}}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			l.keep(path, err.Error())
			return nil
		}
		if path == dir {
			l.Dirs = append(l.Dirs, dir)
			return nil
		}
		if entry.IsDir() {
			l.Dirs = append(l.Dirs, path)
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if _, ok := state[name]; ok && entry.Type().IsRegular() {
			l.remove(path)
			return nil
		}
		if entry.Type().IsRegular() && releasedRule(name, path) {
			l.remove(path)
			return nil
		}
		l.keep(path, config.Text("install.uninstall_not_kander_file"))
		return nil
	})
	if err != nil {
		l.keep(dir, err.Error())
	}
}

// releasedRule reports whether the file at path holds a released version of the rule name.
func releasedRule(name, path string) bool {
	want, err := rules.Hash(name)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	digest := fileHash(data)
	return digest == want || isPreviousOfficial(name, digest)
}

// planShare keeps every terminal definition of .kander/share/: Kander no longer reads it, and
// the definitions belong in the global share.
func (l *LegacyProjectInstall) planShare(dir string) {
	if l.entries(dir) == nil {
		return
	}
	global := ""
	if paths, err := config.GlobalInstallPaths(); err == nil {
		global = paths.ShareDir
	}
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && path != dir && !entry.IsDir() {
			l.keep(path, config.Text("projectrules.legacy_share_kept", global))
		}
		return nil
	})
}

// planOther keeps every other entry of .kander/.
func (l *LegacyProjectInstall) planOther(install string) {
	for _, entry := range l.entries(install) {
		switch entry.Name() {
		case "bin", "rules", "share", "config.json":
			continue
		}
		l.keep(filepath.Join(install, entry.Name()), config.Text("install.uninstall_not_kander_file"))
	}
}

// planExtension plans the project-level pi extension when its digest is a released version.
func (l *LegacyProjectInstall) planExtension(root string) {
	spec, embedded, ok := config.AgentExtension("pi")
	if !ok || spec.Project == "" {
		return
	}
	target := filepath.Join(root, filepath.FromSlash(spec.Project))
	if err := rejectReparseChain(root, target); err != nil {
		l.keep(target, err.Error())
		return
	}
	data, found, err := kfs.ReadRegularFileIfExists(root, target)
	switch {
	case err != nil:
		l.keep(target, err.Error())
	case !found:
	case classifyExtensionContent(data, embedded) == ExtensionModified:
		l.keep(target, config.Text("install.uninstall_extension_modified"))
	default:
		l.remove(target)
		l.Dirs = append(l.Dirs, filepath.Dir(target), filepath.Dir(filepath.Dir(target)))
	}
}

// planExclude plans dropping the /.kander/ line a project install appended to info/exclude.
func (l *LegacyProjectInstall) planExclude(root string) {
	exclude, err := config.GitInfoExcludePath(root)
	if err != nil {
		return
	}
	data, err := os.ReadFile(exclude)
	if err != nil {
		return
	}
	lines := strings.SplitAfter(string(data), "\n")
	kept := lines[:0:0]
	for _, line := range lines {
		if strings.TrimRight(line, "\r\n") == config.LegacyProjectGitExcludePattern {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == len(lines) {
		return
	}
	l.Items = append(l.Items, UninstallItem{Action: UninstallEdit, Path: exclude, content: []byte(strings.Join(kept, ""))})
}

// overlayScopeOnly lists the settings only the scope configuration may hold: the bookkeeping
// keys and the rules injection switch at the top level, and the per-agent switch and config
// directory below agents.
var overlayScopeOnly = []string{"schema_version", "welcome_complete", "install_rules"}

// planConfig plans the migration of .kander/config.json into the project overlay.
func (l *LegacyProjectInstall) planConfig(path string) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return
	}
	plan := &LegacyProjectConfig{Path: path, Overlay: filepath.Join(l.Root, config.OverlayFilename)}
	l.Config = plan
	data, found, err := kfs.ReadRegularFileIfExists(l.Root, path)
	if err == nil && !found {
		err = errors.New(config.Text("menu.not_found", path))
	}
	var values map[string]any
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		err = decoder.Decode(&values)
	}
	overlay := map[string]any{}
	if err == nil {
		overlay, err = config.ReadOverlayFile(plan.Overlay)
	}
	if err != nil {
		plan.Err = err
		return
	}
	defaults := defaultConfigDocument()
	plan.values = map[string]any{}
	for key, value := range values {
		switch {
		case slices.Contains(overlayScopeOnly, key):
			if key != "schema_version" && key != "welcome_complete" && !jsonEqual(value, defaults[key]) {
				plan.Retained = append(plan.Retained, key)
			}
			continue
		case key == "agents":
			value = plan.stripAgentScopeKeys(value)
		}
		if _, ok := overlay[key]; ok {
			plan.Conflicts = append(plan.Conflicts, key)
			continue
		}
		plan.Migrate = append(plan.Migrate, key)
		plan.values[key] = value
	}
	slices.Sort(plan.Migrate)
	slices.Sort(plan.Conflicts)
	slices.Sort(plan.Retained)
}

// stripAgentScopeKeys drops agents.<name>.install_rules and agents.<name>.config_dir, which the
// overlay does not apply; a value that is set is retained for the user to decide.
func (c *LegacyProjectConfig) stripAgentScopeKeys(value any) any {
	agents, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := map[string]any{}
	for name, raw := range agents {
		entry, ok := raw.(map[string]any)
		if !ok {
			out[name] = raw
			continue
		}
		copied := map[string]any{}
		for key, field := range entry {
			switch {
			case key == "install_rules":
				// Only a switched-off agent differs from the default.
				if field != true {
					c.Retained = append(c.Retained, "agents."+name+"."+key)
				}
			case key == "config_dir":
				if field != "" {
					c.Retained = append(c.Retained, "agents."+name+"."+key)
				}
			default:
				copied[key] = field
			}
		}
		if len(copied) > 0 {
			out[name] = copied
		}
	}
	return out
}

func defaultConfigDocument() map[string]any {
	data, err := json.Marshal(config.DefaultConfig())
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

func jsonEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

// LegacyProjectRepair is what RepairLegacyProjectInstall changed.
type LegacyProjectRepair struct {
	// Removed lists the files removed or edited.
	Removed []string
	// Kept lists the binaries the version check did not confirm, with the reason.
	Kept []UninstallItem
	// Migrated lists the keys written into the project overlay.
	Migrated []string
	// ConfigRemoved reports that .kander/config.json was removed after its migration.
	ConfigRemoved bool
}

// RepairLegacyProjectInstall applies the plan: it confirms each binary with `version`,
// migrates the configuration, removes and edits the planned files without following links,
// and removes the directories left empty.
func RepairLegacyProjectInstall(plan LegacyProjectInstall) (LegacyProjectRepair, error) {
	var out LegacyProjectRepair
	var failed error
	items := make([]UninstallItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		if item.Action == UninstallRemove && item.Reason == legacyBinaryCheck {
			if reason := confirmKanderBinary(item.Path); reason != "" {
				out.Kept = append(out.Kept, UninstallItem{Action: UninstallKeep, Path: item.Path, Reason: reason})
				continue
			}
			item.Reason = ""
		}
		items = append(items, item)
	}
	if plan.Config != nil && plan.Config.Err == nil {
		migrated, removed, err := migrateLegacyConfig(*plan.Config)
		out.Migrated, out.ConfigRemoved = migrated, removed
		failed = errors.Join(failed, err)
	}
	_, changed, err := applyUninstallItems(items, io.Discard)
	out.Removed = changed
	failed = errors.Join(failed, err)
	for _, dir := range plan.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			continue
		}
		anchor, err := fileAnchor(dir)
		if err == nil {
			_, err = kfs.RemoveEmptyDirectoryIfExists(anchor, dir)
		}
		failed = errors.Join(failed, err)
	}
	return out, failed
}

// confirmKanderBinary runs path with `version` and returns "" when the output starts with
// "kander ", otherwise the reason to keep it.
func confirmKanderBinary(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), binaryProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil || !strings.HasPrefix(string(out), "kander ") {
		return config.Text("projectrules.legacy_binary_unconfirmed")
	}
	return ""
}

// migrateLegacyConfig writes the migrated keys into the project overlay, refusing a concurrent
// overlay change, and removes .kander/config.json when nothing stays in it.
func migrateLegacyConfig(plan LegacyProjectConfig) ([]string, bool, error) {
	if len(plan.Migrate) > 0 {
		baseline, err := config.ReadOverlayFile(plan.Overlay)
		if err != nil {
			return nil, false, err
		}
		overlay := config.CloneOverlay(baseline)
		for _, key := range plan.Migrate {
			overlay[key] = plan.values[key]
		}
		if _, err := config.SaveOverlayIfUnchanged(plan.Overlay, overlay, baseline); err != nil {
			return nil, false, fmt.Errorf("%s: %w", plan.Overlay, err)
		}
	}
	if !plan.Removable() {
		return plan.Migrate, false, nil
	}
	anchor, err := fileAnchor(plan.Path)
	if err == nil {
		_, err = kfs.RemoveNonDirectoryIfExists(anchor, plan.Path)
	}
	return plan.Migrate, err == nil, err
}
