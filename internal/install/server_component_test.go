package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

func TestCompanionInstallPreflightPartialFailureAndRetry(t *testing.T) {
	setupInstallHome(t)
	source := stubBinary(t)
	server := filepath.Join(filepath.Dir(source), componentBinaryName("kander-server"))
	if err := os.WriteFile(server, []byte("service executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(paths.BinDir, binaryName())
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("original client"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A sibling directory is a preflight failure before the client is replaced.
	if err := os.Remove(server); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(server, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{Source: source, CopyBinary: true}); err == nil {
		t.Fatal("accepted invalid companion")
	}
	if data, _ := os.ReadFile(dest); string(data) != "original client" {
		t.Fatal("preflight changed client")
	}
	if err := os.Remove(server); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server, []byte("service executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := writeExec
	t.Cleanup(func() { writeExec = previous })
	failServer := true
	writeExec = func(anchor, path string, data []byte, replace bool) error {
		if path == filepath.Join(paths.BinDir, componentBinaryName("kander-server")) && failServer {
			return errors.New("injected server write failure")
		}
		return previous(anchor, path, data, replace)
	}
	result, err := Perform(Request{Source: source, CopyBinary: true})
	if err == nil || !result.Copied || result.ServerCopied || !strings.Contains(err.Error(), "client installed") {
		t.Fatalf("partial=%+v %v", result, err)
	}
	failServer = false
	result, err = Perform(Request{Source: source, CopyBinary: true})
	if err != nil || !result.ServerCopied || result.ServerBinary == "" {
		t.Fatalf("retry=%+v %v", result, err)
	}
	if data, err := os.ReadFile(result.ServerBinary); err != nil || string(data) != "service executable" {
		t.Fatalf("server=%q %v", data, err)
	}
}

func TestStandaloneServerInstallDoesNotWriteAgentFiles(t *testing.T) {
	home := setupInstallHome(t)
	source := stubBinary(t)
	dest, copied, err := InstallServer(source, filepath.Join(t.TempDir(), "bin"))
	if err != nil || !copied || filepath.Base(dest) != componentBinaryName("kander-server") {
		t.Fatalf("install=%s %v %v", dest, copied, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("server installed agent files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "kander")); !os.IsNotExist(err) {
		t.Fatalf("server installed client config: %v", err)
	}
	if _, copied, err := InstallServer(dest, filepath.Dir(dest)); err != nil || copied {
		t.Fatalf("self install=%v %v", copied, err)
	}
	stubPackaged(t, true)
	if _, _, err := InstallServer(source, t.TempDir()); err == nil {
		t.Fatal("Store service copied itself")
	}
}

func componentTestArchive(t *testing.T, asset, name string, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if strings.HasSuffix(asset, ".zip") {
		zw := zip.NewWriter(&out)
		header := &zip.FileHeader{Name: name}
		header.SetMode(0o755)
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&out)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o755, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func TestServiceUpdateVerifiesComponentDigestAndVersionBeforeReplacement(t *testing.T) {
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skip("no release asset")
	}
	asset = strings.Replace(asset, "kander-", "kander-server-", 1)
	name := componentBinaryName("kander-server")
	payload := []byte("verified service executable")
	archive := componentTestArchive(t, asset, name, payload)
	sum := sha256.Sum256(archive)
	dest := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dest, []byte("old service"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldHTTP := updateDownloadHTTPClient
	t.Cleanup(func() { updateDownloadHTTPClient = oldHTTP })
	tamper := false
	updateDownloadHTTPClient = &http.Client{Transport: updateRoundTrip(func(req *http.Request) (*http.Response, error) {
		data := archive
		if strings.HasSuffix(req.URL.Path, "checksums.txt") {
			data = []byte(fmt.Sprintf("%x  %s\n", sum, asset))
		} else if tamper {
			data = []byte("tampered archive")
		}
		return &http.Response{StatusCode: 200, Request: req, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	info := UpdateInfo{Version: "2.0.0-alpha.2", AssetName: asset}
	probeVersion := "2.0.0-alpha.1"
	probes := 0
	probe := func(_ context.Context, staged string) (string, error) {
		probes++
		data, err := os.ReadFile(staged)
		if err != nil || !bytes.Equal(data, payload) || filepath.Base(staged) != name {
			t.Fatalf("staged=%q %v", data, err)
		}
		return probeVersion, nil
	}
	if _, err := applyComponentUpdate(t.Context(), info, dest, name, probe); err == nil {
		t.Fatal("accepted wrong component version")
	}
	if data, _ := os.ReadFile(dest); string(data) != "old service" {
		t.Fatal("changed destination before version validation")
	}
	probeVersion = info.Version
	tamper = true
	if _, err := applyComponentUpdate(t.Context(), info, dest, name, probe); err == nil || probes != 1 {
		t.Fatalf("bad checksum reached probe: %d %v", probes, err)
	}
	tamper = false
	result, err := applyComponentUpdate(t.Context(), info, dest, name, probe)
	if err != nil || result.Path != dest || result.Version != info.Version {
		t.Fatalf("update=%+v %v", result, err)
	}
	if data, _ := os.ReadFile(dest); !bytes.Equal(data, payload) {
		t.Fatalf("installed=%q", data)
	}
	wrong := componentTestArchive(t, asset, binaryName(), payload)
	if _, err := extractComponentBinary(asset, wrong, name); err == nil {
		t.Fatal("server accepted client archive")
	}
	if _, err := extractComponentBinary(asset, archive, binaryName()); err == nil {
		t.Fatal("client accepted server archive")
	}
}

func TestServiceStableUpdateAndDowngradeBoundaries(t *testing.T) {
	oldVersion, oldHTTP := version.Version, updateCheckHTTPClient
	t.Cleanup(func() { version.Version = oldVersion; updateCheckHTTPClient = oldHTTP })
	version.Version = "2.0.0-alpha.2"
	calls := 0
	updateCheckHTTPClient = &http.Client{Transport: updateRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Request: req, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v2.1.0"}`))}, nil
	})}
	if info, err := CheckServerUpdate(t.Context()); err != nil || info != nil || calls != 0 {
		t.Fatalf("prerelease stable offer=%+v %v calls=%d", info, err, calls)
	}
	for _, target := range []string{"1.9.0", "2.0.0-alpha.1", "2.0.0-alpha.2", "invalid"} {
		if _, err := ApplyServerUpdate(t.Context(), target); err == nil {
			t.Fatalf("accepted target %s", target)
		}
	}
	version.Version = "2.0.0"
	info, err := CheckServerUpdate(t.Context())
	if err != nil || info == nil || info.Brew || !strings.HasPrefix(info.AssetName, "kander-server-") {
		t.Fatalf("server offer=%+v %v", info, err)
	}
	stubPackaged(t, true)
	if _, err := ApplyServerUpdate(t.Context(), "2.1.0"); err == nil {
		t.Fatal("Store service self-updated")
	}
}
