package scripts

import (
	"bytes"
	"errors"
	"fmt"
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
		code, stderr := runRelease(t, dir, "--skip-tap", "--checksums", checksums, "v2.0.0")
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
		code, stderr := runRelease(t, dir, "--skip-tap", "--checksums", missing, "v2.0.0")
		if code == 0 {
			t.Fatalf("exit code = 0, want nonzero; stderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, "no such checksums file") {
			t.Fatalf("stderr lacks the missing-file error:\n%s", stderr)
		}
	})
}

// gateRig is a release.sh fixture that reaches the release gate check: a bare
// repository as origin, a work tree on a release branch whose HEAD equals the
// origin branch and whose CHANGELOG.md names the version, and a fake gh at the
// front of PATH. Nothing here touches the real HOME, network, or repository tags.
type gateRig struct {
	work      string   // work tree release.sh runs in
	origin    string   // bare repository acting as origin
	tmpdir    string   // TMPDIR of the script
	ghLog     string   // file the fake gh appends one line per `gh run` call to
	callerEnv []string // environment of the script, minus per-test additions
}

const (
	gateVersion       = "v2.0.0"
	gatePrerelease    = "v2.0.0-beta.1"
	prereleaseBranch  = "v2"
	tapSkippedNotice  = "prereleases never reach the tap"
	tapChecksumsStep  = "reading checksums.txt"
	changelogTemplate = "# Changelog\n\n## %s — 2026-01-01\n\n- first\n"
	// gatePassed is the FAKE_GATE_RUNS value of one successful gate run on the
	// commit release.sh asks about.
	gatePassed = "101\t@HEAD@\tcompleted\tsuccess"
)

// fakeGh answers the gh calls release.sh makes. `gh run list` is logged to
// FAKE_GH_LOG and prints FAKE_GATE_RUNS, the tab-separated lines the real gh
// prints for the script's --jq projection, with @HEAD@ replaced by the value
// of --commit. FAKE_GH_RUN_LIST_FAIL makes `gh run list` fail.
const fakeGh = `#!/bin/sh
case "$1" in
auth) exit 0 ;;
release) printf '0\n'; exit 0 ;;
run)
	printf '%s\n' "$*" >> "$FAKE_GH_LOG"
	[ "$2" = list ] || exit 1
	[ -z "${FAKE_GH_RUN_LIST_FAIL-}" ] || exit 1
	commit=
	while [ "$#" -gt 0 ]; do
		if [ "$1" = --commit ]; then commit=$2; fi
		shift
	done
	if [ -n "${FAKE_GATE_RUNS-}" ]; then
		printf '%s\n' "$FAKE_GATE_RUNS" | sed "s/@HEAD@/$commit/g"
	fi
	exit 0
	;;
esac
exit 1
`

// newGateRig builds the release fixture on main with gateVersion at the top of
// CHANGELOG.md.
func newGateRig(t *testing.T) *gateRig {
	t.Helper()
	return newGateRigOn(t, "main", gateVersion)
}

