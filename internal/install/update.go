package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/internal/version"
)

const (
	latestReleaseURL = "https://api.github.com/repos/dualface/kander/releases/latest"
	maxReleaseJSON   = 1 << 20
	maxChecksums     = 1 << 20
	maxArchive       = 128 << 20
	maxUpdateBinary  = 128 << 20
	maxDiagnostic    = 64 << 10
)

// UpdateInfo describes one newer stable release compatible with this binary.
type UpdateInfo struct {
	Current   string
	Version   string
	AssetName string
	Brew      bool
}

// UpdateResult records the verified binary to restart and bounded command output.
type UpdateResult struct {
	Path       string
	Version    string
	Diagnostic string
}

var (
	updateCheckHTTPClient    = newUpdateHTTPClient(5 * time.Second)
	updateDownloadHTTPClient = newUpdateHTTPClient(10 * time.Minute)
)

func newUpdateHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !allowedUpdateHost(req.URL) {
				return errors.New("update download redirected to an untrusted host")
			}
			return nil
		},
	}
}

func allowedUpdateHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	default:
		return false
	}
}

func updateAssetName(goos, goarch string) (string, bool) {
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
		return "", false
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "kander-" + goos + "-" + goarch + ext, true
}

// CheckUpdate queries the latest stable GitHub release. No update is (nil, nil).
func CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	current := version.String()
	if !version.Valid(current) || strings.Contains(strings.TrimPrefix(current, "v"), "-") {
		return nil, nil
	}
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return nil, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, err := updateGet(checkCtx, updateCheckHTTPClient, latestReleaseURL, maxReleaseJSON)
	if err != nil {
		return nil, err
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	target := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if release.Draft || release.Prerelease || !version.Valid(target) || strings.Contains(target, "-") || version.Compare(target, current) <= 0 {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return &UpdateInfo{Current: current, Version: target, AssetName: asset, Brew: isBrewManaged(resolved)}, nil
}

func updateGet(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kander/"+version.String())
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request update: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || !allowedUpdateHost(response.Request.URL) {
		return nil, errors.New("update response came from an untrusted host")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update server returned HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read update response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("update response exceeds size limit")
	}
	return data, nil
}

// ApplyUpdate installs info and returns the verified executable path.
func ApplyUpdate(ctx context.Context, info UpdateInfo) (UpdateResult, error) {
	if !version.Valid(info.Version) || strings.Contains(info.Version, "-") {
		return UpdateResult{}, errors.New("invalid update version")
	}
	expectedAsset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok || info.AssetName != expectedAsset {
		return UpdateResult{}, errors.New("update asset does not match this platform")
	}
	exe, err := os.Executable()
	if err != nil {
		return UpdateResult{}, fmt.Errorf("resolve current executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	info.Brew = isBrewManaged(resolved)
	if info.Brew {
		return applyBrewUpdate(ctx, info)
	}
	return applyDirectUpdate(ctx, info, resolved)
}

func applyBrewUpdate(ctx context.Context, info UpdateInfo) (UpdateResult, error) {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return UpdateResult{}, errors.New("Homebrew executable not found")
	}
	var output limitedBuffer
	cmd := exec.CommandContext(ctx, brew, "upgrade", "kander")
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return UpdateResult{Diagnostic: output.String()}, fmt.Errorf("brew upgrade kander: %w", err)
	}
	installed, err := exec.LookPath(binaryName())
	if err != nil {
		return UpdateResult{Diagnostic: output.String()}, errors.New("updated kander not found on PATH")
	}
	got, err := validateUpdateBinary(ctx, installed)
	if err != nil || version.Compare(got, info.Version) < 0 {
		if err == nil {
			err = fmt.Errorf("updated kander reports %s, expected at least %s", got, info.Version)
		}
		return UpdateResult{Diagnostic: output.String()}, err
	}
	return UpdateResult{Path: installed, Version: got, Diagnostic: output.String()}, nil
}

func applyDirectUpdate(ctx context.Context, info UpdateInfo, dest string) (UpdateResult, error) {
	tag := "v" + strings.TrimPrefix(info.Version, "v")
	base := "https://github.com/dualface/kander/releases/download/" + tag + "/"
	checksums, err := updateGet(ctx, updateDownloadHTTPClient, base+"checksums.txt", maxChecksums)
	if err != nil {
		return UpdateResult{}, err
	}
	want, err := checksumFor(checksums, info.AssetName)
	if err != nil {
		return UpdateResult{}, err
	}
	archive, err := updateGet(ctx, updateDownloadHTTPClient, base+info.AssetName, maxArchive)
	if err != nil {
		return UpdateResult{}, err
	}
	got := sha256.Sum256(archive)
	if !bytes.Equal(got[:], want) {
		return UpdateResult{}, errors.New("update archive checksum mismatch")
	}
	binary, err := extractUpdateBinary(info.AssetName, archive)
	if err != nil {
		return UpdateResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "kander-update-*")
	if err != nil {
		return UpdateResult{}, err
	}
	defer os.RemoveAll(tempDir)
	staged := filepath.Join(tempDir, binaryName())
	if err := os.WriteFile(staged, binary, 0o700); err != nil {
		return UpdateResult{}, err
	}
	gotVersion, err := validateUpdateBinary(ctx, staged)
	if err != nil {
		return UpdateResult{}, err
	}
	if version.Compare(gotVersion, info.Version) != 0 {
		return UpdateResult{}, fmt.Errorf("update binary reports %s, expected %s", gotVersion, info.Version)
	}
	if err := writeBinary(dest, binary); err != nil {
		return UpdateResult{}, fmt.Errorf("replace current executable: %w", err)
	}
	return UpdateResult{Path: dest, Version: gotVersion}, nil
}

func checksumFor(data []byte, name string) ([]byte, error) {
	var match []byte
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size || match != nil {
			return nil, fmt.Errorf("expected exactly one valid checksum for %s", name)
		}
		match = decoded
	}
	if match == nil {
		return nil, fmt.Errorf("expected exactly one valid checksum for %s", name)
	}
	return match, nil
}

