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

const (
	modelScopeWorkflow = ".github/workflows/w.yml"
	modelScopeZeros    = "0000000000000000000000000000000000000000"
	modelScopeMissing  = "1111111111111111111111111111111111111111"
)

// modelScopeBase is the base commit of every case: internal/board imports
// internal/dep, which embeds sub/data.txt; its external test imports
// internal/xtest; internal/other is imported by nothing.
var modelScopeBase = map[string]string{
	"go.mod":                           "module example.test/m\n\ngo 1.25\n",
	"go.sum":                           "",
	modelScopeWorkflow:                 "name: w\n",
	"internal/board/board.go":          "package board\n\nimport _ \"example.test/m/internal/dep\"\n",
	"internal/board/board_ext_test.go": "package board_test\n\nimport _ \"example.test/m/internal/xtest\"\n",
	"internal/dep/dep.go":              "package dep\n\nimport _ \"embed\"\n\n//go:embed sub/data.txt\nvar Data string\n",
	"internal/dep/sub/data.txt":        "data\n",
	"internal/xtest/x.go":              "package xtest\n",
	"internal/other/other.go":          "package other\n",
}

// modelScopeSkip skips on Windows, where nothing runs the script: it only runs
// in the ubuntu-latest and macos-latest all jobs and in the Linux model-full
// job. It also skips when a tool the script needs is missing.
func modelScopeSkip(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("model-test-scope.sh runs only on ubuntu, macOS and the Linux model-full job; not run on Windows")
	}
	for _, tool := range []string{"bash", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("model-test-scope.sh needs %s, which is not on PATH", tool)
		}
	}
}

// modelScopeEnv is the environment of every git and script call: no user
// configuration, no network, and the caller's Go caches.
func modelScopeEnv(t *testing.T, home string) []string {
	t.Helper()
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test",
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
		"GOPROXY=off",
	}
	for _, name := range []string{"GOCACHE", "GOMODCACHE", "GOENV", "TMPDIR"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	if os.Getenv("GOCACHE") == "" {
		out, err := exec.Command("go", "env", "GOCACHE").Output()
		if err != nil {
			t.Fatalf("go env GOCACHE: %v", err)
		}
		env = append(env, "GOCACHE="+strings.TrimSpace(string(out)))
	}
	return env
}

