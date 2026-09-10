// Package ghclitest provides a fake GitHub CLI and Git executable for process
// boundary tests. It copies the running test binary under the names `gh` and
// `git`; the copy recognizes the activation environment before the Go test
// runner starts, so no shell, scripting host, or platform specific launcher is
// involved.
//
// Every test package that uses it must call Main first in TestMain:
//
//	func TestMain(m *testing.M) { ghclitest.Main(); os.Exit(m.Run()) }
package ghclitest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ActivationEnv turns the copied test binary into a fake provider program.
const ActivationEnv = "KANDER_FAKE_CLI"

// RecordEnv names a file that receives one JSON line per invocation with the
// working directory and the argv array.
const RecordEnv = "KANDER_FAKE_CLI_RECORD"

// Options describes the canned behavior of one fake program.
type Options struct {
	Stdout      string
	Stderr      string
	Exit        int
	Sleep       time.Duration
	Repeat      int
	InvalidUTF8 bool
}

type invocation struct {
	Directory string   `json:"directory"`
	Argv      []string `json:"argv"`
}

// Main must run before the Go test runner. When this process was launched as a
// fake program it emits the configured behavior and exits.
func Main() {
	if os.Getenv(ActivationEnv) == "" {
		return
	}
	emit()
	os.Exit(exitCode())
}

func emit() {
	options := activeOptions()
	if record := os.Getenv(RecordEnv); record != "" {
		directory, err := os.Getwd()
		if err == nil {
			entry, marshalErr := json.Marshal(invocation{Directory: directory, Argv: os.Args[1:]})
			if marshalErr == nil {
				file, openErr := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if openErr == nil {
					_, _ = file.Write(append(entry, '\n'))
					_ = file.Close()
				}
			}
		}
	}
	if options.Sleep > 0 {
		time.Sleep(options.Sleep)
	}
	stdout := options.Stdout
	if options.Repeat > 1 {
		stdout = strings.Repeat(stdout, options.Repeat)
	}
	if options.InvalidUTF8 {
		_, _ = os.Stdout.Write([]byte{0xff, 0xfe, 'x'})
	} else {
		_, _ = io.WriteString(os.Stdout, stdout)
	}
	_, _ = io.WriteString(os.Stderr, options.Stderr)
}

// activeOptions selects the per-command route whose key is a substring of the
// joined argv, falling back to the environment defaults.
func activeOptions() Options {
	if encoded := os.Getenv(prefix() + "ROUTES"); encoded != "" {
		routes := map[string]Options{}
		if err := json.Unmarshal([]byte(encoded), &routes); err == nil {
			joined := strings.Join(os.Args[1:], " ")
			for key, options := range routes {
				if strings.Contains(joined, key) {
					return options
				}
			}
		}
	}
	return Options{
		Stdout:      os.Getenv(prefix() + "STDOUT"),
		Stderr:      os.Getenv(prefix() + "STDERR"),
		Exit:        intValue("EXIT"),
		Sleep:       durationValue("SLEEP_MS"),
		Repeat:      intValue("REPEAT"),
		InvalidUTF8: value("INVALID_UTF8") != "",
	}
}

func exitCode() int {
	return activeOptions().Exit
}

func prefix() string {
	base := filepath.Base(os.Args[0])
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	return "KANDER_FAKE_" + strings.ToUpper(base) + "_"
}

func value(name string) string {
	return strings.TrimSpace(os.Getenv(prefix() + name))
}

func intValue(name string) int {
	if value := value(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return 0
}

func durationValue(name string) time.Duration {
	return time.Duration(intValue(name)) * time.Millisecond
}

// Set configures one fake program for the current test.
func Set(t *testing.T, program string, options Options) {
	t.Helper()
	base := "KANDER_FAKE_" + strings.ToUpper(program) + "_"
	t.Setenv(base+"STDOUT", options.Stdout)
	t.Setenv(base+"STDERR", options.Stderr)
	t.Setenv(base+"EXIT", fmt.Sprintf("%d", options.Exit))
	t.Setenv(base+"SLEEP_MS", fmt.Sprintf("%d", options.Sleep.Milliseconds()))
	t.Setenv(base+"REPEAT", fmt.Sprintf("%d", options.Repeat))
	invalid := ""
	if options.InvalidUTF8 {
		invalid = "1"
	}
	t.Setenv(base+"INVALID_UTF8", invalid)
}

// SetRoutes configures per-command behavior: the first route whose key is a
// substring of the joined argv wins over the Set defaults.
func SetRoutes(t *testing.T, program string, routes map[string]Options) {
	t.Helper()
	encoded, err := json.Marshal(routes)
	if err != nil {
		t.Fatalf("encode routes: %v", err)
	}
	t.Setenv("KANDER_FAKE_"+strings.ToUpper(program)+"_ROUTES", string(encoded))
}

// Install copies the running test binary into a temporary directory as `gh` and
// `git` and prepends that directory to PATH. It returns the directory.
func Install(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	directory := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	for _, name := range []string{"gh", "git"} {
		target := filepath.Join(directory, name+suffix)
		if err := copyExecutable(executable, target); err != nil {
			t.Fatalf("install fake %s: %v", name, err)
		}
	}
	t.Setenv(ActivationEnv, "1")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return directory
}

// Record returns the invocations captured so far, in order.
func Record(t *testing.T, path string) []invocation {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read record: %v", err)
	}
	var invocations []invocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry invocation
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode record line %q: %v", line, err)
		}
		invocations = append(invocations, entry)
	}
	return invocations
}

func copyExecutable(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o755)
}
