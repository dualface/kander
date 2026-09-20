package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/version"
)

func TestUpdateAssetName(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch, want string
		ok                 bool
	}{
		{"linux", "amd64", "kander-linux-amd64.tar.gz", true},
		{"darwin", "arm64", "kander-darwin-arm64.tar.gz", true},
		{"windows", "amd64", "kander-windows-amd64.zip", true},
		{"freebsd", "amd64", "", false},
		{"linux", "386", "", false},
	} {
		got, ok := updateAssetName(tc.goos, tc.goarch)
		if got != tc.want || ok != tc.ok {
			t.Errorf("updateAssetName(%q, %q) = %q, %v", tc.goos, tc.goarch, got, ok)
		}
	}
}

func TestChecksumForRequiresOneExactValidEntry(t *testing.T) {
	digest := sha256.Sum256([]byte("archive"))
	line := hex.EncodeToString(digest[:]) + "  kander-linux-amd64.tar.gz\n"
	got, err := checksumFor([]byte(line), "kander-linux-amd64.tar.gz")
	if err != nil || !bytes.Equal(got, digest[:]) {
		t.Fatalf("checksum=%x err=%v", got, err)
	}
	for _, bad := range []string{
		"bad  kander-linux-amd64.tar.gz\n",
		line + line,
		hex.EncodeToString(digest[:]) + "  other.tar.gz\n",
	} {
		if _, err := checksumFor([]byte(bad), "kander-linux-amd64.tar.gz"); err == nil {
			t.Fatalf("accepted checksum file %q", bad)
		}
	}
}

func TestExtractUpdateTarRejectsUnsafeEntries(t *testing.T) {
	archive := func(entries ...tar.Header) []byte {
		var out bytes.Buffer
		gz := gzip.NewWriter(&out)
		tw := tar.NewWriter(gz)
		for _, entry := range entries {
			if err := tw.WriteHeader(&entry); err != nil {
				t.Fatal(err)
			}
			if entry.Size > 0 {
				if _, err := io.CopyN(tw, strings.NewReader(strings.Repeat("x", int(entry.Size))), entry.Size); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	regular := tar.Header{Name: binaryName(), Mode: 0o755, Size: 3, Typeflag: tar.TypeReg}
	got, err := extractUpdateTar(archive(regular))
	if err != nil || string(got) != "xxx" {
		t.Fatalf("binary=%q err=%v", got, err)
	}
	for _, entries := range [][]tar.Header{
		{{Name: "../" + binaryName(), Size: 1, Typeflag: tar.TypeReg}},
		{{Name: binaryName(), Typeflag: tar.TypeSymlink, Linkname: "target"}},
		{regular, regular},
		{{Name: "other", Size: 1, Typeflag: tar.TypeReg}},
	} {
		if _, err := extractUpdateTar(archive(entries...)); err == nil {
			t.Fatalf("accepted unsafe entries: %#v", entries)
		}
	}
}

type updateRoundTrip func(*http.Request) (*http.Response, error)

func (f updateRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestCheckUpdateStableRelease(t *testing.T) {
	oldVersion, oldClient := version.Version, updateCheckHTTPClient
	t.Cleanup(func() { version.Version, updateCheckHTTPClient = oldVersion, oldClient })
	version.Version = "1.2.3"
	updateCheckHTTPClient = &http.Client{Transport: updateRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v1.3.0","draft":false,"prerelease":false}`)),
			Request:    request,
		}, nil
	})}
	info, err := CheckUpdate(t.Context())
	if err != nil || info == nil || info.Version != "1.3.0" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	version.Version = "dev"
	updateCheckHTTPClient = &http.Client{Transport: updateRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("must not request")
	})}
	if info, err := CheckUpdate(t.Context()); err != nil || info != nil {
		t.Fatalf("dev info=%+v err=%v", info, err)
	}
}

