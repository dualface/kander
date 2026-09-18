package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// ExtensionStatus reports what InspectExtension or EnsureExtension found at one
// agent extension target.
type ExtensionStatus int

const (
	// ExtensionInstalled means the file exists and matches the embedded payload.
	ExtensionInstalled ExtensionStatus = iota
	// ExtensionMissing means no file exists at the target.
	ExtensionMissing
	// ExtensionOutdated means the file matches a previous released payload.
	ExtensionOutdated
	// ExtensionModified means the file matches no known payload: a local edit.
	ExtensionModified
	// ExtensionWritten means EnsureExtension installed a missing file.
	ExtensionWritten
	// ExtensionUpdated means EnsureExtension rewrote an outdated file.
	ExtensionUpdated
)

// ExtensionOutcome records the inspection or write result of one agent extension.
type ExtensionOutcome struct {
	Agent  string
	Target string
	Status ExtensionStatus
}

// AgentExtension records ensuring one agent extension file during install.
type AgentExtension struct {
	ExtensionOutcome
	Err error
}

// extensionTarget resolves the scope's install target of one agent's rules
// extension: <home>/<spec.Global> for a global install, or
// <ProjectRoot>/<spec.Project> for a project install.
func extensionTarget(agent string, paths config.InstallPaths) string {
	spec, _, ok := config.AgentExtension(agent)
	if !ok {
		return ""
	}
	rel := spec.Global
	if paths.Mode == config.ModeProject {
		rel = spec.Project
		if paths.ProjectRoot == "" {
			return ""
		}
		return filepath.Join(paths.ProjectRoot, filepath.FromSlash(rel))
	}
	if strings.TrimSpace(rel) == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(rel))
}

// ExtensionAgents lists the agents whose extension the scope covers, mirroring
// the coverage rules of IntegrationAgents: a project install covers every
// embedded agent declaring a project target, while a global install covers
// only agents whose configuration directory already exists (the parent of the
// extension directory, e.g. ~/.pi/agent), so absent tools gain no files.
func ExtensionAgents(paths config.InstallPaths) []string {
	var out []string
	for _, agent := range config.ExecutionAgents {
		spec, _, ok := config.AgentExtension(agent)
		if !ok {
			continue
		}
		if paths.Mode == config.ModeProject {
			if spec.Project != "" && paths.ProjectRoot != "" {
				out = append(out, agent)
			}
			continue
		}
		if spec.Global == "" {
			continue
		}
		target := extensionTarget(agent, paths)
		if target == "" {
			continue
		}
		if info, err := os.Stat(filepath.Dir(filepath.Dir(target))); err == nil && info.IsDir() {
			out = append(out, agent)
		}
	}
	return out
}

// rejectExtensionReparse refuses to read or write through a symlink, junction,
// or other reparse point: every existing component between the base directory
// and the target file is checked. Unlike EnsureRulesIntegration, which follows
// user-managed symlinks in the global scope, an executable extension file is
// never written through a redirect.
func rejectExtensionReparse(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s", config.Text("install.rules_target_symlink_escapes_project", target))
	}
	current := base
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fs.IsReparsePoint(current) {
			return fmt.Errorf("%s", config.Text("install.target_is_symlink", current))
		}
		if !info.IsDir() && current != target {
			return fmt.Errorf("%s", config.Text("menu.cannot_read", current))
		}
	}
	return nil
}

// extensionBase is the trusted anchor the target must stay under: the home
// directory for a global install or the main worktree for a project install.
func extensionBase(paths config.InstallPaths) (string, error) {
	if paths.Mode == config.ModeProject {
		if paths.ProjectRoot == "" {
			return "", fmt.Errorf("%s", config.Text("config.project_install_paths_are_missing_the_main_worktree"))
		}
		return paths.ProjectRoot, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%s", config.Text("config.cannot_resolve_home_directory"))
	}
	return home, nil
}

// classifyExtensionContent maps file bytes to installed/outdated/modified.
func classifyExtensionContent(data, embedded []byte) ExtensionStatus {
	digest := fileHash(data)
	if digest == fileHash(embedded) {
		return ExtensionInstalled
	}
	if isPreviousExtensionVersion(digest) {
		return ExtensionOutdated
	}
	return ExtensionModified
}

// InspectExtension reports the state of one agent's extension file without
// writing: installed, missing, outdated, or locally modified.
func InspectExtension(agent string, paths config.InstallPaths) (ExtensionOutcome, error) {
	outcome := ExtensionOutcome{Agent: agent}
	_, embedded, ok := config.AgentExtension(agent)
	if !ok {
		return outcome, nil
	}
	target := extensionTarget(agent, paths)
	if target == "" {
		return outcome, nil
	}
	outcome.Target = target
	base, err := extensionBase(paths)
	if err != nil {
		return outcome, err
	}
	if err := rejectExtensionReparse(base, target); err != nil {
		return outcome, err
	}
	anchor, err := fileAnchor(target)
	if err != nil {
		return outcome, err
	}
	data, found, err := fs.ReadRegularFileIfExists(anchor, target)
	if err != nil {
		return outcome, err
	}
	if !found {
		outcome.Status = ExtensionMissing
		return outcome, nil
	}
	outcome.Status = classifyExtensionContent(data, embedded)
	return outcome, nil
}

// EnsureExtension installs or upgrades one agent's extension file: a missing
// file is written, a file matching a previous released version is rewritten,
// and a file matching no known version is treated as a local edit and left
// untouched. Reparse points anywhere on the target chain refuse the write.
func EnsureExtension(agent string, paths config.InstallPaths) (ExtensionOutcome, error) {
	outcome := ExtensionOutcome{Agent: agent}
	_, embedded, ok := config.AgentExtension(agent)
	if !ok {
		return outcome, nil
	}
	target := extensionTarget(agent, paths)
	if target == "" {
		return outcome, nil
	}
	outcome.Target = target
	base, err := extensionBase(paths)
	if err != nil {
		return outcome, err
	}
	if err := rejectExtensionReparse(base, target); err != nil {
		return outcome, err
	}
	anchor, err := fileAnchor(target)
	if err != nil {
		return outcome, err
	}
	data, found, err := fs.ReadRegularFileIfExists(anchor, target)
	if err != nil {
		return outcome, err
	}
	write := false
	if !found {
		write = true
		outcome.Status = ExtensionWritten
	} else {
		switch classifyExtensionContent(data, embedded) {
		case ExtensionInstalled:
			outcome.Status = ExtensionInstalled
			return outcome, nil
		case ExtensionOutdated:
			write = true
			outcome.Status = ExtensionUpdated
		default:
			outcome.Status = ExtensionModified
			return outcome, nil
		}
	}
	if !write {
		return outcome, nil
	}
	if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(target)); err != nil {
		return outcome, err
	}
	if err := fs.WriteBytesAtomicInherited(anchor, target, embedded, true); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// integrateAgentExtensions ensures the covered agents' extension files after an
// install, recording a per-agent error instead of failing the installation.
func integrateAgentExtensions(paths config.InstallPaths) []AgentExtension {
	var out []AgentExtension
	for _, agent := range ExtensionAgents(paths) {
		outcome, err := EnsureExtension(agent, paths)
		out = append(out, AgentExtension{ExtensionOutcome: outcome, Err: err})
	}
	return out
}
