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

// gateRig is a release.sh fixture that reaches the local test gate: a bare
// repository as origin, a work tree on main whose HEAD equals origin/main and
// whose CHANGELOG.md names the version, and a fake go and gh at the front of
// PATH. Nothing here touches the real HOME, network, or repository tags.
type gateRig struct {
	work      string   // work tree release.sh runs in
	origin    string   // bare repository acting as origin
	tmpdir    string   // TMPDIR of the script; the gate's directories land here
	goLog     string   // file the fake go appends one record per call to
	callerEnv []string // environment of the script, minus per-test additions
}

const gateVersion = "v0.0.1"

// fakeGo answers `go env NAME...` with the value of FAKE_NAME and records every
// other call: its arguments and the variables the gate is meant to control.
const fakeGo = `#!/bin/sh
if [ "$1" = env ]; then
	shift
	for name in "$@"; do
		eval "printf '%s\n' \"\${FAKE_$name}\""
	done
	exit 0
fi
home_state=nonempty
if [ -z "$(ls -A "$HOME")" ]; then home_state=empty; fi
config_state=nonempty
if [ -z "$(ls -A "$XDG_CONFIG_HOME")" ]; then config_state=empty; fi
{
	printf 'args=%s\n' "$*"
	printf 'HOME=%s\n' "$HOME"
	printf 'HOME_STATE=%s\n' "$home_state"
	printf 'XDG_CONFIG_HOME=%s\n' "$XDG_CONFIG_HOME"
	printf 'XDG_STATE=%s\n' "$config_state"
	printf 'KANDER_CONFIG=%s\n' "${KANDER_CONFIG-<unset>}"
	printf 'GOCACHE=%s\n' "$GOCACHE"
	printf 'GOMODCACHE=%s\n' "$GOMODCACHE"
	printf 'GOENV=%s\n' "$GOENV"
	printf -- '--\n'
} >> "$FAKE_GO_LOG"
if [ "$1" = test ] && [ "${FAKE_GO_FAIL-}" = test ]; then exit 1; fi
exit 0
`

const fakeGh = `#!/bin/sh
case "$1" in
auth) exit 0 ;;
release) printf '0\n'; exit 0 ;;
esac
exit 1
`

