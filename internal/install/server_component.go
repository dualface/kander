package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/version"
)

func componentBinaryName(component string) string {
	if runtime.GOOS == "windows" {
		return component + ".exe"
	}
	return component
}

// companionSource preflights an optional sibling before installing either file.
func companionSource(source string) (string, error) {
	candidate := filepath.Join(filepath.Dir(source), componentBinaryName("kander-server"))
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := rejectSource(candidate); err != nil {
		return "", err
	}
	if !destIsExecutable(candidate) {
		return "", errors.New("server companion is not executable")
	}
	if _, err := readSource(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

// InstallServer installs just the service component. It does not write client
// configuration or agent rules and does not start or restart a service.
func InstallServer(source, dir string) (path string, copied bool, err error) {
	if Packaged() {
		return "", false, errStoreManaged()
	}
	source, err = resolveSource(source, true)
	if err != nil {
		return "", false, err
	}
	if dir == "" {
		paths, e := config.GlobalInstallPaths()
		if e != nil {
			return "", false, e
		}
		dir = paths.BinDir
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	path = filepath.Join(dir, componentBinaryName("kander-server"))
	if err = rejectDest(path, false); err != nil {
		return path, false, err
	}
	data, err := readSource(source)
	if err != nil {
		return path, false, err
	}
	if sameFile(source, path) {
		return path, false, nil
	}
	if err = fs.EnsureInheritedDirectoryPath(dir); err != nil {
		return path, false, err
	}
	if err = writeBinary(path, data); err != nil {
		return path, false, err
	}
	CleanupStaleBinary(config.InstallPaths{BinDir: dir})
	return path, true, nil
}

// CheckServerUpdate uses the stable channel with the same prerelease isolation
// as the client. Operators can explicitly name a prerelease for ApplyServerUpdate.
func CheckServerUpdate(ctx context.Context) (*UpdateInfo, error) {
	info, err := CheckUpdate(ctx)
	if err != nil || info == nil {
		return info, err
	}
	info.AssetName = strings.Replace(info.AssetName, "kander-", "kander-server-", 1)
	info.Brew = false
	return info, nil
}

// ApplyServerUpdate verifies the service-specific archive and executable before
// replacing the current component. It never sends signals to a serving process.
func ApplyServerUpdate(ctx context.Context, target string) (UpdateResult, error) {
	if Packaged() {
		return UpdateResult{}, errStoreManaged()
	}
	target = strings.TrimPrefix(target, "v")
	if !version.Valid(target) {
		return UpdateResult{}, errors.New("invalid server update version")
	}
	current := version.String()
	if version.Valid(current) && version.Compare(target, current) <= 0 {
		return UpdateResult{}, fmt.Errorf("server update %s is not newer than %s", target, current)
	}
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return UpdateResult{}, errors.New("unsupported server update platform")
	}
	exe, err := os.Executable()
	if err != nil {
		return UpdateResult{}, err
	}
	// Package managers own their versioned paths. Never mutate a Homebrew Cellar.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return UpdateResult{}, err
	}
	if isBrewManaged(resolved) {
		return UpdateResult{}, errors.New("update the service through its package manager")
	}
	return applyComponentUpdate(ctx, UpdateInfo{Current: current, Version: target, AssetName: strings.Replace(asset, "kander-", "kander-server-", 1)}, resolved, componentBinaryName("kander-server"), probeServerBinary)
}

func probeServerBinary(ctx context.Context, path string) (string, error) {
	return validateComponentVersion(ctx, path, "--version", "")
}
