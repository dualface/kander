package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
