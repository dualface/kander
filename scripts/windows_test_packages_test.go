package scripts

import (
	"bytes"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// windowsPackagesOtherSteps are the packages a Windows step other than the
// services step runs: the board step and the remote step of
// distributed-checks.yml. The services list leaves them out by exact match.
var windowsPackagesOtherSteps = []string{"internal/board", "internal/remote"}

// windowsPackagesImportsRefused are the imports a Linux-only package never has.
var windowsPackagesImportsRefused = []string{"os/exec", "syscall", "net", "net/http/httptest"}

// windowsPackagesSkip skips on Windows, where the Windows CI step that runs
// windows-test-packages.sh exposes any failure of the script itself, and where
// bash is not guaranteed to be the one the script needs. It also skips when a
// tool the script needs is missing.
func windowsPackagesSkip(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows-test-packages.sh runs in its own Windows CI step, which fails on a script error; not run here")
	}
	for _, tool := range []string{"bash", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("windows-test-packages.sh needs %s, which is not on PATH", tool)
		}
	}
}

// windowsPackagesRoot returns the repository root; go test runs in scripts/.
func windowsPackagesRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}
	return root
}

// windowsPackagesRun runs the real script from the repository root with extra
// environment entries and returns stdout lines, the exit code and stderr.
func windowsPackagesRun(t *testing.T, env []string, args ...string) ([]string, int, string) {
	t.Helper()
	root := windowsPackagesRoot(t)
	cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", "windows-test-packages.sh")}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run windows-test-packages.sh: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return strings.Fields(stdout.String()), code, stderr.String()
}

// windowsPackagesGOOS are the platforms whose package lists the script must
// cover: a package whose files all carry a Windows constraint is invisible to
// go list on Linux.
var windowsPackagesGOOS = []string{"linux", "windows"}

// windowsPackagesMust runs the script for one GOOS and requires exit code 0.
func windowsPackagesMust(t *testing.T, goos string, args ...string) []string {
	t.Helper()
	out, code, stderr := windowsPackagesRun(t, []string{"GOOS=" + goos}, args...)
	if code != 0 {
		t.Fatalf("GOOS=%s windows-test-packages.sh %v exited %d: %s", goos, args, code, stderr)
	}
	return out
}

// windowsPackagesGo runs go in dir with extra environment entries.
func windowsPackagesGo(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

// windowsPackagesRel turns an import path into a repository-relative directory.
func windowsPackagesRel(module, importPath string) string {
	return strings.TrimPrefix(importPath, module+"/")
}

// windowsPackagesNameSuffix matches a file name that carries a GOOS or GOARCH
// suffix, or _unix, in any of the forms the go command honors.
func windowsPackagesNameSuffix(t *testing.T, root string) *regexp.Regexp {
	t.Helper()
	seen := map[string]bool{"unix": true}
	for line := range strings.Lines(windowsPackagesGo(t, root, nil, "tool", "dist", "list")) {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(line), "/")
		if !ok {
			t.Fatalf("unexpected go tool dist list line %q", line)
		}
		seen[goos] = true
		seen[goarch] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, regexp.QuoteMeta(name))
	}
	slices.Sort(names)
	alt := strings.Join(names, "|")
	return regexp.MustCompile(`_(` + alt + `)(_(` + alt + `))?(_test)?\.go$`)
}

