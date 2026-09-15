package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

type copyConfirm func(check pathCheck, dest string) (bool, error)

var confirmCopy copyConfirm = runCopyYesNo

var (
	copyPromptIn  io.Reader = os.Stdin
	copyPromptOut io.Writer = os.Stderr
)

func copyDescription(check pathCheck, dest string) string {
	description := config.Text("install.copy_current", check.Current)
	if check.Err != nil {
		description += "\n" + config.Text("install.path_unknown", check.Err.Error())
	} else if check.Found == "" {
		description += "\n" + config.Text("install.path_missing")
	} else {
		description += "\n" + config.Text("install.path_conflict", check.Found)
	}
	return description + "\n" + config.Text("install.copy_destination", dest)
}

func parseCopyAnswer(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func runCopyYesNo(check pathCheck, dest string) (bool, error) {
	fmt.Fprintln(copyPromptOut, config.Text("install.copy_question"))
	fmt.Fprintln(copyPromptOut, copyDescription(check, dest))
	fmt.Fprint(copyPromptOut, config.Text("install.copy_yn"))
	reader := bufio.NewReader(copyPromptIn)
	line, err := reader.ReadString('\n')
	answer := strings.TrimSpace(line)
	if err != nil && answer == "" {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return parseCopyAnswer(line), nil
}

// offerCopy reports whether kander install should copy the running binary to
// the global entry. PATH identity match skips the copy; otherwise it copies
// without asking.
func offerCopy() bool {
	return !inspectPath("").Matches
}

func applyGlobalInstallDefaults(req *Request) {
	req.CopyBinary = offerCopy()
	req.DeleteLegacy = true
}

func warnPath(current string) {
	check := inspectPath(current)
	if check.Matches {
		return
	}
	fmt.Fprintln(os.Stderr, config.Text("install.path_adjust", filepath.Dir(current)))
	if check.Found != "" {
		fmt.Fprintln(os.Stderr, config.Text("install.path_conflict", check.Found))
	}
	if check.Err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.path_unknown", check.Err.Error()))
	}
}

// CheckStartupCopy offers a binary-only install before the board opens when a
// scope config already exists. handled means the caller must return code (after
// a handoff, cancellation, or error). Missing-config bare launches skip this
// prompt and open the board options panel after doctor; post-install startup
// also skips it so declining or copying cannot cause a prompt loop in one launch.
func CheckStartupCopy() (handled bool, code int) {
	if os.Getenv(EnvSkipInstall) != "" || requireInteractive() != nil || inSourceTree() {
		return false, 0
	}
	entry, err := lookupExecutable()
	if err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.path_unknown", err.Error()))
		return false, 0
	}
	paths, err := config.InstallPathsFromEntry(entry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 1
	}
	if paths.Mode == config.ModeProject {
		return false, 0
	}
	return copyForStartup(paths)
}

func copyForStartup(paths config.InstallPaths) (bool, int) {
	check := inspectPath("")
	if check.Matches {
		return false, 0
	}
	dest := filepath.Join(paths.BinDir, binaryName())
	copy, err := confirmCopy(check, dest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 1
	}
	if !copy {
		return false, 0
	}
	source, err := resolveSource("", true)
	if err == nil {
		err = rejectDest(dest, false)
		if err == nil {
			err = fs.EnsureInheritedDirectoryPath(paths.BinDir)
		}
		if err == nil {
			_, err = installBinary(source, dest)
		}
		if err == nil {
			CleanupStaleBinary(paths)
			warnPath(dest)
			err = launchInstalled(dest, config.ResolveLanguage())
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.copy_failed", err.Error()))
		return true, 1
	}
	return true, 0
}
