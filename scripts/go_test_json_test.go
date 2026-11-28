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

// goTestJSONStream is what the fake go prints: two test events, a build-output
// event, an event without output, and a line that is not JSON.
const goTestJSONStream = `{"Action":"output","Package":"p","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Action":"pass","Package":"p","Test":"TestA","Elapsed":1.5}
{"Action":"build-output","ImportPath":"q","Output":"q/x.go:1: syntax error\n"}
go: warning that is not JSON
{"Action":"output","Package":"p","Output":"FAIL\tp\t1.5s\n"}
`

// goTestJSONRun runs go-test-json.sh with a fake go that prints
// goTestJSONStream and exits with goStatus. It returns the script's stdout, its
// exit code, and the JSON file it wrote.
func goTestJSONRun(t *testing.T, goStatus string) (string, int, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("go-test-json.sh runs in every release-gate.yml step on Windows, which fails on a script error; not run here")
	}
	for _, tool := range []string{"bash", "jq", "tee"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("go-test-json.sh needs %s, which is not on PATH", tool)
		}
	}
	fakeBin := t.TempDir()
	stream := filepath.Join(fakeBin, "stream.txt")
	if err := os.WriteFile(stream, []byte(goTestJSONStream), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\n[ \"$1 $2\" = 'test -json' ] || { echo \"fake go: unexpected arguments $*\" >&2; exit 99; }\ncat '" + stream + "'\nexit " + goStatus + "\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(t.TempDir(), "nested", "out.json")
	cmd := exec.Command("bash", filepath.Join(platformPackagesRoot(t), "scripts", "go-test-json.sh"), jsonFile, "-count=1", "./...")
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run go-test-json.sh: %v", err)
		}
		code = exitErr.ExitCode()
	}
	if strings.Contains(stderr.String(), "fake go: unexpected arguments") {
		t.Fatalf("go-test-json.sh called go with unexpected arguments: %s", stderr.String())
	}
	saved, err := os.ReadFile(jsonFile)
	if err != nil {
		t.Fatalf("read the JSON file: %v", err)
	}
	return stdout.String(), code, string(saved)
}

// TestGoTestJSONKeepsTheStreamAndPrintsTheOutput checks that the script saves
// the go test stream unchanged and prints only the test output and lines that
// are not JSON.
func TestGoTestJSONKeepsTheStreamAndPrintsTheOutput(t *testing.T) {
	stdout, code, saved := goTestJSONRun(t, "0")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if saved != goTestJSONStream {
		t.Errorf("saved stream:\n%s\nwant:\n%s", saved, goTestJSONStream)
	}
	want := "=== RUN   TestA\nq/x.go:1: syntax error\ngo: warning that is not JSON\nFAIL\tp\t1.5s\n"
	if stdout != want {
		t.Errorf("printed output:\n%q\nwant:\n%q", stdout, want)
	}
}

// TestGoTestJSONKeepsTheStatusOfGoTest checks that a failing go test fails the
// script with the same status.
func TestGoTestJSONKeepsTheStatusOfGoTest(t *testing.T) {
	_, code, _ := goTestJSONRun(t, "3")
	if code != 3 {
		t.Errorf("exit %d, want 3, the status of go test", code)
	}
}
