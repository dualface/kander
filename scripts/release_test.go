package scripts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runRelease runs the real release.sh with dir as its working directory and
// returns the exit code and stderr. The script cds to the git top level of its
// working directory, so dir must be a git work tree.
func runRelease(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	script, err := filepath.Abs("release.sh")
	if err != nil {
		t.Fatalf("resolve release.sh: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, stderr.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), stderr.String()
	default:
		t.Fatalf("run release.sh: %v", err)
		return 0, ""
	}
}

// newWorkTree creates an empty git work tree in a temporary directory.
func newWorkTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// TestReleaseSkipTapExitStatus guards the --skip-tap path: it exits before the
// tap work directory is created, and the EXIT trap must not turn that success
// into exit 1. --checksums implies --dry-run, so no gh call, tag, or push runs.
func TestReleaseSkipTapExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release.sh is a maintainer bash script; not run on Windows")
	}
	for _, tool := range []string{"bash", "git", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("release.sh needs %s, which is not on PATH", tool)
		}
	}

	t.Run("success exits 0", func(t *testing.T) {
		dir := newWorkTree(t)
		checksums := filepath.Join(t.TempDir(), "checksums.txt")
		if err := os.WriteFile(checksums, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stderr := runRelease(t, dir, "--skip-tap", "--checksums", checksums, "v0.0.0")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if !strings.Contains(stderr, "skipping the Homebrew tap update") {
			t.Fatalf("stderr lacks the skip-tap notice:\n%s", stderr)
		}
	})

	t.Run("missing checksums file fails", func(t *testing.T) {
		dir := newWorkTree(t)
		missing := filepath.Join(t.TempDir(), "missing.txt")
		code, stderr := runRelease(t, dir, "--skip-tap", "--checksums", missing, "v0.0.0")
		if code == 0 {
			t.Fatalf("exit code = 0, want nonzero; stderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, "no such checksums file") {
			t.Fatalf("stderr lacks the missing-file error:\n%s", stderr)
		}
	})
}
