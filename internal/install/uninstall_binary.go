package install

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/process"
)

// The global entry is the kander the user runs from PATH. A direct install copies it into the
// global bin directory, which uninstall deletes. Homebrew links it from the Cellar and the
// Microsoft Store installs it inside a package: uninstall leaves those to their package manager.
var (
	// uninstallPackaged reports a Microsoft Store (MSIX) install.
	uninstallPackaged = Packaged
	// uninstallExecutable is the running binary.
	uninstallExecutable = os.Executable
	// uninstallVersion runs `<path> version` and returns its first output line.
	uninstallVersion = probeVersionLine
	// uninstallBrewEntry finds a kander that Homebrew manages.
	uninstallBrewEntry = brewEntry
	// removeEntry deletes the global entry without following a link.
	removeEntry = fs.RemoveNonDirectoryIfExists
	// startDeferredDelete starts a detached helper that deletes path once process pid exited.
	startDeferredDelete = startDeferredDeleteHelper
)

// planBinaryRemoval plans the global entry: deleted when it is a kander binary the installer
// copied, skipped with the package manager's uninstall when Homebrew or the Microsoft Store
// owns it, and kept with the reason otherwise.
func planBinaryRemoval(paths config.InstallPaths) []UninstallItem {
	if uninstallPackaged() {
		exe, _ := uninstallExecutable()
		return []UninstallItem{{Action: UninstallSkip, Path: exe, Reason: config.Text("install.uninstall_binary_msix")}}
	}
	entry := filepath.Join(paths.BinDir, binaryName())
	var items []UninstallItem
	keep := func(reason string) {
		items = append(items, UninstallItem{Action: UninstallKeep, Path: entry, Reason: reason})
	}
	brew := uninstallBrewEntry()
	info, err := os.Lstat(entry)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		keep(err.Error())
	case info.Mode()&os.ModeSymlink != 0:
		if resolved, err := filepath.EvalSymlinks(entry); err == nil && isBrewManaged(resolved) {
			brew = entry
			break
		}
		keep(config.Text("install.uninstall_binary_symlink"))
	case fs.IsReparsePoint(entry) || !info.Mode().IsRegular():
		keep(config.Text("install.uninstall_binary_symlink"))
	default:
		line, err := uninstallVersion(entry)
		if err != nil || !strings.HasPrefix(line, "kander ") {
			keep(config.Text("install.uninstall_binary_not_kander", strings.TrimSpace(line)))
			break
		}
		items = append(items, UninstallItem{Action: UninstallRemove, Path: entry})
	}
	if brew != "" {
		items = append(items, UninstallItem{Action: UninstallSkip, Path: brew, Reason: config.Text("install.uninstall_binary_brew")})
	}
	return items
}

// brewEntry returns the kander that Homebrew manages: the first kander on PATH when it resolves
// into a Cellar, else the running binary when it does. It returns "" for neither.
func brewEntry() string {
	if found, err := firstPathBinary(os.Getenv("PATH"), pathNames()); err == nil && found != "" {
		if resolved, err := filepath.EvalSymlinks(found); err == nil && isBrewManaged(resolved) {
			return found
		}
	}
	exe, err := uninstallExecutable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && isBrewManaged(resolved) {
		return exe
	}
	return ""
}

// probeVersionLine runs `<path> version` with a short deadline and returns its first line.
func probeVersionLine(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, command.VersionArguments()...).Output()
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), err
}

// removeBinary deletes the global entry. Windows refuses to delete a running executable: the
// entry is then renamed to <name>.old in its directory, and a detached helper deletes that file
// after this process exited. The renamed file is returned as an outcome: removed by the helper,
// or kept with the reason when the helper cannot start (the next install deletes it).
func removeBinary(item UninstallItem, out io.Writer) (*UninstallItem, error) {
	anchor, err := fileAnchor(item.Path)
	if err != nil {
		return nil, err
	}
	observeUninstallWrite(item.Path)
	_, err = removeEntry(anchor, item.Path)
	if err == nil {
		fmt.Fprintln(out, config.Text("install.uninstall_removed", item.Path))
		return nil, nil
	}
	if !asideOnBusy() || !fileIsBusy(err) {
		return nil, err
	}
	aside := item.Path + ".old"
	observeUninstallWrite(aside)
	_, _ = fs.RemoveNonDirectoryIfExists(anchor, aside)
	if renameErr := fs.Rename(anchor, item.Path, aside); renameErr != nil {
		return nil, errors.Join(err, renameErr)
	}
	if err := startDeferredDelete(aside, os.Getpid()); err != nil {
		reason := config.Text("install.uninstall_binary_left", item.Path, aside, err)
		fmt.Fprintln(out, reason)
		return &UninstallItem{Action: UninstallKeep, Path: aside, Reason: reason}, nil
	}
	reason := config.Text("install.uninstall_binary_deferred", item.Path, aside)
	fmt.Fprintln(out, reason)
	return &UninstallItem{Action: UninstallRemove, Path: aside, Reason: reason}, nil
}

// startDeferredDeleteHelper starts a hidden, detached PowerShell that waits for process pid to
// exit and then deletes path. The script is passed encoded, so no path needs shell quoting. It
// is used on Windows only, where a running executable cannot be deleted.
func startDeferredDeleteHelper(path string, pid int) error {
	quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	script := "Wait-Process -Id " + strconv.Itoa(pid) + " -ErrorAction SilentlyContinue; " +
		"Remove-Item -LiteralPath " + quoted + " -Force -ErrorAction SilentlyContinue"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	process.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// encodePowerShell encodes a script for -EncodedCommand: base64 of its UTF-16LE bytes.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, 0, 2*len(units))
	for _, u := range units {
		data = append(data, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(data)
}
