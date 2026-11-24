package launch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as git: installFakeGit copies it under that name in
// front of PATH, and TestMain hands control to serveFakeGit, which records the
// arguments, delays the network commands a test names, can move a ref of the
// bare repository before an ls-remote, and then runs the real git.
const (
	envFakeGitReal = "KANDER_TEST_FAKE_GIT_REAL"
	envFakeGitLog  = "KANDER_TEST_FAKE_GIT_LOG"
	// envFakeGitSlow lists "<subcommand>=<duration>" pairs separated by commas.
	envFakeGitSlow = "KANDER_TEST_FAKE_GIT_SLOW"
	// envFakeGitRewind is "<bare>|<ref>|<commit>|<n>": the n-th ls-remote of
	// ref first points ref of the bare repository at commit.
	envFakeGitRewind = "KANDER_TEST_FAKE_GIT_REWIND"
)

// fakeGitSeparator joins the arguments of one call on its log line.
const fakeGitSeparator = "\x1f"

func serveFakeGit() {
	real := os.Getenv(envFakeGitReal)
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if real == "" || name != "git" {
		return
	}
	args := os.Args[1:]
	sub := fakeGitSubcommand(args)
	if sub == "ls-remote" {
		rewindBeforeLsRemote(real, args)
	}
	if path := os.Getenv(envFakeGitLog); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString(strings.Join(args, fakeGitSeparator) + "\n")
			_ = f.Close()
		}
	}
	for _, pair := range strings.Split(os.Getenv(envFakeGitSlow), ",") {
		command, delay, ok := strings.Cut(pair, "=")
		if d, err := time.ParseDuration(delay); ok && err == nil && command == sub {
			time.Sleep(d)
		}
	}
	cmd := exec.Command(real, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		os.Exit(exit.ExitCode())
	case err != nil:
		os.Exit(128)
	}
	os.Exit(0)
}

// fakeGitSubcommand skips the "-c name=value" options git takes before its
// subcommand.
func fakeGitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

func rewindBeforeLsRemote(real string, args []string) {
	parts := strings.Split(os.Getenv(envFakeGitRewind), "|")
	if len(parts) != 4 || len(args) == 0 || args[len(args)-1] != parts[1] {
		return
	}
	// The log holds the earlier calls; this one is not written yet.
	data, _ := os.ReadFile(os.Getenv(envFakeGitLog))
	seen := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Split(line, fakeGitSeparator)
		if fakeGitSubcommand(fields) == "ls-remote" && fields[len(fields)-1] == parts[1] {
			seen++
		}
	}
	if parts[3] == strconv.Itoa(seen+1) {
		_ = exec.Command(real, "--git-dir", parts[0], "update-ref", parts[1], parts[2]).Run()
	}
}

// installFakeGit puts the fake git in front of PATH for the rest of the test
// and returns the file its calls are recorded in.
func installFakeGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "git.log")
	t.Setenv(envFakeGitReal, real)
	t.Setenv(envFakeGitLog, log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// fakeGitCalls returns the recorded calls of one subcommand.
func fakeGitCalls(t *testing.T, log, sub string) [][]string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var calls [][]string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, fakeGitSeparator)
		if fakeGitSubcommand(fields) == sub {
			calls = append(calls, fields)
		}
	}
	return calls
}
