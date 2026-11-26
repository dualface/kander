//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/cli"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

const subscribeSignalChildArgs = "KANDER_SUBSCRIBE_SIGNAL_CHILD_ARGS"

// TestSubscribeSignalChild is the child process of TestSubscribeSignalExitsZeroInBothModes: it runs
// the real command tree with the arguments in the environment and exits with its status. Without
// that variable it does nothing.
func TestSubscribeSignalChild(t *testing.T) {
	args := os.Getenv(subscribeSignalChildArgs)
	if args == "" {
		t.Skip("child of TestSubscribeSignalExitsZeroInBothModes")
	}
	os.Exit(cli.Run(append([]string{"kander", "--lang", "en"}, strings.Fields(args)...)))
}

// runSubscribeUntilSignal runs `kander subscribe` in a child process, waits until its complete
// snapshot line is on standard output, sends SIGTERM to the child only, and returns the exit
// error (nil for status 0), the standard error text, and the JSON lines. The child is always reaped and killed when
// it does not end in time, so no signal or process outlives the call.
func runSubscribeUntilSignal(t *testing.T, args string) (waitErr error, stderr string, lines []map[string]any) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSubscribeSignalChild$")
	cmd.Env = append(os.Environ(), subscribeSignalChildArgs+"="+args)
	var errText bytes.Buffer
	cmd.Stderr = &errText
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(out)
	first := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		first <- line
	}()
	// Reads of the child's output end when the child ends, so Wait below runs after them.
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var snapshot string
	select {
	case snapshot = <-first:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("%s: no snapshot line in 30s; stderr=%s", args, errText.String())
	}
	if !strings.HasSuffix(snapshot, "\n") {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("%s: the first line is incomplete: %q; stderr=%s", args, snapshot, errText.String())
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	rest := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		rest <- data
	}()
	var tail []byte
	select {
	case tail = <-rest:
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("%s: the child did not end after SIGTERM; stderr=%s", args, errText.String())
	}
	waitErr = cmd.Wait()
	for line := range strings.SplitSeq(strings.TrimSuffix(snapshot+string(tail), "\n"), "\n") {
		var object map[string]any
		if json.Unmarshal([]byte(line), &object) != nil {
			t.Fatalf("%s: line is not a JSON object: %q", args, line)
		}
		lines = append(lines, object)
	}
	return waitErr, errText.String(), lines
}

// A termination signal ends `kander subscribe` with status 0 and the lines already written, with
// and without --follow: the default single-event mode must not change what the signal does. The
// command runs in a child process, so the signal never reaches the test binary; each child gets
// the signal after its complete snapshot line, and the two outputs are compared whole, without
// the values every subscription draws for itself.
func TestSubscribeSignalExitsZeroInBothModes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	cfg := config.DefaultConfig()
	cfg.Language, cfg.AgentLanguage = "en", "en"
	session, _ := commandServiceWithServer(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "subscribe-signal", Title: "Subscribe signal", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	normalized := func(args string) []map[string]any {
		t.Helper()
		waitErr, stderr, lines := runSubscribeUntilSignal(t, args)
		if waitErr != nil {
			t.Fatalf("%s: the child did not exit with status 0: %v; stderr=%q", args, waitErr, stderr)
		}
		// With --json every diagnostic is one object; a notice (such as the server version
		// remark) is fine, an error object is not.
		for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
			var object map[string]any
			if line != "" && (json.Unmarshal([]byte(line), &object) != nil || object["error"] != nil) {
				t.Fatalf("%s: unexpected diagnostic %q", args, line)
			}
		}
		if len(lines) == 0 || lines[0]["event"] != "snapshot" {
			t.Fatalf("%s: lines=%v", args, lines)
		}
		for _, line := range lines {
			delete(line, "subscription_id")
			delete(line, "observed_at")
		}
		return lines
	}
	base := "subscribe --json --refresh 1 --watch " + task.TaskID
	followed := normalized(base + " --follow")
	single := normalized(base)
	if !reflect.DeepEqual(followed, single) {
		t.Fatalf("follow=%v\nsingle=%v", followed, single)
	}
}
