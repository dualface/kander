// Package terminaltest provides a fake terminal executable for declarative
// backend tests. The running test binary acts as the fake when the activation
// environment names a script directory; it records every argv and answers with
// the first reply whose argv prefix matches, so no shell or platform-specific
// launcher is involved.
//
// Every test package that uses it must call Main first in TestMain:
//
//	func TestMain(m *testing.M) { terminaltest.Main(); os.Exit(m.Run()) }
package terminaltest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ActivationEnv names the directory holding the replies and the call log.
const ActivationEnv = "KANDER_FAKE_TERMINAL_DIR"

// Reply answers every invocation whose argv starts with Args.
type Reply struct {
	Args   []string      `json:"args"`
	Stdout string        `json:"stdout,omitempty"`
	Stderr string        `json:"stderr,omitempty"`
	Code   int           `json:"code,omitempty"`
	Sleep  time.Duration `json:"sleep,omitempty"`
	// Times limits how often the reply matches; zero is unlimited.
	Times int `json:"times,omitempty"`
}

// Main runs the fake and exits when this process was launched as one.
func Main() {
	dir := os.Getenv(ActivationEnv)
	if dir == "" {
		return
	}
	os.Exit(serve(dir, os.Args[1:]))
}

func serve(dir string, args []string) int {
	line, _ := json.Marshal(args)
	if log, err := os.OpenFile(filepath.Join(dir, "calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_, _ = log.Write(append(line, '\n'))
		_ = log.Close()
	}
	replies := readReplies(dir)
	for index := range replies {
		reply := &replies[index]
		if !hasPrefix(args, reply.Args) || !consume(dir, index, reply.Times) {
			continue
		}
		time.Sleep(reply.Sleep)
		_, _ = os.Stdout.WriteString(reply.Stdout)
		_, _ = os.Stderr.WriteString(reply.Stderr)
		return reply.Code
	}
	return 0
}

func readReplies(dir string) []Reply {
	data, err := os.ReadFile(filepath.Join(dir, "replies.json"))
	if err != nil {
		return nil
	}
	var replies []Reply
	_ = json.Unmarshal(data, &replies)
	return replies
}

func hasPrefix(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for index := range prefix {
		if args[index] != prefix[index] {
			return false
		}
	}
	return true
}

// consume counts the uses of a limited reply in a marker file per use.
func consume(dir string, index, times int) bool {
	if times == 0 {
		return true
	}
	prefix := filepath.Join(dir, "used-"+strconv.Itoa(index)+"-")
	for use := 0; use < times; use++ {
		file, err := os.OpenFile(prefix+strconv.Itoa(use), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return true
		}
	}
	return false
}

// Fake is one configured fake terminal executable.
type Fake struct {
	// Program is the executable to run: the test binary itself.
	Program string
	dir     string
}

// New activates a fake for the test with the given replies.
func New(t *testing.T, replies ...Reply) *Fake {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := &Fake{Program: program, dir: t.TempDir()}
	fake.SetReplies(t, replies...)
	t.Setenv(ActivationEnv, fake.dir)
	return fake
}

// SetReplies replaces the replies of later invocations.
func (f *Fake) SetReplies(t *testing.T, replies ...Reply) {
	t.Helper()
	data, err := json.Marshal(replies)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "replies.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(f.dir, "used-*"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

// Calls returns the argv of every invocation so far, and clears the log.
func (f *Fake) Calls(t *testing.T) [][]string {
	t.Helper()
	path := filepath.Join(f.dir, "calls.jsonl")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, args)
	}
	return calls
}
