package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/version"
)

// The check reads one page of the releases list instead of releases/latest:
// a prerelease never becomes latest, and a later 1.x patch takes latest back
// from 2.x. One page of 30 entries (the GitHub default) reaches far past the
// newest release of either 2.x channel, and older entries are never
// compared. A measured 70-entry page with bodies and assets was about
// 950 KB, so 30 entries are about 460 KB; the 4 MiB cap keeps a full page
// with eightfold headroom and fails instead of parsing a truncated page.
const (
	releaseListURL  = "https://api.github.com/repos/dualface/kander/releases?per_page=30"
	maxReleaseList  = 4 << 20
	maxChecksums    = 1 << 20
	maxArchive      = 128 << 20
	maxUpdateBinary = 128 << 20
	maxDiagnostic   = 64 << 10
)

// UpdateInfo describes one newer release in the running binary's channel.
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

// brewFormula pins the upgrade target to this tap so a conflicting
// short-name formula can never be selected instead.
const brewFormula = "dualface/tap/kander"

var (
	updateCheckHTTPClient    = newUpdateHTTPClient(5 * time.Second)
	updateDownloadHTTPClient = newUpdateHTTPClient(10 * time.Minute)
	updateLookPath           = exec.LookPath
	updateExecutable         = os.Executable
	runBrew                  = runBrewCommand
	probeBinary              = validateUpdateBinary
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

// CheckUpdate returns the newest GitHub release in the running binary's
// channel (see channelOf). No update is (nil, nil). A binary outside every
// channel never sends a request. A Microsoft Store install never checks: the
// Store delivers its updates. A Homebrew install on the prerelease channel
// never checks either, because the tap publishes only releases.
func CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	if Packaged() {
		return nil, nil
	}
	current := strings.TrimPrefix(version.String(), "v")
	channel := channelOf(current)
	if channel == channelNone {
		return nil, nil
	}
	asset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return nil, nil
	}
	resolved, err := resolvedExecutable()
	if err != nil {
		return nil, err
	}
	brew := isBrewManaged(resolved)
	if brew && channel == channelPrerelease {
		return nil, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, err := updateGet(checkCtx, updateCheckHTTPClient, releaseListURL, maxReleaseList)
	if err != nil {
		return nil, err
	}
	target, err := newestInChannel(body, current)
	if err != nil || target == "" {
		return nil, err
	}
	return &UpdateInfo{Current: current, Version: target, AssetName: asset, Brew: brew}, nil
}

func resolvedExecutable() (string, error) {
	exe, err := updateExecutable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return resolved, nil
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
// A Microsoft Store install refuses before any download or file write.
func ApplyUpdate(ctx context.Context, info UpdateInfo) (UpdateResult, error) {
	if Packaged() {
		return UpdateResult{}, errStoreManaged()
	}
	current := strings.TrimPrefix(version.String(), "v")
	channel := channelOf(current)
	if channel == channelNone || channelOf(info.Version) != channel {
		return UpdateResult{}, errors.New("update version is not in the running version's release channel")
	}
	if version.Compare(info.Version, current) <= 0 {
		return UpdateResult{}, errors.New("update is not newer than the running version")
	}
	expectedAsset, ok := updateAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok || info.AssetName != expectedAsset {
		return UpdateResult{}, errors.New("update asset does not match this platform")
	}
	resolved, err := resolvedExecutable()
	if err != nil {
		return UpdateResult{}, err
	}
	info.Brew = isBrewManaged(resolved)
	if info.Brew && channel == channelPrerelease {
		return UpdateResult{}, errors.New("Homebrew publishes no prereleases; reinstall from the GitHub release to update a prerelease")
	}
	if info.Brew {
		return applyBrewUpdate(ctx, info)
	}
	return applyDirectUpdate(ctx, info, resolved)
}

func applyBrewUpdate(ctx context.Context, info UpdateInfo) (UpdateResult, error) {
	brew, err := updateLookPath("brew")
	if err != nil {
		return UpdateResult{}, errors.New("Homebrew executable not found")
	}
	var output limitedBuffer
	// A stale local tap makes `brew upgrade` succeed without installing
	// anything, so refresh the tap metadata before upgrading.
	if err := runBrew(ctx, brew, &output, "update"); err != nil {
		return UpdateResult{Diagnostic: output.String()}, fmt.Errorf("brew update: %w", err)
	}
	if err := runBrew(ctx, brew, &output, "upgrade", brewFormula); err != nil {
		return UpdateResult{Diagnostic: output.String()}, fmt.Errorf("brew upgrade %s: %w", brewFormula, err)
	}
	installed, err := updateLookPath(binaryName())
	if err != nil {
		return UpdateResult{Diagnostic: output.String()}, errors.New("updated kander not found on PATH")
	}
	got, err := probeBinary(ctx, installed)
	if err != nil || version.Compare(got, info.Version) < 0 {
		if err == nil {
			err = fmt.Errorf("updated kander reports %s, expected at least %s", got, info.Version)
		}
		return UpdateResult{Diagnostic: output.String()}, err
	}
	return UpdateResult{Path: installed, Version: got, Diagnostic: output.String()}, nil
}

func runBrewCommand(ctx context.Context, brew string, output io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, brew, args...)
	cmd.Stdout, cmd.Stderr = output, output
	return cmd.Run()
}

func applyDirectUpdate(ctx context.Context, info UpdateInfo, dest string) (UpdateResult, error) {
	return applyComponentUpdate(ctx, info, dest, binaryName(), probeBinary)
}

func applyComponentUpdate(ctx context.Context, info UpdateInfo, dest, name string, probe func(context.Context, string) (string, error)) (UpdateResult, error) {
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
	binary, err := extractComponentBinary(info.AssetName, archive, name)
	if err != nil {
		return UpdateResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "kander-update-*")
	if err != nil {
		return UpdateResult{}, err
	}
	defer os.RemoveAll(tempDir)
	staged := filepath.Join(tempDir, name)
	if err := os.WriteFile(staged, binary, 0o700); err != nil {
		return UpdateResult{}, err
	}
	gotVersion, err := probe(ctx, staged)
	if err != nil {
		return UpdateResult{}, err
	}
	if strings.TrimPrefix(gotVersion, "v") != strings.TrimPrefix(info.Version, "v") {
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

func extractComponentBinary(asset string, data []byte, name string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		return extractComponentZip(data, name)
	}
	return extractComponentTar(data, name)
}

func extractComponentTar(data []byte, name string) ([]byte, error) {
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
		if !safeArchiveName(header.Name) || header.Typeflag != tar.TypeReg || path.Base(header.Name) != name || binary != nil {
			return nil, errors.New("update archive has an unsafe or unexpected entry")
		}
		binary, err = readLimited(reader, maxUpdateBinary)
		if err != nil {
			return nil, err
		}
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("update archive does not contain %s", name)
	}
	return binary, nil
}

func extractComponentZip(data []byte, name string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open update archive: %w", err)
	}
	var binary []byte
	for _, file := range reader.File {
		if !safeArchiveName(file.Name) || !file.Mode().IsRegular() || path.Base(file.Name) != name || binary != nil {
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
		return nil, fmt.Errorf("update archive does not contain %s", name)
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
	return validateComponentVersion(ctx, binary, command.VersionArguments(), "kander")
}

func validateComponentVersion(ctx context.Context, binary string, arguments []string, prefix string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, binary, arguments...).Output()
	if err != nil {
		return "", fmt.Errorf("validate update binary: %w", err)
	}
	line, _, _ := strings.Cut(string(output), "\n")
	reported := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
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
