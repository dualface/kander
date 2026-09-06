package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

var gitOverrideVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"}

func lexicalAbsolute(path string) (string, error) {
	expanded := expandUser(path)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", configErrorfWrap(err,
			"config.invalid_install_path", path,
		)
	}
	return abs, nil
}

func sameDirName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func unsafePathError(path string) *Error {
	return configErrorf(
		"config.path_component_must_not_be_a_symlink_reparse_point", path,
	)
}

func rejectLeafReparse(path string) error {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return configErrorfWrap(err, "config.failed_to_inspect_path", path)
	}
	if fs.IsReparsePoint(path) {
		return unsafePathError(path)
	}
	return nil
}

func ensureWindowsPathNofollowSafe(path string) (string, error) {
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return "", err
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return "", configErrorf("config.invalid_install_path", path)
	}
	current := vol
	if !strings.HasSuffix(current, string(os.PathSeparator)) {
		current += string(os.PathSeparator)
	}
	rest := strings.TrimPrefix(abs, current)
	if rest == abs {
		rest = strings.TrimPrefix(abs, vol)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	if rest == "" {
		return abs, nil
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if _, err := os.Lstat(current); err != nil {
			if os.IsNotExist(err) {
				break
			}
			return "", configErrorfWrap(err, "config.failed_to_inspect_path", current)
		}
		if fs.IsReparsePoint(current) {
			return "", unsafePathError(current)
		}
	}
	return abs, nil
}

func gitSubprocessEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	blocked := map[string]struct{}{}
	for _, name := range gitOverrideVars {
		blocked[name] = struct{}{}
		if runtime.GOOS == "windows" {
			blocked[strings.ToUpper(name)] = struct{}{}
		}
	}
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if _, ok := blocked[name]; ok {
			continue
		}
		if runtime.GOOS == "windows" {
			if _, ok := blocked[strings.ToUpper(name)]; ok {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func gitMainWorktree(directory string) (string, error) {
	target, err := lexicalAbsolute(directory)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return "", nil
	}
	cmd := exec.Command("git", "-C", target, "worktree", "list", "--porcelain")
	cmd.Env = gitSubprocessEnv()
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return lexicalAbsolute(strings.TrimPrefix(line, "worktree "))
		}
	}
	return "", nil
}

func globalInstallPaths() (InstallPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return InstallPaths{}, configErrorfWrap(err, "config.cannot_resolve_home_directory")
	}
	return InstallPaths{
		Mode:       ModeGlobal,
		ConfigPath: filepath.Join(home, ".config", "kander", "config.json"),
		RulesDir:   filepath.Join(home, ".agents"),
		BinDir:     filepath.Join(home, ".local", "bin"),
		ShareDir:   filepath.Join(home, ".local", "share", "kander"),
	}, nil
}

func projectInstallLayout(projectRoot string) InstallPaths {
	installRoot := filepath.Join(projectRoot, ProjectInstallDirname)
	return InstallPaths{
		Mode:        ModeProject,
		ConfigPath:  filepath.Join(installRoot, "config.json"),
		RulesDir:    filepath.Join(installRoot, "rules"),
		BinDir:      filepath.Join(installRoot, "bin"),
		ShareDir:    filepath.Join(installRoot, "share"),
		ProjectRoot: projectRoot,
		InstallRoot: installRoot,
	}
}

func currentEntry() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", configErrorfWrap(err, "config.cannot_resolve_current_entry")
	}
	return lexicalAbsolute(exe)
}

// InstallPathsFromEntry resolves global or project install paths from the entry point.
// Project mode is entered only when the entry lives in a directory named .kander/bin/, so a source tree's cmd/ is never mistaken for one.
func InstallPathsFromEntry(entry string) (InstallPaths, error) {
	source, err := lexicalAbsolute(entry)
	if err != nil {
		return InstallPaths{}, err
	}
	binDir := filepath.Dir(source)
	installRoot := filepath.Dir(binDir)
	if !sameDirName(filepath.Base(binDir), "bin") || !sameDirName(filepath.Base(installRoot), ProjectInstallDirname) {
		return globalInstallPaths()
	}
	if runtime.GOOS == "windows" {
		if _, err := ensureWindowsPathNofollowSafe(binDir); err != nil {
			return InstallPaths{}, err
		}
	} else {
		if err := rejectLeafReparse(installRoot); err != nil {
			return InstallPaths{}, err
		}
		if err := rejectLeafReparse(binDir); err != nil {
			return InstallPaths{}, err
		}
	}
	parent := filepath.Dir(installRoot)
	main, err := gitMainWorktree(parent)
	if err != nil {
		return InstallPaths{}, err
	}
	if main == "" {
		return InstallPaths{}, configErrorf(
			"config.project_is_not_a_git_repository", parent,
		)
	}
	paths := projectInstallLayout(main)
	if runtime.GOOS == "windows" {
		if _, err := ensureWindowsPathNofollowSafe(paths.BinDir); err != nil {
			return InstallPaths{}, err
		}
	} else {
		if paths.InstallRoot != "" {
			if err := rejectLeafReparse(paths.InstallRoot); err != nil {
				return InstallPaths{}, err
			}
		}
		if err := rejectLeafReparse(paths.BinDir); err != nil {
			return InstallPaths{}, err
		}
	}
	return paths, nil
}