// TestWindowsPackagesLinuxOnlyMeetTheRule checks every Linux-only package
// against the classification rule written at the top of the script.
func TestWindowsPackagesLinuxOnlyMeetTheRule(t *testing.T) {
	windowsPackagesSkip(t)
	root := windowsPackagesRoot(t)
	module := strings.TrimSpace(windowsPackagesGo(t, root, nil, "list", "-m"))
	suffix := windowsPackagesNameSuffix(t, root)
	// The union over both platforms: a package only Windows sees is listed too.
	listed := map[string]bool{}
	var linuxOnly []string
	for _, goos := range windowsPackagesGOOS {
		for _, importPath := range windowsPackagesMust(t, goos, "--linux-only") {
			rel := windowsPackagesRel(module, importPath)
			if !listed[rel] {
				listed[rel] = true
				linuxOnly = append(linuxOnly, importPath)
			}
		}
	}
	for _, importPath := range linuxOnly {
		rel := windowsPackagesRel(module, importPath)
		t.Run(rel, func(t *testing.T) {
			dir := filepath.Join(root, filepath.FromSlash(rel))
			files, err := filepath.Glob(filepath.Join(dir, "*.go"))
			if err != nil || len(files) == 0 {
				t.Fatalf("no .go files in %s: %v", rel, err)
			}
			for _, file := range files {
				name := filepath.Base(file)
				if suffix.MatchString(name) {
					t.Errorf("%s/%s: the file name carries a GOOS or GOARCH suffix", rel, name)
				}
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				for n, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
						t.Errorf("%s/%s:%d: a build constraint: %s", rel, name, n+1, line)
					}
					if strings.Contains(line, "runtime.GOOS") {
						t.Errorf("%s/%s:%d: runtime.GOOS: %s", rel, name, n+1, strings.TrimSpace(line))
					}
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), file, data, parser.ImportsOnly)
				if err != nil {
					t.Fatal(err)
				}
				for _, spec := range parsed.Imports {
					path, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						t.Fatal(err)
					}
					if slices.Contains(windowsPackagesImportsRefused, path) || strings.HasPrefix(path, "golang.org/x/sys") {
						t.Errorf("%s/%s: imports %s", rel, name, path)
					}
				}
			}
			// The Windows import graph is the one that matters for this rule.
			deps := windowsPackagesGo(t, root, []string{"GOOS=windows"},
				"list", "-test", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./"+rel)
			for dep := range strings.FieldsSeq(deps) {
				if dep != module && !strings.HasPrefix(dep, module+"/") {
					continue
				}
				depRel := windowsPackagesRel(module, dep)
				depRel, _, _ = strings.Cut(depRel, " [")
				depRel = strings.TrimSuffix(strings.TrimSuffix(depRel, ".test"), "_test")
				if !listed[depRel] {
					t.Errorf("%s: its test build depends on %s, which is not Linux-only", rel, depRel)
				}
			}
		})
	}
}

// TestWindowsPackagesPartition checks, for the package list of each platform,
// that the Linux-only list, the services list and the packages of the other
// Windows steps are disjoint and together cover go list ./....
func TestWindowsPackagesPartition(t *testing.T) {
	windowsPackagesSkip(t)
	root := windowsPackagesRoot(t)
	module := strings.TrimSpace(windowsPackagesGo(t, root, nil, "list", "-m"))
	var others []string
	for _, rel := range windowsPackagesOtherSteps {
		others = append(others, module+"/"+rel)
	}
	for _, goos := range windowsPackagesGOOS {
		t.Run(goos, func(t *testing.T) {
			all := strings.Fields(windowsPackagesGo(t, root, []string{"GOOS=" + goos}, "list", "./..."))
			linuxOnly := windowsPackagesMust(t, goos, "--linux-only")
			services := windowsPackagesMust(t, goos)
			owner := map[string]string{}
			for group, packages := range map[string][]string{"linux-only": linuxOnly, "services": services, "other steps": others} {
				for _, importPath := range packages {
					if previous, ok := owner[importPath]; ok {
						t.Errorf("%s is in both %s and %s", importPath, previous, group)
					}
					owner[importPath] = group
				}
			}
			for _, importPath := range all {
				if _, ok := owner[importPath]; !ok {
					t.Errorf("%s is in no list", importPath)
				}
			}
			if len(owner) != len(all) {
				t.Errorf("the lists name %d packages, go list ./... has %d", len(owner), len(all))
			}

			// Entries that end in a slash match a prefix and the others match exactly.
			for _, want := range []struct {
				importPath string
				group      string
			}{
				{module + "/internal/statemachine/dispatch", "linux-only"},
				{module + "/internal/version", "linux-only"},
				{module + "/internal/board/boardtest", "services"},
				{module + "/internal/board", "other steps"},
			} {
				if owner[want.importPath] != want.group {
					t.Errorf("%s is in %q, want %q", want.importPath, owner[want.importPath], want.group)
				}
			}
		})
	}
}

// TestWindowsPackagesFailLoudly checks that a failing go list and a bad
// argument end the script with a non-zero status instead of a short list.
func TestWindowsPackagesFailLoudly(t *testing.T) {
	windowsPackagesSkip(t)
	fakeBin := t.TempDir()
	fake := "#!/bin/sh\necho 'fake go: refusing' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	for _, args := range [][]string{nil, {"--linux-only"}} {
		out, code, _ := windowsPackagesRun(t, []string{path}, args...)
		if code == 0 || len(out) != 0 {
			t.Errorf("args %v with a failing go: exit %d, stdout %v, want a non-zero exit and no output", args, code, out)
		}
	}
	if _, code, _ := windowsPackagesRun(t, nil, "--bogus"); code != 2 {
		t.Errorf("an unknown argument exited %d, want 2", code)
	}
	if _, code, _ := windowsPackagesRun(t, nil, "--linux-only", "extra"); code != 2 {
		t.Errorf("an extra argument exited %d, want 2", code)
	}
}
