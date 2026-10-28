package install

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

// stubPackaged makes the process look like a Microsoft Store install.
func stubPackaged(t *testing.T, value bool) {
	t.Helper()
	previous := packaged
	packaged = func() bool { return value }
	t.Cleanup(func() { packaged = previous })
}

func TestPackagedIsFalseForTestProcess(t *testing.T) {
	// go test binaries never carry MSIX package identity on any platform.
	if Packaged() {
		t.Fatalf("Packaged() = true on %s test process", runtime.GOOS)
	}
}

func TestStoreInstallSkipsUpdateCheck(t *testing.T) {
	stubPackaged(t, true)
	oldVersion, oldClient := version.Version, updateCheckHTTPClient
	t.Cleanup(func() { version.Version, updateCheckHTTPClient = oldVersion, oldClient })
	version.Version = "1.2.3"
	var requests int
	updateCheckHTTPClient = &http.Client{Transport: updateRoundTrip(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("must not request")
	})}
	info, err := CheckUpdate(t.Context())
	if err != nil || info != nil || requests != 0 {
		t.Fatalf("info=%+v err=%v requests=%d", info, err, requests)
	}
}

func TestStoreInstallRefusesApplyUpdate(t *testing.T) {
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	stubPackaged(t, true)
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	oldClient, oldRun := updateDownloadHTTPClient, runBrew
	t.Cleanup(func() { updateDownloadHTTPClient, runBrew = oldClient, oldRun })
	var requests, brewCalls int
	updateDownloadHTTPClient = &http.Client{Transport: updateRoundTrip(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("must not download")
	})}
	runBrew = func(context.Context, string, io.Writer, ...string) error {
		brewCalls++
		return errors.New("must not run brew")
	}
	result, err := ApplyUpdate(t.Context(), UpdateInfo{Current: "1.2.3", Version: "9.9.9", AssetName: asset})
	if err == nil || err.Error() != config.Text("install.update_store_managed") {
		t.Fatalf("err=%v", err)
	}
	if result != (UpdateResult{}) || requests != 0 || brewCalls != 0 {
		t.Fatalf("result=%+v requests=%d brew=%d", result, requests, brewCalls)
	}
}

func TestStoreInstallSkipsStartupCopy(t *testing.T) {
	previousInteractive := startupInteractive
	startupInteractive = func() error { return nil }
	t.Cleanup(func() { startupInteractive = previousInteractive })
	t.Setenv(EnvSkipInstall, "")
	previousLookup, previousConfirm := lookupExecutable, confirmCopy
	t.Cleanup(func() { lookupExecutable, confirmCopy = previousLookup, previousConfirm })
	var lookups int
	lookupExecutable = func() (string, error) {
		lookups++
		return "", errors.New("lookup stopped by test")
	}
	confirmCopy = func(pathCheck, string) (bool, error) {
		t.Fatal("store install asked to copy")
		return false, nil
	}

	// Control: without package identity the startup copy goes on to resolve
	// the running executable, so the zero count below comes from the gate.
	stubPackaged(t, false)
	CheckStartupCopy()
	if lookups == 0 {
		t.Fatal("unpackaged startup copy never resolved the executable")
	}

	lookups = 0
	stubPackaged(t, true)
	if handled, code := CheckStartupCopy(); handled || code != 0 || lookups != 0 {
		t.Fatalf("handled=%v code=%d lookups=%d", handled, code, lookups)
	}
}

func TestStoreInstallGlobalDefaultsDoNotCopy(t *testing.T) {
	source := stubBinary(t)
	mockCurrentBinary(t, source)
	// A mismatched PATH copies for an ordinary install; see
	// TestGlobalInstallDefaultsCopyAndDeleteWithoutPrompt.
	t.Setenv("PATH", t.TempDir())
	stubPackaged(t, true)
	req := Request{Mode: config.ModeGlobal}
	applyGlobalInstallDefaults(&req)
	if req.CopyBinary || !req.DeleteLegacy {
		t.Fatalf("copy=%v delete=%v", req.CopyBinary, req.DeleteLegacy)
	}
}