// CurrentInstallPaths resolves the scope from the current executable entry point.
func CurrentInstallPaths() (InstallPaths, error) {
	entry, err := currentEntry()
	if err != nil {
		return InstallPaths{}, err
	}
	return InstallPathsFromEntry(entry)
}

// ProjectInstallPaths normalizes a user-supplied project directory to the project install paths under the Git main worktree.
func ProjectInstallPaths(project string) (InstallPaths, error) {
	candidate, err := lexicalAbsolute(project)
	if err != nil {
		return InstallPaths{}, err
	}
	if runtime.GOOS == "windows" {
		if _, err := ensureWindowsPathNofollowSafe(candidate); err != nil {
			return InstallPaths{}, err
		}
	} else if err := rejectLeafReparse(candidate); err != nil {
		return InstallPaths{}, err
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return InstallPaths{}, configErrorf(
			"config.project_directory_does_not_exist", candidate,
		)
	}
	main, err := gitMainWorktree(candidate)
	if err != nil {
		return InstallPaths{}, err
	}
	if main == "" {
		return InstallPaths{}, configErrorf(
			"config.project_is_not_a_git_repository", candidate,
		)
	}
	if runtime.GOOS == "windows" {
		if _, err := ensureWindowsPathNofollowSafe(main); err != nil {
			return InstallPaths{}, err
		}
	}
	return projectInstallLayout(main), nil
}

func gitExcludePath(gitRoot string) (string, error) {
	cmd := exec.Command("git", "-C", gitRoot, "rev-parse", "--git-path", "info/exclude")
	cmd.Env = gitSubprocessEnv()
	output, err := cmd.Output()
	if err != nil {
		return "", configErrorfWrap(err, "board.cannot_locate_git_info_exclude")
	}
	exclude := strings.TrimSpace(string(output))
	if exclude == "" {
		return "", configErrorf("board.cannot_locate_git_info_exclude")
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(gitRoot, exclude)
	}
	return lexicalAbsolute(exclude)
}

func volumeAnchor(abs string) (string, error) {
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(abs)
		if vol == "" {
			return "", configErrorf("config.invalid_install_path", abs)
		}
		return vol + `\`, nil
	}
	return string(os.PathSeparator), nil
}

func appendGitExcludePattern(gitRoot, pattern string) (string, error) {
	exclude, err := gitExcludePath(gitRoot)
	if err != nil {
		return "", err
	}
	var openRoot string
	if runtime.GOOS == "windows" {
		if _, err := ensureWindowsPathNofollowSafe(filepath.Dir(exclude)); err != nil {
			return "", err
		}
		if err := rejectLeafReparse(exclude); err != nil {
			return "", err
		}
		if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(exclude)); err != nil {
			return "", configErrorfWrap(err,
				"board.failed_to_update_git_exclude", exclude, err.Error(),
			)
		}
		openRoot, err = volumeAnchor(exclude)
		if err != nil {
			return "", err
		}
	} else {
		if err := rejectLeafReparse(gitRoot); err != nil {
			return "", err
		}
		rel, err := filepath.Rel(gitRoot, filepath.Dir(exclude))
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", configErrorf(
				"config.git_exclude_is_outside_the_repository", exclude,
			)
		}
		current := gitRoot
		if rel != "." {
			for _, part := range strings.Split(rel, string(os.PathSeparator)) {
				if part == "" || part == "." {
					continue
				}
				current = filepath.Join(current, part)
				if err := rejectLeafReparse(current); err != nil {
					return "", err
				}
			}
		}
		if err := rejectLeafReparse(exclude); err != nil {
			return "", err
		}
		openRoot = gitRoot
	}
	if err := fs.AppendUniqueLine(openRoot, exclude, pattern); err != nil {
		return "", configErrorfWrap(err,
			"board.failed_to_update_git_exclude", exclude, err.Error(),
		)
	}
	return exclude, nil
}

// EnsureProjectGitExclude idempotently writes /.kander/ into the repository-local Git exclude while preserving existing permissions.
func EnsureProjectGitExclude(project string) (string, error) {
	paths, err := ProjectInstallPaths(project)
	if err != nil {
		return "", err
	}
	if paths.ProjectRoot == "" {
		return "", configErrorf(
			"config.project_install_paths_are_missing_the_main_worktree",
		)
	}
	return appendGitExcludePattern(paths.ProjectRoot, ProjectGitExcludePattern)
}

// ConfigPath returns the config file path in force; KANDER_CONFIG overrides the scope default.
func ConfigPath() (string, error) {
	if override := os.Getenv(EnvConfig); override != "" {
		return expandUser(override), nil
	}
	paths, err := CurrentInstallPaths()
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}