func newGateRig(t *testing.T) *gateRig {
	t.Helper()
	root := t.TempDir()
	rig := &gateRig{
		work:   filepath.Join(root, "work"),
		origin: filepath.Join(root, "origin.git"),
		tmpdir: filepath.Join(root, "tmp"),
		goLog:  filepath.Join(root, "go.log"),
	}
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{rig.tmpdir, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"go": fakeGo, "gh": fakeGh} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(rig.goLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	rig.callerEnv = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "caller-home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "caller-config"),
		"KANDER_CONFIG=" + filepath.Join(root, "caller-config.json"),
		"TMPDIR=" + rig.tmpdir,
		"FAKE_GOCACHE=/caller/gocache",
		"FAKE_GOMODCACHE=/caller/gomodcache",
		"FAKE_GOENV=/caller/goenv",
		"FAKE_GO_LOG=" + rig.goLog,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	}

	rig.git(t, root, "init", "-q", "--bare", rig.origin)
	rig.git(t, root, "-C", rig.origin, "symbolic-ref", "HEAD", "refs/heads/main")
	rig.git(t, root, "clone", "-q", rig.origin, rig.work)
	rig.git(t, rig.work, "checkout", "-q", "-b", "main")
	changelog := "# Changelog\n\n## " + gateVersion + " — 2026-01-01\n\n- first\n"
	if err := os.WriteFile(filepath.Join(rig.work, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}
	rig.git(t, rig.work, "add", "CHANGELOG.md")
	rig.git(t, rig.work, "commit", "-q", "-m", "changelog")
	rig.git(t, rig.work, "push", "-q", "origin", "main")
	return rig
}

func (r *gateRig) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = r.callerEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// release runs release.sh in the work tree with the rig's environment plus
// extra, and returns the exit code and stderr. --skip-tap ends the script
// right after the tag is pushed and the fake gh reports the release assets.
func (r *gateRig) release(t *testing.T, extra []string, args ...string) (int, string) {
	t.Helper()
	script, err := filepath.Abs("release.sh")
	if err != nil {
		t.Fatalf("resolve release.sh: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script, "--skip-tap"}, args...)...)
	cmd.Dir = r.work
	cmd.Env = append(append([]string{}, r.callerEnv...), extra...)
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

// gateCalls parses the fake go log into one field map per recorded call.
func (r *gateRig) gateCalls(t *testing.T) []map[string]string {
	t.Helper()
	data, err := os.ReadFile(r.goLog)
	if err != nil {
		t.Fatal(err)
	}
	var calls []map[string]string
	for _, record := range strings.Split(string(data), "--\n") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := map[string]string{}
		for _, line := range strings.Split(strings.TrimRight(record, "\n"), "\n") {
			name, value, _ := strings.Cut(line, "=")
			fields[name] = value
		}
		calls = append(calls, fields)
	}
	return calls
}

func (r *gateRig) callerValue(name string) string {
	for _, kv := range r.callerEnv {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value
		}
	}
	return ""
}

func (r *gateRig) localTags(t *testing.T) string {
	return strings.TrimSpace(r.git(t, r.work, "tag", "--list"))
}

func (r *gateRig) originTags(t *testing.T) string {
	return strings.TrimSpace(r.git(t, r.work, "ls-remote", "--tags", r.origin))
}

func (r *gateRig) requireNoLeftovers(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(r.tmpdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("release.sh left %s in TMPDIR", entry.Name())
	}
}

// TestReleaseLocalGate covers the local go vet / go test gate that runs between
// the preflight and the tag. The gate must mirror the Release workflow's runner:
// a 30-minute package timeout, an empty HOME and XDG_CONFIG_HOME, no
// KANDER_CONFIG, and the caller's Go caches.
func TestReleaseLocalGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release.sh is a maintainer bash script; not run on Windows")
	}
	for _, tool := range []string{"bash", "git", "awk", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("release.sh needs %s, which is not on PATH", tool)
		}
	}

	t.Run("passing gate runs in a clean environment and tags", func(t *testing.T) {
		rig := newGateRig(t)
		code, stderr := rig.release(t, nil, gateVersion)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		calls := rig.gateCalls(t)
		if len(calls) != 2 {
			t.Fatalf("fake go recorded %d calls, want 2 (vet, test): %v", len(calls), calls)
		}
		if got := calls[0]["args"]; got != "vet ./..." {
			t.Errorf("first go call = %q, want %q", got, "vet ./...")
		}
		if got := calls[1]["args"]; got != "test -timeout 30m ./..." {
			t.Errorf("second go call = %q, want %q", got, "test -timeout 30m ./...")
		}
		callerHome := rig.callerValue("HOME")
		callerConfig := rig.callerValue("XDG_CONFIG_HOME")
		for _, call := range calls {
			if call["HOME"] == "" || call["HOME"] == callerHome {
				t.Errorf("%s: HOME = %q, want a new directory different from %q", call["args"], call["HOME"], callerHome)
			}
			if call["XDG_CONFIG_HOME"] == "" || call["XDG_CONFIG_HOME"] == callerConfig {
				t.Errorf("%s: XDG_CONFIG_HOME = %q, want a new directory different from %q", call["args"], call["XDG_CONFIG_HOME"], callerConfig)
			}
			if call["HOME"] == call["XDG_CONFIG_HOME"] {
				t.Errorf("%s: HOME and XDG_CONFIG_HOME are the same directory %q", call["args"], call["HOME"])
			}
			if call["HOME_STATE"] != "empty" || call["XDG_STATE"] != "empty" {
				t.Errorf("%s: HOME is %s and XDG_CONFIG_HOME is %s, want both empty", call["args"], call["HOME_STATE"], call["XDG_STATE"])
			}
			if call["KANDER_CONFIG"] != "<unset>" {
				t.Errorf("%s: KANDER_CONFIG = %q, want it unset", call["args"], call["KANDER_CONFIG"])
			}
			for name, want := range map[string]string{
				"GOCACHE":    "/caller/gocache",
				"GOMODCACHE": "/caller/gomodcache",
				"GOENV":      "/caller/goenv",
			} {
				if call[name] != want {
					t.Errorf("%s: %s = %q, want the caller's %q", call["args"], name, call[name], want)
				}
			}
			for _, dir := range []string{call["HOME"], call["XDG_CONFIG_HOME"]} {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Errorf("gate directory %s still exists after release.sh exited (stat err: %v)", dir, err)
				}
			}
		}
		if got := rig.localTags(t); got != gateVersion {
			t.Errorf("local tags = %q, want %q", got, gateVersion)
		}
		if !strings.Contains(rig.originTags(t), "refs/tags/"+gateVersion) {
			t.Errorf("origin lacks tag %s", gateVersion)
		}
		rig.requireNoLeftovers(t)
	})

	t.Run("failing gate stops before the tag", func(t *testing.T) {
		rig := newGateRig(t)
		code, stderr := rig.release(t, []string{"FAKE_GO_FAIL=test"}, gateVersion)
		if code == 0 {
			t.Fatalf("exit code = 0, want nonzero; stderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, "gate failed; no tag was created") {
			t.Errorf("stderr lacks the gate failure notice:\n%s", stderr)
		}
		if got := rig.localTags(t); got != "" {
			t.Errorf("local tags = %q, want none", got)
		}
		if got := rig.originTags(t); got != "" {
			t.Errorf("origin tags = %q, want none", got)
		}
		calls := rig.gateCalls(t)
		for _, call := range calls {
			for _, dir := range []string{call["HOME"], call["XDG_CONFIG_HOME"]} {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Errorf("gate directory %s still exists after the failed gate (stat err: %v)", dir, err)
				}
			}
		}
		if len(calls) != 2 {
			t.Errorf("fake go recorded %d calls, want 2 (vet, failing test)", len(calls))
		}
		rig.requireNoLeftovers(t)
	})

	t.Run("no-test skips the gate and still tags", func(t *testing.T) {
		rig := newGateRig(t)
		code, stderr := rig.release(t, []string{"FAKE_GO_FAIL=test"}, "--no-test", gateVersion)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if calls := rig.gateCalls(t); len(calls) != 0 {
			t.Errorf("fake go recorded %d calls with --no-test, want none: %v", len(calls), calls)
		}
		if got := rig.localTags(t); got != gateVersion {
			t.Errorf("local tags = %q, want %q", got, gateVersion)
		}
		rig.requireNoLeftovers(t)
	})
}
