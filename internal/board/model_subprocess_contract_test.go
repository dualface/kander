package board

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const modelProbeEnv = "KANDER_MODEL_SUBPROCESS_PROBE"

// This fixture exercises the process boundary without repeating the search.
func TestModelSubprocessProbe(t *testing.T) {
	mode := os.Getenv(modelProbeEnv)
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	if os.Getenv(modelChildEnv) != "TestModelSubprocessProbe" {
		if deadline, ok := t.Deadline(); ok {
			t.Setenv("KANDER_MODEL_PROBE_DEADLINE", deadline.Format(time.RFC3339Nano))
		}
	}
	if modelSubprocess(t, "TestModelSubprocessProbe") {
		return
	}
	if flag.Lookup("test.testlogfile").Value.String() != "" {
		t.Fatal("child inherited testlog")
	}
	if modelModeBounds() != modelFullBounds {
		t.Fatal("child lost full mode")
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if os.Getenv(key) != os.Getenv("KANDER_MODEL_PROBE_TEMP") {
			t.Fatalf("child lost %s", key)
		}
	}
	_, deadline := t.Deadline()
	wantDeadline := mode != "no-deadline"
	if deadline != wantDeadline {
		t.Fatal("child deadline does not match parent mode")
	}
	if deadline {
		parent, err := time.Parse(time.RFC3339Nano, os.Getenv("KANDER_MODEL_PROBE_DEADLINE"))
		child, _ := t.Deadline()
		if err != nil || !child.Before(parent) {
			t.Fatalf("child deadline %v is not before parent %v: %v", child, parent, err)
		}
	}
	fmt.Println("model child output")
	switch mode {
	case "verdict":
		for _, failure := range modelVerdict([]modelRun{{violations: []string{"model verdict failure"}}}, nil) {
			t.Error(failure)
		}
	case "panic":
		panic("model child panic")
	case "timeout":
		time.Sleep(time.Minute)
	case "stream":
		// The caller releases us only after observing the forwarded output.
		for {
			if _, err := os.Stat(os.Getenv("KANDER_MODEL_PROBE_RELEASE")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func modelProbeCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	timeout := "20s"
	if mode == "no-deadline" {
		timeout = "0"
	} else if mode == "timeout" {
		timeout = "5s"
	}
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestModelSubprocessProbe$", "-test.v=true", "-test.timeout="+timeout, "-test.testlogfile="+filepath.Join(dir, "parent.testlog"))
	cmd.Env = append(os.Environ(), modelChildEnv+"=", modelProbeEnv+"="+mode, modelDepthEnv+"=full", "KANDER_MODEL_PROBE_TEMP="+dir, "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
	return cmd
}

func TestModelSubprocessContract(t *testing.T) {
	for _, mode := range []string{"pass", "no-deadline", "verdict", "panic", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			cmd := modelProbeCommand(t, mode)
			output, err := cmd.CombinedOutput()
			failure := mode == "verdict" || mode == "panic" || mode == "timeout"
			if (err != nil) != failure {
				t.Fatalf("exit: %v\n%s", err, output)
			}
			for _, want := range []string{"model child output", map[string]string{"verdict": "model verdict failure", "panic": "model child panic", "timeout": "test timed out"}[mode]} {
				if want != "" && !strings.Contains(string(output), want) {
					t.Fatalf("missing %q:\n%s", want, output)
				}
			}
			if failure && !strings.Contains(string(output), "model subprocess failed") {
				t.Fatalf("parent did not report child failure:\n%s", output)
			}
		})
	}
}

func TestModelSubprocessStreamsOutput(t *testing.T) {
	cmd := modelProbeCommand(t, "stream")
	release := filepath.Join(t.TempDir(), "release")
	cmd.Env = append(cmd.Env, "KANDER_MODEL_PROBE_RELEASE="+release)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(pipe)
	seen := false
	for scanner.Scan() {
		if scanner.Text() == "model child output" {
			seen = true
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("child output was not forwarded before exit")
	}
}