func modelScopeGit(t *testing.T, repo string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func modelScopeWrite(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// modelScopeCase is one row of the decision table.
type modelScopeCase struct {
	name       string
	changes    map[string]string // files written in the commit after the base
	event      string
	ref        string // refs/heads/task when empty
	env        func(base string) []string
	originV2   bool // refs/remotes/origin/v2 points at the base commit
	failingGo  bool // a go on PATH that exits 1
	wantRun    string
	wantReason string // substring of the reason, printed and written to the job summary
	wantNote   string // substring of the other lines printed, not in the summary
}

func TestModelScope(t *testing.T) {
	modelScopeSkip(t)
	script, err := os.ReadFile("model-test-scope.sh")
	if err != nil {
		t.Fatal(err)
	}
	other := map[string]string{"internal/other/x.go": "package other\n\nvar X = 1\n"}
	cases := []modelScopeCase{
		{name: "v2 push runs whatever changed", event: "push", ref: "refs/heads/v2", changes: other,
			env:     func(base string) []string { return []string{"PUSH_BEFORE=" + base} },
			wantRun: "true", wantReason: "refs/heads/v2 always runs it", wantNote: "internal/other/x.go"},
		{name: "v2 push with an all-zero before", event: "push", ref: "refs/heads/v2", changes: other,
			env:     func(string) []string { return []string{"PUSH_BEFORE=" + modelScopeZeros} },
			wantRun: "true", wantReason: "refs/heads/v2 always runs it", wantNote: "created the branch"},
		{name: "v2 push with a before that is not in the checkout", event: "push", ref: "refs/heads/v2", changes: other,
			env:     func(string) []string { return []string{"PUSH_BEFORE=" + modelScopeMissing} },
			wantRun: "true", wantReason: "refs/heads/v2 always runs it", wantNote: "not in the checkout"},
		{name: "dispatch skips an unrelated package", event: "workflow_dispatch", changes: other,
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "false", wantReason: "model search skipped: none of the 1 files changed since model_base"},
		{name: "dispatch runs for a dependency", event: "workflow_dispatch",
			changes: map[string]string{"internal/dep/x.go": "package dep\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "internal/dep/x.go"},
		{name: "dispatch runs for an embedded file below a dependency", event: "workflow_dispatch",
			changes: map[string]string{"internal/dep/sub/data.txt": "changed\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "internal/dep/sub/data.txt"},
		{name: "dispatch runs for a dependency of the external test", event: "workflow_dispatch",
			changes: map[string]string{"internal/xtest/x.go": "package xtest\n\nvar X = 1\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "internal/xtest/x.go"},
		{name: "go.mod runs", event: "workflow_dispatch",
			changes: map[string]string{"go.mod": "module example.test/m\n\ngo 1.25\n// changed\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "go.mod (always runs the search)"},
		{name: "go.sum runs", event: "workflow_dispatch",
			changes: map[string]string{"go.sum": "changed\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "go.sum (always runs the search)"},
		{name: "the workflow file runs", event: "workflow_dispatch",
			changes: map[string]string{modelScopeWorkflow: "name: changed\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: modelScopeWorkflow + " (always runs the search)"},
		{name: "the script itself runs", event: "workflow_dispatch",
			changes: map[string]string{"scripts/model-test-scope.sh": string(script) + "# changed\n"},
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "scripts/model-test-scope.sh (always runs the search)"},
		{name: "model_base is not a commit of the checkout", event: "workflow_dispatch", changes: other,
			env:     func(string) []string { return []string{"MODEL_BASE=" + modelScopeMissing} },
			wantRun: "true", wantReason: "is not a commit of this checkout"},
		{name: "model_base is an option", event: "workflow_dispatch", changes: other,
			env:     func(string) []string { return []string{"MODEL_BASE=--output=x"} },
			wantRun: "true", wantReason: "not a full hexadecimal commit ID"},
		{name: "model_base is a revision expression", event: "workflow_dispatch", changes: other,
			env:     func(string) []string { return []string{"MODEL_BASE=HEAD~1"} },
			wantRun: "true", wantReason: "not a full hexadecimal commit ID"},
		{name: "go list fails", event: "workflow_dispatch", changes: other, failingGo: true,
			env:     func(base string) []string { return []string{"MODEL_BASE=" + base} },
			wantRun: "true", wantReason: "go list failed"},
		{name: "a task branch compares with the merge base with origin/v2", event: "push", changes: other, originV2: true,
			wantRun: "false", wantReason: "the merge base with origin/v2"},
		{name: "a task branch with a dependency change runs", event: "push", originV2: true,
			changes: map[string]string{"internal/board/board.go": "package board\n\nimport _ \"example.test/m/internal/dep\"\n\nvar X = 1\n"},
			wantRun: "true", wantReason: "internal/board/board.go"},
		{name: "pull request with a dependency change runs", event: "pull_request",
			changes: map[string]string{"internal/dep/x.go": "package dep\n"},
			env:     func(base string) []string { return []string{"PR_BASE=" + base} },
			wantRun: "true", wantReason: "internal/dep/x.go"},
		{name: "pull request with an unrelated change skips", event: "pull_request", changes: other,
			env:     func(base string) []string { return []string{"PR_BASE=" + base} },
			wantRun: "false", wantReason: "the pull request base"},
		{name: "pull request without a base runs", event: "pull_request", changes: other,
			wantRun: "true", wantReason: "the pull request base  is not a commit of this checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			env := modelScopeEnv(t, home)
			repo := t.TempDir()
			modelScopeGit(t, repo, env, "-c", "init.defaultBranch=main", "init", "-q")
			modelScopeWrite(t, repo, modelScopeBase)
			modelScopeWrite(t, repo, map[string]string{"scripts/model-test-scope.sh": string(script)})
			modelScopeGit(t, repo, env, "add", "-A")
			modelScopeGit(t, repo, env, "commit", "-q", "-m", "base")
			base := modelScopeGit(t, repo, env, "rev-parse", "HEAD")
			if tc.originV2 {
				modelScopeGit(t, repo, env, "update-ref", "refs/remotes/origin/v2", base)
			}
			modelScopeWrite(t, repo, tc.changes)
			modelScopeGit(t, repo, env, "add", "-A")
			modelScopeGit(t, repo, env, "commit", "-q", "-m", "change")

			out := t.TempDir()
			outputFile := filepath.Join(out, "output")
			summaryFile := filepath.Join(out, "summary")
			runEnv := append([]string{}, env...)
			if tc.failingGo {
				fakeBin := t.TempDir()
				fake := "#!/bin/sh\necho 'fake go: refusing' >&2\nexit 1\n"
				if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(fake), 0o755); err != nil {
					t.Fatal(err)
				}
				runEnv = append(runEnv, "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			ref := tc.ref
			if ref == "" {
				ref = "refs/heads/task"
			}
			runEnv = append(runEnv,
				"EVENT="+tc.event, "REF="+ref, "FULL_REFS=refs/heads/v2",
				"WORKFLOW_FILE="+modelScopeWorkflow,
				"GITHUB_OUTPUT="+outputFile, "GITHUB_STEP_SUMMARY="+summaryFile)
			if tc.env != nil {
				runEnv = append(runEnv, tc.env(base)...)
			}
			cmd := exec.Command("bash", "scripts/model-test-scope.sh")
			cmd.Dir = repo
			cmd.Env = runEnv
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", exitErr.ExitCode(), stdout.String(), stderr.String())
				}
				t.Fatal(err)
			}
			output, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("read GITHUB_OUTPUT: %v\nstdout:\n%s", err, stdout.String())
			}
			if got, want := string(output), "run="+tc.wantRun+"\n"; got != want {
				t.Errorf("GITHUB_OUTPUT = %q, want %q\nstdout:\n%s", got, want, stdout.String())
			}
			if !strings.Contains(stdout.String(), tc.wantReason) {
				t.Errorf("stdout lacks %q:\n%s", tc.wantReason, stdout.String())
			}
			if !strings.Contains(stdout.String(), tc.wantNote) {
				t.Errorf("stdout lacks %q:\n%s", tc.wantNote, stdout.String())
			}
			summary, err := os.ReadFile(summaryFile)
			if err != nil || !strings.Contains(string(summary), tc.wantReason) {
				t.Errorf("job summary lacks %q: %q (%v)", tc.wantReason, summary, err)
			}
			if status := modelScopeGit(t, repo, env, "status", "--porcelain", "--ignored"); status != "" {
				t.Errorf("the script left files in the checkout:\n%s", status)
			}
		})
	}
}
