package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/v2/internal/fs"
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
		ConfigPath: filepath.Join(home, ".config", "kander", "config.json"),
		RulesDir:   filepath.Join(home, ".agents", "kander"),
		BinDir:     filepath.Join(home, ".local", "bin"),
		ShareDir:   filepath.Join(home, ".local", "share", "kander"),
	}, nil
}

// GlobalInstallPaths returns the HOME-scoped install layout.
func GlobalInstallPaths() (InstallPaths, error) {
	return globalInstallPaths()
}

// CurrentInstallPaths returns the install paths in force. Kander has one install scope, the
// global one, whatever directory the running binary lives in.
func CurrentInstallPaths() (InstallPaths, error) {
	return globalInstallPaths()
}

// LegacyProjectInstallRoot reports the retired project install directory the running binary
// lives in: an entry inside a directory named .kander/bin/ (the layout earlier releases wrote
// into a Git main worktree). It only names the directory for the migration notice; the binary
// still runs with the global paths.
func LegacyProjectInstallRoot() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	return LegacyProjectInstallRootOf(exe)
}

// LegacyProjectInstallRootOf is LegacyProjectInstallRoot for the given entry path.
func LegacyProjectInstallRootOf(entry string) (string, bool) {
	source, err := lexicalAbsolute(entry)
	if err != nil {
		return "", false
	}
	binDir := filepath.Dir(source)
	installRoot := filepath.Dir(binDir)
	if !sameDirName(filepath.Base(binDir), "bin") || !sameDirName(filepath.Base(installRoot), LegacyProjectInstallDirname) {
		return "", false
	}
	return installRoot, true
}

// MainWorktree returns the Git main worktree of directory, or "" when directory is not inside
// a Git checkout. Git override variables of the environment are ignored.
func MainWorktree(directory string) (string, error) {
	return gitMainWorktree(directory)
}

// GitInfoExcludePath returns the repository-local exclude file of the checkout at gitRoot.
func GitInfoExcludePath(gitRoot string) (string, error) {
	return gitExcludePath(gitRoot)
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

// ConfigPath returns the config file path in force; KANDER_CONFIG overrides the scope default.
func ConfigPath() (string, error) {
	if override := os.Getenv(EnvConfig); override != "" {
		return expandUser(override), nil
	}
	paths, err := globalInstallPaths()
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}
