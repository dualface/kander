package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

// gcCommandEnv points the command at target and gives it a terminal answering
// answer; an empty answer means no terminal.
func gcCommandEnv(t *testing.T, target, answer string) *bytes.Buffer {
	t.Helper()
	prompt := &bytes.Buffer{}
	previousURL, previousTerminal, previousIn, previousOut := gcServerURL, gcTerminal, gcPromptIn, gcPromptOut
	t.Cleanup(func() {
		gcServerURL, gcTerminal, gcPromptIn, gcPromptOut = previousURL, previousTerminal, previousIn, previousOut
	})
	gcServerURL = func() (string, error) { return target, nil }
	gcTerminal = func() bool { return answer != "" }
	gcPromptIn, gcPromptOut = strings.NewReader(strings.TrimPrefix(answer, "tty:")), prompt
	return prompt
}

// offlineData creates a stopped server's data directory with one project and a log
// larger than 1 MiB.
func offlineData(t *testing.T) (dir string, log []byte) {
	t.Helper()
	dir = t.TempDir()
	s, err := Open(Options{DataDir: dir, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.register(api.RegisterProject{Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	log = bytes.Repeat([]byte("{\"line\":\"x\"}\n"), (3<<20)/13)
	if err := os.WriteFile(filepath.Join(dir, "server.log"), log, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, log
}

// unusedTarget is a loopback URL nothing listens on.
const unusedTarget = "http://127.0.0.1:1"

func runGCCommand(args ...string) (int, string, string) {
	return commandtest.Capture(func() int { return commandtest.Run(NewGCCommand(), args...) })
}

func TestServerGCCommandNeedsConfirmation(t *testing.T) {
	dir, log := offlineData(t)
	before := dataFiles(t, dir)
	unchanged := func(label string) {
		t.Helper()
		after := dataFiles(t, dir)
		delete(after, filepath.Join(dir, "server.lock"))
		delete(before, filepath.Join(dir, "server.lock"))
		if !mapsEqual(before, after) {
			t.Fatalf("%s: no data file may change", label)
		}
	}
	gcCommandEnv(t, unusedTarget, "tty:y\n")
	code, out, errOut := runGCCommand("--data", dir, "--json")
	if code != 2 || !strings.Contains(out+errOut, "interactive_required") {
		t.Fatalf("--json without --yes or --dry-run must be refused with interactive_required: %d %s %s", code, out, errOut)
	}
	unchanged("--json")
	gcCommandEnv(t, unusedTarget, "")
	if code, _, _ := runGCCommand("--data", dir); code != 2 {
		t.Fatalf("without a terminal and without --yes: exit %d, want 2", code)
	}
	unchanged("no terminal")
	for _, answer := range []string{"tty:n\n", "tty:\n"} {
		prompt := gcCommandEnv(t, unusedTarget, answer)
		code, stdout, stderr := runGCCommand("--data", dir, "--log-keep", "1MiB")
		if code != 1 || !strings.Contains(prompt.String(), "[y/N]") || !strings.Contains(stdout, "server.log") || strings.Contains(stdout, "[y/N]") {
			t.Fatalf("answer %q: exit %d, prompt %q, stdout %q, stderr %q", answer, code, prompt.String(), stdout, stderr)
		}
		unchanged("cancelled")
	}
	prompt := gcCommandEnv(t, unusedTarget, "tty:y\n")
	if code, stdout, stderr := runGCCommand("--data", dir, "--log-keep", "1MiB"); code != 0 || !strings.Contains(prompt.String(), "[y/N]") {
		t.Fatalf("confirmed gc: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, "server.log")); len(kept) >= len(log) || len(kept) > 1<<20 {
		t.Fatalf("a confirmed gc must trim server.log to 1 MiB, it has %d bytes", len(kept))
	}
}

func TestServerGCCommandRefusesInvalidValues(t *testing.T) {
	dir, _ := offlineData(t)
	gcCommandEnv(t, unusedTarget, "")
	for _, args := range [][]string{
		{"--older-than", "30m"}, {"--older-than", "7"}, {"--log-keep", "1KiB"}, {"--log-keep", "64MB"},
		{"--project", "bad name"}, {"--project", "unknown"},
	} {
		code, stdout, stderr := runGCCommand(append([]string{"--data", dir, "--dry-run"}, args...)...)
		if code != 2 {
			t.Fatalf("%v: exit %d, want 2\n%s\n%s", args, code, stdout, stderr)
		}
	}
}

func TestServerGCCommandRunsOfflineUnderTheLock(t *testing.T) {
	dir, log := offlineData(t)
	gcCommandEnv(t, unusedTarget, "")
	code, stdout, stderr := runGCCommand("--data", dir, "--dry-run", "--json", "--log-keep", "1MiB")
	var plan api.ServerGCReport
	if code != 0 || json.Unmarshal([]byte(stdout), &plan) != nil || !plan.DryRun || plan.Log == nil || plan.Log.Bytes == 0 || len(plan.Projects) != 1 {
		t.Fatalf("offline dry run: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, "server.log")); !bytes.Equal(kept, log) {
		t.Fatal("a dry run must not trim the log")
	}
	code, stdout, stderr = runGCCommand("--data", dir, "--yes", "--json", "--log-keep", "1MiB")
	var result api.ServerGCReport
	if code != 0 || json.Unmarshal([]byte(stdout), &result) != nil || result.DryRun || result.Log.Bytes != plan.Log.Bytes {
		t.Fatalf("offline run: exit %d\n%s\n%s", code, stdout, stderr)
	}
	kept, _ := os.ReadFile(filepath.Join(dir, "server.log"))
	if !bytes.Equal(kept, log[plan.Log.Bytes:]) {
		t.Fatal("the offline run must keep the planned tail")
	}
}

func TestServerGCCommandUsesTheRunningServer(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	gcCommandEnv(t, f.http.URL, "")
	var hits int
	defer func(previous func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error)) {
		gcCompact = previous
	}(gcCompact)
	gcCompact = func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		hits++
		return board.CompactionReport{}, nil
	}
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--yes", "--json")
	var result api.ServerGCReport
	if code != 0 || json.Unmarshal([]byte(stdout), &result) != nil || result.DryRun || hits != 2 {
		t.Fatalf("online gc: exit %d, compaction calls %d\n%s\n%s", code, hits, stdout, stderr)
	}
	code, _, _ = runGCCommand("--data", f.s.data, "--yes", "--project", "unknown")
	if code != 2 {
		t.Fatalf("an unregistered project: exit %d, want 2", code)
	}
	f.s.gcBusy.Store(true)
	code, stdout, stderr = runGCCommand("--data", f.s.data, "--yes", "--json")
	f.s.gcBusy.Store(false)
	if code != 2 || !strings.Contains(stdout+stderr, api.CodeGCRunning) {
		t.Fatalf("a second gc: exit %d, want 2 with gc_running\n%s\n%s", code, stdout, stderr)
	}
	gcCompact = func(context.Context, string, board.CompactionOptions) (board.CompactionReport, error) {
		return board.CompactionReport{Skipped: []board.SkippedRun{{RunID: "r", Conditions: []board.CompactionCondition{board.CompactionRemoval}, Reason: "removal failed"}}}, nil
	}
	code, stdout, _ = runGCCommand("--data", f.s.data, "--yes")
	if code != 1 || !strings.Contains(stdout, "removal failed") {
		t.Fatalf("a kept candidate: exit %d, want 1 with the reason\n%s", code, stdout)
	}
}