func extractUpdateBinary(asset string, data []byte) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		return extractUpdateZip(data)
	}
	return extractUpdateTar(data)
}

func extractUpdateTar(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open update archive: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var binary []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read update archive: %w", err)
		}
		if !safeArchiveName(header.Name) || header.Typeflag != tar.TypeReg || path.Base(header.Name) != binaryName() || binary != nil {
			return nil, errors.New("update archive has an unsafe or unexpected entry")
		}
		binary, err = readLimited(reader, maxUpdateBinary)
		if err != nil {
			return nil, err
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("update archive does not contain kander")
	}
	return binary, nil
}

func extractUpdateZip(data []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open update archive: %w", err)
	}
	var binary []byte
	for _, file := range reader.File {
		if !safeArchiveName(file.Name) || !file.Mode().IsRegular() || path.Base(file.Name) != binaryName() || binary != nil {
			return nil, errors.New("update archive has an unsafe or unexpected entry")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		binary, err = readLimited(stream, maxUpdateBinary)
		stream.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("update archive does not contain kander")
	}
	return binary, nil
}

func safeArchiveName(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && !path.IsAbs(name) && path.Clean(name) == name && name != "." && !strings.HasPrefix(name, "../")
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("update binary exceeds size limit")
	}
	return data, nil
}

func validateUpdateBinary(ctx context.Context, binary string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, binary, "version").Output()
	if err != nil {
		return "", fmt.Errorf("validate update binary: %w", err)
	}
	line, _, _ := strings.Cut(string(output), "\n")
	reported := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "kander"))
	if !version.Valid(reported) {
		return "", errors.New("update binary reported an invalid version")
	}
	return reported, nil
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := maxDiagnostic - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(len(p), remaining)]...)
	}
	if len(p) > remaining {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.TrimSpace(string(b.data))
	if b.truncated {
		text += "\n[output truncated]"
	}
	return text
}

// RestartUpdatedBinary replaces this process on Unix and waits for it on Windows.
func RestartUpdatedBinary(binary string) error {
	if strings.TrimSpace(binary) == "" {
		return errors.New("updated binary path is empty")
	}
	return handoff(binary, append([]string{binary}, os.Args[1:]...), os.Environ())
}
