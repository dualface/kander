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
	"runtime"
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
	got, err := extractComponentTar(archive(regular), binaryName())
	if err != nil || string(got) != "xxx" {
		t.Fatalf("binary=%q err=%v", got, err)
	}
	for _, entries := range [][]tar.Header{
		{{Name: "../" + binaryName(), Size: 1, Typeflag: tar.TypeReg}},
		{{Name: binaryName(), Typeflag: tar.TypeSymlink, Linkname: "target"}},
		{regular, regular},
		{{Name: "other", Size: 1, Typeflag: tar.TypeReg}},
	} {
		if _, err := extractComponentTar(archive(entries...), binaryName()); err == nil {
			t.Fatalf("accepted unsafe entries: %#v", entries)
		}
	}
}

type updateRoundTrip func(*http.Request) (*http.Response, error)

func (f updateRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// stubUpdateCheck answers every release request with body and counts the
// requests; it also pins the running version and the executable path, so a
// Cellar path stands in for a Homebrew install.
func stubUpdateCheck(t *testing.T, current, executable, body string) *int {
	t.Helper()
	stubPackaged(t, false)
	oldVersion, oldClient, oldExecutable := version.Version, updateCheckHTTPClient, updateExecutable
	t.Cleanup(func() {
		version.Version, updateCheckHTTPClient, updateExecutable = oldVersion, oldClient, oldExecutable
	})
	version.Version = current
	updateExecutable = func() (string, error) { return executable, nil }
	var requests int
	updateCheckHTTPClient = &http.Client{Transport: updateRoundTrip(func(request *http.Request) (*http.Response, error) {
		requests++
		// Spelled out rather than taken from releaseListURL, so a regression
		// of that constant fails here.
		if request.URL.String() != "https://api.github.com/repos/dualface/kander/releases?per_page=30" {
			return nil, fmt.Errorf("unexpected request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	return &requests
}

// releaseList renders a releases list page; each entry is "tag", "tag pre",
// "tag draft", or "tag draft pre".
func releaseList(entries ...string) string {
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		tag, flags, _ := strings.Cut(entry, " ")
		items = append(items, fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"body":"notes","assets":[]}`,
			tag, strings.Contains(flags, "draft"), strings.Contains(flags, "pre")))
	}
	return "[" + strings.Join(items, ",") + "]"
}

const directExecutable = "/usr/local/bin/kander"
const brewExecutable = "/opt/homebrew/Cellar/kander/2.0.0-beta.1/bin/kander"

type channelCase struct {
	name, current string
	entries       []string
	want          string
}

func runChannelCases(t *testing.T, cases []channelCase) {
	t.Helper()
	if _, ok := updateAssetName(runtime.GOOS, runtime.GOARCH); !ok {
		t.Skipf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests := stubUpdateCheck(t, tc.current, directExecutable, releaseList(tc.entries...))
			info, err := CheckUpdate(t.Context())
			if err != nil || *requests != 1 {
				t.Fatalf("err=%v requests=%d", err, *requests)
			}
			if tc.want == "" {
				if info != nil {
					t.Fatalf("info=%+v, want no update", info)
				}
				return
			}
			if info == nil || info.Version != tc.want || info.Current != strings.TrimPrefix(tc.current, "v") || info.Brew {
				t.Fatalf("info=%+v, want %s", info, tc.want)
			}
		})
	}
}

// A 2.x release updates only to a newer 2.x release, even when GitHub's
// latest is a 1.x release; a dev build never asks.
func TestCheckUpdateStableRelease(t *testing.T) {
	runChannelCases(t, []channelCase{
		{"release ignores 1.x and prereleases", "2.0.0", []string{"v2.0.1", "v1.7.0", "v2.1.0-beta.1 pre"}, "2.0.1"},
		{"release tag with prerelease flag", "2.0.0", []string{"v2.0.1 pre"}, ""},
		{"nothing newer", "2.0.1", []string{"v2.0.1", "v2.0.0"}, ""},
		{"leading v on the running version", "v2.0.0", []string{"v2.0.1"}, "2.0.1"},
	})
	requests := stubUpdateCheck(t, "dev", directExecutable, releaseList("v2.0.1"))
	if info, err := CheckUpdate(t.Context()); err != nil || info != nil || *requests != 0 {
		t.Fatalf("dev info=%+v err=%v requests=%d", info, err, *requests)
	}
}

// A 2.x prerelease updates only to a newer 2.x prerelease, never to a release.
func TestCheckUpdateSeparatesV2Prereleases(t *testing.T) {
	runChannelCases(t, []channelCase{
		{"prerelease to newer prerelease", "2.0.0-beta.1", []string{"v2.0.0-beta.2 pre"}, "2.0.0-beta.2"},
		{"prerelease ignores releases", "2.0.0-beta.1", []string{"v2.0.0", "v1.6.4"}, ""},
		{"prerelease takes the highest", "2.0.0-beta.1", []string{"v2.0.0-rc.1 pre", "v2.0.0-beta.3 pre"}, "2.0.0-rc.1"},
		{"numeric prerelease order", "2.0.0-rc.9", []string{"v2.0.0-rc.10 pre"}, "2.0.0-rc.10"},
		{"draft prerelease", "2.0.0-beta.1", []string{"v2.0.0-beta.2 draft pre"}, ""},
		{"prerelease tag without prerelease flag", "2.0.0-beta.1", []string{"v2.0.0-beta.2"}, ""},
		{"tags need exactly one leading v", "2.0.0-beta.1", []string{"vv2.0.0-beta.2 pre", "2.0.0-beta.3 pre"}, ""},
	})
}

func TestCheckUpdateSkipsVersionsOutsideChannels(t *testing.T) {
	for _, current := range []string{"vv2.0.0", "2.0.0-dev+612d677", "2.0.0-beta.1-5-gabc1234", "2.0.0+build.1", "1.6.3", "3.0.0", "2.0.0-beta", "2.0.0-beta.01", "2.0.0-preview.1"} {
		requests := stubUpdateCheck(t, current, directExecutable, releaseList("v2.0.1", "v2.0.0-beta.9 pre"))
		if info, err := CheckUpdate(t.Context()); err != nil || info != nil || *requests != 0 {
			t.Fatalf("%s: info=%+v err=%v requests=%d", current, info, err, *requests)
		}
	}
}

func TestCheckUpdateSkipsHomebrewPrerelease(t *testing.T) {
	calls := stubBrew(t, "", "", nil)
	requests := stubUpdateCheck(t, "2.0.0-beta.1", brewExecutable, releaseList("v2.0.0-beta.2 pre"))
	if info, err := CheckUpdate(t.Context()); err != nil || info != nil || *requests != 0 || len(*calls) != 0 {
		t.Fatalf("info=%+v err=%v requests=%d brew=%v", info, err, *requests, *calls)
	}
}

func TestCheckUpdateReportsHomebrewRelease(t *testing.T) {
	if _, ok := updateAssetName(runtime.GOOS, runtime.GOARCH); !ok {
		t.Skipf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	stubUpdateCheck(t, "2.0.0", "/opt/homebrew/Cellar/kander/2.0.0/bin/kander", releaseList("v2.0.1"))
	if info, err := CheckUpdate(t.Context()); err != nil || info == nil || info.Version != "2.0.1" || !info.Brew {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestCheckUpdateRejectsBadReleaseList(t *testing.T) {
	if _, ok := updateAssetName(runtime.GOOS, runtime.GOARCH); !ok {
		t.Skipf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	oversized := `[{"tag_name":"v2.0.1","body":"` + strings.Repeat("x", maxReleaseList) + `"}]`
	for name, body := range map[string]string{
		"oversized": oversized,
		"not json":  "<html>",
		"object":    `{"tag_name":"v2.0.1","draft":false,"prerelease":false}`,
		"null":      `null`,
		"null item": `[{"tag_name":"v2.0.1","draft":false,"prerelease":false},null]`,
		"no draft":  `[{"tag_name":"v2.0.1","prerelease":false}]`,
		"null flag": `[{"tag_name":"v2.0.1","draft":false,"prerelease":null}]`,
		"no tag":    `[{"draft":false,"prerelease":false}]`,
		"bad item":  `[{"tag_name":"v2.0.1","draft":false,"prerelease":false},{"tag_name":7,"draft":false,"prerelease":false}]`,
	} {
		for _, current := range []string{"2.0.0", "2.0.0-beta.1"} {
			stubUpdateCheck(t, current, directExecutable, body)
			if info, err := CheckUpdate(t.Context()); err == nil || info != nil {
				t.Fatalf("%s/%s: info=%+v err=%v", name, current, info, err)
			}
		}
	}
}

func TestApplyUpdateRefusesOtherChannels(t *testing.T) {
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	oldDownload := updateDownloadHTTPClient
	t.Cleanup(func() { updateDownloadHTTPClient = oldDownload })
	var downloads int
	updateDownloadHTTPClient = &http.Client{Transport: updateRoundTrip(func(*http.Request) (*http.Response, error) {
		downloads++
		return nil, errors.New("must not download")
	})}
	for _, tc := range []struct{ current, target, executable, want string }{
		{"2.0.0-beta.1", "2.0.0", directExecutable, "release channel"},
		{"2.0.0", "2.0.1-beta.1", directExecutable, "release channel"},
		{"2.0.0", "2.0.1+build.1", directExecutable, "release channel"},
		{"2.0.0", "3.0.0", directExecutable, "release channel"},
		{"2.0.0", "v2.0.1", directExecutable, "release channel"},
		{"dev", "2.0.1", directExecutable, "release channel"},
		{"2.0.0", "2.0.0", directExecutable, "not newer"},
		{"2.0.0-beta.2", "2.0.0-beta.1", directExecutable, "not newer"},
		{"2.0.0-beta.1", "2.0.0-beta.2", brewExecutable, "Homebrew"},
	} {
		calls := stubBrew(t, "", tc.target, nil)
		stubUpdateCheck(t, tc.current, tc.executable, "[]")
		_, err := ApplyUpdate(t.Context(), UpdateInfo{Current: tc.current, Version: tc.target, AssetName: asset})
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(*calls) != 0 || downloads != 0 {
			t.Fatalf("%s -> %s: err=%v brew=%v downloads=%d", tc.current, tc.target, err, *calls, downloads)
		}
	}
	stubUpdateCheck(t, "2.0.0", directExecutable, "[]")
	if _, err := ApplyUpdate(t.Context(), UpdateInfo{Version: "2.0.1", AssetName: "kander-other.tar.gz"}); err == nil || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("asset mismatch err=%v", err)
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