// newGateRigOn builds the release fixture on branch, with changelogVersion at
// the top of CHANGELOG.md.
func newGateRigOn(t *testing.T, branch, changelogVersion string) *gateRig {
	t.Helper()
	root := t.TempDir()
	rig := &gateRig{
		work:   filepath.Join(root, "work"),
		origin: filepath.Join(root, "origin.git"),
		tmpdir: filepath.Join(root, "tmp"),
		ghLog:  filepath.Join(root, "gh.log"),
	}
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{rig.tmpdir, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rig.ghLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	rig.callerEnv = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "caller-home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "caller-config"),
		"KANDER_CONFIG=" + filepath.Join(root, "caller-config.json"),
		"TMPDIR=" + rig.tmpdir,
		"FAKE_GH_LOG=" + rig.ghLog,
		"FAKE_GATE_RUNS=" + gatePassed,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	}

	rig.git(t, root, "init", "-q", "--bare", rig.origin)
	rig.git(t, root, "-C", rig.origin, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	rig.git(t, root, "clone", "-q", rig.origin, rig.work)
	rig.git(t, rig.work, "checkout", "-q", "-b", branch)
	changelog := fmt.Sprintf(changelogTemplate, changelogVersion)
	if err := os.WriteFile(filepath.Join(rig.work, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}
	rig.git(t, rig.work, "add", "CHANGELOG.md")
	rig.git(t, rig.work, "commit", "-q", "-m", "changelog")
	rig.git(t, rig.work, "push", "-q", "origin", branch)
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
	return r.run(t, extra, append([]string{"--skip-tap"}, args...)...)
}

// run is release without the implied --skip-tap. The fake gh cannot serve
// checksums.txt, so a run that reaches the tap step fails.
func (r *gateRig) run(t *testing.T, extra []string, args ...string) (int, string) {
	t.Helper()
	script, err := filepath.Abs("release.sh")
	if err != nil {
		t.Fatalf("resolve release.sh: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
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

// runListCalls returns the arguments of every `gh run list` call, in order.
func (r *gateRig) runListCalls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(r.ghLog)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "run list") {
			calls = append(calls, strings.TrimSpace(line))
		}
	}
	return calls
}

// head returns the commit of the work tree's HEAD.
func (r *gateRig) head(t *testing.T) string {
	return strings.TrimSpace(r.git(t, r.work, "rev-parse", "HEAD"))
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

// TestReleaseGate covers the release gate check that runs between the
// preflight and the tag: the script tags only when release-gate.yml has a
// completed, successful run on exactly the commit to tag.
func TestReleaseGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release.sh is a maintainer bash script; not run on Windows")
	}
	for _, tool := range []string{"bash", "git", "awk", "sh", "sed"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("release.sh needs %s, which is not on PATH", tool)
		}
	}

	for _, tc := range []struct {
		name string
		runs string // FAKE_GATE_RUNS
		run  string // the run ID the script must name
	}{
		{name: "a successful run on the commit tags", runs: gatePassed, run: "101"},
		{name: "a success after a failure on the commit tags", runs: "100\t@HEAD@\tcompleted\tfailure\n102\t@HEAD@\tcompleted\tsuccess", run: "102"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newGateRig(t)
			code, stderr := rig.release(t, []string{"FAKE_GATE_RUNS=" + tc.runs}, gateVersion)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
			}
			calls := rig.runListCalls(t)
			want := "--repo dualface/kander --workflow release-gate.yml --commit " + rig.head(t)
			if len(calls) != 1 || !strings.Contains(calls[0], want) {
				t.Errorf("gh run list calls = %q, want one with %q", calls, want)
			}
			if !strings.Contains(stderr, "release gate passed: https://github.com/dualface/kander/actions/runs/"+tc.run+"\n") {
				t.Errorf("stderr does not name run %s:\n%s", tc.run, stderr)
			}
			if got := rig.localTags(t); got != gateVersion {
				t.Errorf("local tags = %q, want %q", got, gateVersion)
			}
			if !strings.Contains(rig.originTags(t), "refs/tags/"+gateVersion) {
				t.Errorf("origin lacks tag %s", gateVersion)
			}
			rig.requireNoLeftovers(t)
		})
	}

	for _, tc := range []struct {
		name   string
		env    []string
		notice string
	}{
		{name: "no run", env: []string{"FAKE_GATE_RUNS="}, notice: "no successful release-gate.yml run on"},
		{name: "a failed run", env: []string{"FAKE_GATE_RUNS=101\t@HEAD@\tcompleted\tfailure"}, notice: "no successful release-gate.yml run on"},
		{name: "a cancelled run", env: []string{"FAKE_GATE_RUNS=101\t@HEAD@\tcompleted\tcancelled"}, notice: "no successful release-gate.yml run on"},
		{name: "an unfinished run", env: []string{"FAKE_GATE_RUNS=101\t@HEAD@\tin_progress\t"}, notice: "no successful release-gate.yml run on"},
		{name: "a success on another commit", env: []string{"FAKE_GATE_RUNS=101\t0123456789abcdef0123456789abcdef01234567\tcompleted\tsuccess"}, notice: "no successful release-gate.yml run on"},
		{name: "a failed query", env: []string{"FAKE_GH_RUN_LIST_FAIL=1"}, notice: "could not query the release gate runs of"},
	} {
		t.Run(tc.name+" stops before the tag", func(t *testing.T) {
			rig := newGateRig(t)
			code, stderr := rig.release(t, tc.env, gateVersion)
			if code == 0 {
				t.Fatalf("exit code = 0, want nonzero; stderr:\n%s", stderr)
			}
			for _, want := range []string{tc.notice, "no tag was created", "gh workflow run release-gate.yml --repo dualface/kander --ref main"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			if got := rig.localTags(t); got != "" {
				t.Errorf("local tags = %q, want none", got)
			}
			if got := rig.originTags(t); got != "" {
				t.Errorf("origin tags = %q, want none", got)
			}
			rig.requireNoLeftovers(t)
		})
	}

	t.Run("no-test skips the check and still tags", func(t *testing.T) {
		rig := newGateRig(t)
		code, stderr := rig.release(t, []string{"FAKE_GH_RUN_LIST_FAIL=1"}, "--no-test", gateVersion)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if calls := rig.runListCalls(t); len(calls) != 0 {
			t.Errorf("gh run list ran %d times with --no-test, want none: %q", len(calls), calls)
		}
		if !strings.Contains(stderr, "skipping the release gate check (--no-test)") {
			t.Errorf("stderr lacks the skip notice:\n%s", stderr)
		}
		if got := rig.localTags(t); got != gateVersion {
			t.Errorf("local tags = %q, want %q", got, gateVersion)
		}
		rig.requireNoLeftovers(t)
	})
}

