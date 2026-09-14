package install

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

type copyConfirm func(check pathCheck, dest string) (bool, error)

var confirmCopy copyConfirm = runCopyConfirmation

// ConfirmCopy presents the optional binary-copy question. The TUI registers a
// shared confirmation dialog at init; while unset, the Huh form remains so
// this package can be used without the board.
type ConfirmCopy func(title, body string) (bool, error)

// SetConfirmCopy wires the copy prompt. A nil function restores the Huh form.
func SetConfirmCopy(fn ConfirmCopy) {
	if fn == nil {
		confirmCopy = runCopyConfirmation
		return
	}
	confirmCopy = func(check pathCheck, dest string) (bool, error) {
		return fn(config.Text("install.copy_question"), copyDescription(check, dest))
	}
}

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

func runCopyConfirmation(check pathCheck, dest string) (bool, error) {
	copy := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(config.Text("install.copy_question")).
			Description(copyDescription(check, dest)).
			Affirmative(config.Text("install.confirm_yes")).
			Negative(config.Text("install.confirm_no")).Value(&copy),
	))
	err := form.Run()
	return copy, mapWizardErr(err)
}

func offerCopy(paths config.InstallPaths) (bool, error) {
	check := inspectPath("")
	if check.Matches {
		return false, nil
	}
	return confirmCopy(check, filepath.Join(paths.BinDir, binaryName()))
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
	copy, err := offerCopy(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 1
	}
	if !copy {
		return false, 0
	}
	source, err := resolveSource("", true)
	if err == nil {
		dest := filepath.Join(paths.BinDir, binaryName())
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
