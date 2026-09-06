package board

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

var installPathsFn = config.CurrentInstallPaths

// notFoundError means no board can be located in the current environment (including an unusable cwd); it lets callers tell this apart from other location errors.
type notFoundError struct{ inner *Error }

func (e *notFoundError) Error() string { return e.inner.Error() }

func boardNotFound() error {
	return &notFoundError{inner: kanbanError("board.board_directory_not_found_run_inside_a_project_or")}
}

// IsBoardNotFound reports whether the error means "no board could be located".
func IsBoardNotFound(err error) bool {
	var nf *notFoundError
	return errors.As(err, &nf)
}

func expandUser(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(path, `~\`)) {
		return filepath.Join(home, path[2:])
	}
	return path
}

func absoluteUserPath(path string) (string, error) {
	expanded := expandUser(path)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", kanbanError("board.invalid_board_path", path)
	}
	return abs, nil
}

func ensureWindowsLexicalPathSafe(path string) error {
	if !isWindows() {
		return nil
	}
	absolute, err := absoluteUserPath(path)
	if err != nil {
		return err
	}
	vol := filepath.VolumeName(absolute)
	if vol == "" {
		return kanbanError("board.invalid_board_path", path)
	}
	current := vol
	if !strings.HasSuffix(current, string(os.PathSeparator)) {
		current += string(os.PathSeparator)
	}
	rest := strings.TrimPrefix(absolute, current)
	if rest == absolute {
		rest = strings.TrimPrefix(absolute, vol)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	candidates := []string{strings.TrimRight(current, `/\`)}
	if rest != "" {
		for _, part := range strings.Split(rest, string(os.PathSeparator)) {
			if part == "" {
				continue
			}
			current = filepath.Join(current, part)
			candidates = append(candidates, current)
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Lstat(candidate); err != nil {
			if os.IsNotExist(err) {
				break
			}
			return kanbanError("board.invalid_board_path", candidate)
		}
		if fs.IsReparsePoint(candidate) {
			return kanbanError(
				"board.board_path_component_must_not_be_a_symlink_reparse", candidate,
			)
		}
	}
	return nil
}

func gitMainWorktree(directory string) string {
	abs, err := absoluteUserPath(directory)
	if err != nil {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return ""
	}
	cmd := exec.Command("git", "-C", abs, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			resolved, err := absoluteUserPath(strings.TrimPrefix(line, "worktree "))
			if err != nil {
				return ""
			}
			return resolved
		}
	}
	return ""
}

func isDirNoFollow(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if fs.IsReparsePoint(path) {
		return false
	}
	return info.IsDir()
}

func existsNoFollow(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func boardRootReparseError(path string) error {
	return kanbanError(
		"board.board_directory_does_not_exist_or_is_a_symlink", path,
	)
}

// inspectBoardCandidate checks one locate candidate: a missing candidate continues the search; a reparse point fails closed immediately;
// an existing regular directory hits; anything else that exists fails without walking further up.
func inspectBoardCandidate(candidate string) (abs string, hit bool, err error) {
	if err := ensureWindowsLexicalPathSafe(candidate); err != nil {
		return "", false, err
	}
	if !existsNoFollow(candidate) {
		return "", false, nil
	}
	if fs.IsReparsePoint(candidate) {
		return "", false, boardRootReparseError(candidate)
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", false, kanbanError("board.invalid_board_path", candidate)
	}
	if !info.IsDir() {
		return "", false, kanbanError("board.board_path_is_not_a_directory", candidate)
	}
	abs, err = absoluteUserPath(candidate)
	if err != nil {
		return "", false, err
	}
	return abs, true, nil
}

func isFileNoFollow(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// BoardRoot locates the board in the order KANBAN_DIR -> the main worktree's kanban/ -> an upward search.
func BoardRoot() (string, error) {
	if configured := os.Getenv(EnvBoardDir); configured != "" {
		root, err := absoluteUserPath(configured)
		if err != nil {
			return "", err
		}
		if err := ensureWindowsLexicalPathSafe(root); err != nil {
			return "", err
		}
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", boardNotFound()
	}
	if main := gitMainWorktree(cwd); main != "" {
		abs, hit, err := inspectBoardCandidate(filepath.Join(main, "kanban"))
		if err != nil {
			return "", err
		}
		if hit {
			return abs, nil
		}
	}
	current, err := absoluteUserPath(cwd)
	if err != nil {
		return "", err
	}
	if err := ensureWindowsLexicalPathSafe(current); err != nil {
		return "", err
	}
	for {
		abs, hit, err := inspectBoardCandidate(filepath.Join(current, "kanban"))
		if err != nil {
			return "", err
		}
		if hit {
			return abs, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", boardNotFound()
}

func rulesPath() (string, error) {
	paths, err := installPathsFn()
	if err != nil {
		return "", &Error{Message: err.Error()}
	}
	return filepath.Join(paths.RulesDir, "KANDER-KANBAN-RULES.md"), nil
}

func addGitExclude(root string) (string, error) {
	parent := filepath.Dir(root)
	cmd := exec.Command("git", "-C", parent, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	gitRoot, err := absoluteUserPath(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	boardAbs, err := absoluteUserPath(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(gitRoot, boardAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", nil
	}
	pattern := "/" + strings.Trim(filepath.ToSlash(rel), "/") + "/"
	excludeCmd := exec.Command("git", "-C", gitRoot, "rev-parse", "--git-path", "info/exclude")
	excludeOut, err := excludeCmd.Output()
	if err != nil {
		return "", kanbanError("board.cannot_locate_git_info_exclude")
	}
	exclude := strings.TrimSpace(string(excludeOut))
	if exclude == "" {
		return "", kanbanError("board.cannot_locate_git_info_exclude")
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(gitRoot, exclude)
	}
	exclude, err = absoluteUserPath(exclude)
	if err != nil {
		return "", err
	}
	if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(exclude)); err != nil {
		return "", wrapFS(err, "board.cannot_locate_git_info_exclude")
	}
	openRoot := gitRoot
	if isWindows() {
		anchor, err := volumeAnchor(exclude)
		if err != nil {
			return "", err
		}
		openRoot = anchor
	}
	if err := fs.AppendUniqueLine(openRoot, exclude, pattern); err != nil {
		return "", wrapFS(err,
			"board.failed_to_update_git_exclude", exclude, err.Error(),
		)
	}
	return exclude, nil
}

func ensurePrivateBoardDirectory(path string) error {
	parent := filepath.Dir(path)
	if parent == path {
		return kanbanError("board.board_directory_cannot_be_a_filesystem_root", path)
	}
	if err := fs.EnsureInheritedDirectoryPath(parent); err != nil {
		return wrapFS(err, "board.board_path_is_not_a_directory", path)
	}
	_, err := fs.DirectoryIdentity(parent, path)
	if isNotExist(err) {
		if err := fs.CreatePrivateDirectory(parent, path); err != nil {
			return wrapFS(err, "board.board_path_is_not_a_directory", path)
		}
		return nil
	}
	if err != nil {
		return wrapFS(err, "board.board_path_is_not_a_directory", path)
	}
	if err := fs.EnsurePrivateDirectory(parent, path, false); err != nil {
		return wrapFS(err, "board.board_path_is_not_a_directory", path)
	}
	return nil
}

func createStateDirectory(path string) error {
	if err := ensureWindowsLexicalPathSafe(path); err != nil {
		return err
	}
	if existsNoFollow(path) && !isDirNoFollow(path) {
		return kanbanError("board.state_path_is_not_a_directory", path)
	}
	if isWindows() {
		return ensurePrivateBoardDirectory(path)
	}
	if err := fs.EnsureInheritedDirectoryPath(path); err != nil {
		return wrapFS(err, "board.state_path_is_not_a_directory", path)
	}
	if !isDirNoFollow(path) {
		return kanbanError("board.state_path_is_not_a_directory", path)
	}
	return nil
}

func InitBoard(project string) (root string, exclude string, rules string, err error) {
	configured := os.Getenv(EnvBoardDir)
	if project != "" && configured != "" {
		return "", "", "", kanbanError("board.project_path_and_kanban_dir_cannot_be_used_together")
	}
	if project != "" {
		proj, e := absoluteUserPath(project)
		if e != nil {
			return "", "", "", e
		}
		if e := ensureWindowsLexicalPathSafe(proj); e != nil {
			return "", "", "", e
		}
		info, e := os.Stat(proj)
		if e != nil || !info.IsDir() {
			return "", "", "", kanbanError("board.project_directory_does_not_exist", proj)
		}
		root = filepath.Join(proj, "kanban")
	} else if configured != "" {
		root, err = absoluteUserPath(configured)
		if err != nil {
			return "", "", "", err
		}
	} else {
		cwd, e := os.Getwd()
		if e != nil {
			return "", "", "", kanbanError("board.project_directory_does_not_exist", cwd)
		}
		proj := gitMainWorktree(cwd)
		if proj == "" {
			proj, err = absoluteUserPath(cwd)
			if err != nil {
				return "", "", "", err
			}
		}
		root = filepath.Join(proj, "kanban")
	}
	if err := ensureWindowsLexicalPathSafe(root); err != nil {
		return "", "", "", err
	}
	if existsNoFollow(root) && !isDirNoFollow(root) {
		return "", "", "", kanbanError("board.board_path_is_not_a_directory", root)
	}
	if isWindows() {
		if err := ensurePrivateBoardDirectory(root); err != nil {
			return "", "", "", err
		}
	} else {
		if err := fs.EnsureInheritedDirectoryPath(root); err != nil {
			return "", "", "", wrapFS(err, "board.board_path_is_not_a_directory", root)
		}
	}
	for _, state := range States {
		if err := createStateDirectory(filepath.Join(root, state)); err != nil {
			return "", "", "", err
		}
	}
	exclude, err = addGitExclude(root)
	if err != nil {
		return "", "", "", err
	}
	rules, err = rulesPath()
	if err != nil {
		return "", "", "", err
	}
	return root, exclude, rules, nil
}

func ensureLayout(root string) error {
	if err := ensureWindowsLexicalPathSafe(root); err != nil {
		return err
	}
	if !isDirNoFollow(root) || fs.IsReparsePoint(root) {
		return boardRootReparseError(root)
	}
	for _, state := range States {
		path := filepath.Join(root, state)
		if fs.IsReparsePoint(path) {
			return kanbanError("board.state_directory_must_not_be_a_symlink_reparse_point", path)
		}
		if isDirNoFollow(path) {
			continue
		}
		if existsNoFollow(path) {
			return kanbanError("board.state_path_is_not_a_directory", path)
		}
		if state == "review" {
			ok := true
			for _, legacy := range legacyStates {
				legacyPath := filepath.Join(root, legacy)
				if !isDirNoFollow(legacyPath) || fs.IsReparsePoint(legacyPath) {
					ok = false
					break
				}
			}
			if ok {
				if err := createStateDirectory(path); err != nil {
					return err
				}
				continue
			}
		}
		return kanbanError("board.state_directory_does_not_exist", path)
	}
	return nil
}