// TestReleaseVersionChannels covers the 2.x version rules: the script accepts
// only v2.Y.Z and v2.Y.Z-(alpha|beta|rc).N, cuts a prerelease from the v2
// branch only, and never lets a prerelease reach the Homebrew tap.
func TestReleaseVersionChannels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release.sh is a maintainer bash script; not run on Windows")
	}
	for _, tool := range []string{"bash", "git", "awk", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("release.sh needs %s, which is not on PATH", tool)
		}
	}

	t.Run("versions outside 2.x are rejected", func(t *testing.T) {
		rig := newGateRig(t)
		for _, bad := range []string{"v1.6.4", "v3.0.0", "v2.0.0-dev", "v2.0.0-beta", "v2.0.0-beta.01", "v2.0.0-preview.1", "2.0.0", "v2.0.0-beta.1\n", "v2.0.0\nv2.0.0"} {
			code, stderr := rig.run(t, nil, bad)
			if code == 0 || !strings.Contains(stderr, "version must look like v2.0.0 or v2.0.0-beta.1") {
				t.Errorf("%s: exit code = %d; stderr:\n%s", bad, code, stderr)
			}
		}
		if got := rig.originTags(t); got != "" {
			t.Errorf("origin tags = %q, want none", got)
		}
	})

	t.Run("prerelease outside the v2 branch is rejected", func(t *testing.T) {
		rig := newGateRigOn(t, "main", gatePrerelease)
		code, stderr := rig.run(t, nil, "--no-test", gatePrerelease)
		if code == 0 || !strings.Contains(stderr, "releases are cut from v2, current branch is main") {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
		}
		if got := rig.originTags(t); got != "" {
			t.Errorf("origin tags = %q, want none", got)
		}
	})

	t.Run("prerelease changelog error points at the v2 branch", func(t *testing.T) {
		rig := newGateRigOn(t, prereleaseBranch, "v2.0.0-beta.0")
		code, stderr := rig.run(t, nil, "--no-test", gatePrerelease)
		if code == 0 || !strings.Contains(stderr, "commit it on v2, and push v2") || strings.Contains(stderr, "develop") {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
		}
	})

	t.Run("prerelease on v2 tags and skips the tap", func(t *testing.T) {
		rig := newGateRigOn(t, prereleaseBranch, gatePrerelease)
		code, stderr := rig.run(t, nil, gatePrerelease)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr)
		}
		if calls := rig.runListCalls(t); len(calls) != 1 || !strings.Contains(calls[0], "--commit "+rig.head(t)) {
			t.Errorf("gh run list calls = %q, want one for the commit to tag", calls)
		}
		if !strings.Contains(stderr, tapSkippedNotice) || strings.Contains(stderr, tapChecksumsStep) {
			t.Errorf("prerelease reached the tap step; stderr:\n%s", stderr)
		}
		if got := rig.localTags(t); got != gatePrerelease {
			t.Errorf("local tags = %q, want %q", got, gatePrerelease)
		}
		if !strings.Contains(rig.originTags(t), "refs/tags/"+gatePrerelease) {
			t.Errorf("origin lacks tag %s", gatePrerelease)
		}
		rig.requireNoLeftovers(t)
	})

	t.Run("skip-tag prerelease skips the tap", func(t *testing.T) {
		rig := newGateRigOn(t, prereleaseBranch, gatePrerelease)
		code, stderr := rig.run(t, nil, "--skip-tag", gatePrerelease)
		if code != 0 || !strings.Contains(stderr, tapSkippedNotice) || strings.Contains(stderr, tapChecksumsStep) {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
		}
		if got := rig.originTags(t); got != "" {
			t.Errorf("origin tags = %q, want none with --skip-tag", got)
		}
	})
}