// stubBrew swaps the Homebrew seams of applyBrewUpdate and records the
// command sequence. failOn names the brew argument that returns an error.
func stubBrew(t *testing.T, failOn string, installedVersion string, probeErr error) *[][]string {
	t.Helper()
	origLook, origRun, origProbe := updateLookPath, runBrew, probeBinary
	t.Cleanup(func() { updateLookPath, runBrew, probeBinary = origLook, origRun, origProbe })
	var calls [][]string
	updateLookPath = func(name string) (string, error) { return "/fake/bin/" + name, nil }
	runBrew = func(_ context.Context, brew string, output io.Writer, args ...string) error {
		calls = append(calls, append([]string{brew}, args...))
		if _, err := fmt.Fprintln(output, "brew "+strings.Join(args, " ")); err != nil {
			return err
		}
		if len(args) > 0 && args[0] == failOn {
			return errors.New("brew " + failOn + " failed")
		}
		return nil
	}
	probeBinary = func(context.Context, string) (string, error) { return installedVersion, probeErr }
	return &calls
}

func TestApplyBrewUpdateRefreshesTapThenUpgradesFormula(t *testing.T) {
	calls := stubBrew(t, "", "0.7.12", nil)
	result, err := applyBrewUpdate(t.Context(), UpdateInfo{Version: "0.7.12"})
	if err != nil {
		t.Fatalf("applyBrewUpdate: %v", err)
	}
	want := [][]string{
		{"/fake/bin/brew", "update"},
		{"/fake/bin/brew", "upgrade", "dualface/tap/kander"},
	}
	if len(*calls) != len(want) {
		t.Fatalf("calls=%v want %v", *calls, want)
	}
	for i := range want {
		if strings.Join((*calls)[i], " ") != strings.Join(want[i], " ") {
			t.Fatalf("calls=%v want %v", *calls, want)
		}
	}
	if result.Path != "/fake/bin/kander" && result.Path != "/fake/bin/kander.exe" {
		t.Fatalf("result.Path=%q", result.Path)
	}
	if result.Version != "0.7.12" || !strings.Contains(result.Diagnostic, "brew update") {
		t.Fatalf("result=%+v", result)
	}
}

func TestApplyBrewUpdateStopsWhenRefreshFails(t *testing.T) {
	calls := stubBrew(t, "update", "0.7.12", nil)
	_, err := applyBrewUpdate(t.Context(), UpdateInfo{Version: "0.7.12"})
	if err == nil || !strings.Contains(err.Error(), "brew update") {
		t.Fatalf("err=%v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("upgrade ran after failed refresh: %v", *calls)
	}
}

func TestApplyBrewUpdateReportsUpgradeFailure(t *testing.T) {
	calls := stubBrew(t, "upgrade", "0.7.12", nil)
	result, err := applyBrewUpdate(t.Context(), UpdateInfo{Version: "0.7.12"})
	if err == nil || !strings.Contains(err.Error(), "brew upgrade dualface/tap/kander") {
		t.Fatalf("err=%v", err)
	}
	if len(*calls) != 2 || !strings.Contains(result.Diagnostic, "brew upgrade") {
		t.Fatalf("calls=%v diagnostic=%q", *calls, result.Diagnostic)
	}
}

func TestApplyBrewUpdateRejectsStaleInstalledVersion(t *testing.T) {
	stubBrew(t, "", "0.7.11", nil)
	_, err := applyBrewUpdate(t.Context(), UpdateInfo{Version: "0.7.12"})
	if err == nil || !strings.Contains(err.Error(), "0.7.11") || !strings.Contains(err.Error(), "0.7.12") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyBrewUpdateReportsProbeFailure(t *testing.T) {
	stubBrew(t, "", "", errors.New("probe failed"))
	_, err := applyBrewUpdate(t.Context(), UpdateInfo{Version: "0.7.12"})
	if err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteBinaryRestoresBusyOriginalAfterReplacementFailure(t *testing.T) {
	home := setupInstallHome(t)
	dest := home + "/kander"
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	origAside, origBusy, origWrite := asideOnBusy, fileIsBusy, writeExec
	t.Cleanup(func() { asideOnBusy, fileIsBusy, writeExec = origAside, origBusy, origWrite })
	calls := 0
	asideOnBusy = func() bool { return true }
	fileIsBusy = func(error) bool { return true }
	writeExec = func(string, string, []byte, bool) error {
		calls++
		if calls == 1 {
			return errors.New("busy")
		}
		return errors.New("write failed")
	}
	if err := writeBinary(dest, []byte("new")); err == nil {
		t.Fatal("expected replacement failure")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "old" {
		t.Fatalf("restored=%q err=%v", got, err)
	}
}
