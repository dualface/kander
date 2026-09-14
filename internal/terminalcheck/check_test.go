package terminalcheck

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal"
)

func TestEveryBackendMethodHasCheck(t *testing.T) {
	methods := reflect.TypeOf((*terminal.Backend)(nil)).Elem()
	found := map[string]bool{}
	capabilities := reflect.TypeOf(terminal.Capabilities{})
	for _, s := range methodSteps {
		if s.check == nil || found[s.method] {
			t.Fatalf("missing or duplicate check: %s", s.method)
		}
		if _, ok := methods.MethodByName(s.method); !ok {
			t.Fatalf("stale method: %s", s.method)
		}
		for _, name := range strings.FieldsFunc(s.capability, func(r rune) bool { return r == '|' || r == '&' }) {
			if name != "" {
				if _, ok := capabilities.FieldByName(name); !ok {
					t.Fatalf("unknown capability: %s", name)
				}
			}
		}
		if _, err := supports(terminal.Capabilities{}, s.capability); err != nil {
			t.Fatalf("invalid runtime requirement for %s: %v", s.method, err)
		}
		found[s.method] = true
	}
	for i := 0; i < methods.NumMethod(); i++ {
		if !found[methods.Method(i).Name] {
			t.Errorf("unchecked Backend method: %s", methods.Method(i).Name)
		}
	}
}

func TestDefinitionConformanceLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real conformance commands require POSIX sh")
	}
	for _, test := range []struct {
		name, capability       string
		keep, wrong, failFacts bool
		wantCode               int
	}{
		{name: "pass"}, {name: "metadata mismatch", wrong: true, wantCode: 1}, {name: "keep", keep: true},
		{name: "keep after failure", keep: true, wrong: true, wantCode: 1},
		{name: "no metadata", capability: "pane_metadata"}, {name: "no foreground", capability: "foreground_process"},
		{name: "no metadata ordinary failure", capability: "pane_metadata", failFacts: true, wantCode: 1},
		{name: "no foreground ordinary failure", capability: "foreground_process", failFacts: true, wantCode: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, root := fixture(t, test.capability)
			if test.wrong {
				writeFixture(t, root, "wrong", "yes")
			}
			if test.failFacts {
				failure := "facts"
				if test.capability == "foreground_process" {
					failure = "foreground"
				}
				writeFixture(t, root, "failure", failure)
			}
			var output bytes.Buffer
			args := []string{"test", "checkterm"}
			if test.keep {
				args = append(args, "--keep")
			}
			code := run(args, &output, &output)
			text := output.String()
			if code != test.wantCode {
				t.Fatalf("exit=%d want=%d\n%s", code, test.wantCode, text)
			}
			closed, _ := os.ReadFile(filepath.Join(root, "closed"))
			if (string(closed) == "yes") == test.keep {
				t.Fatalf("cleanup/keep violated\n%s", text)
			}
			if test.wrong && (!strings.Contains(text, "fail SetSessionMarker") || !strings.Contains(text, "wrong-value")) {
				t.Fatalf("missing actual metadata\n%s", text)
			}
			if test.keep && !strings.Contains(text, "Kept container: checkterm:c1:p1") {
				t.Fatalf("kept address missing\n%s", text)
			}
			if code == 0 {
				for _, s := range methodSteps {
					state := "pass"
					if s.method == "ReportSession" || (s.method == "CloseContainer" && test.keep) || (test.capability == "pane_metadata" && (s.method == "SetSessionMarker" || s.method == "ReverseLookup")) || (test.capability == "foreground_process" && s.method == "PaneFacts") {
						state = "skip"
					}
					if !strings.Contains(text, state+" "+s.method+" ") {
						t.Errorf("missing %s %s\n%s", state, s.method, text)
					}
				}
				state := "pass"
				if test.keep {
					state = "skip"
				}
				if !strings.Contains(text, state+" PaneFacts.gone") {
					t.Errorf("gone step missing\n%s", text)
				}
				if !strings.Contains(text, "argv=") || !strings.Contains(text, "stdout=") || !strings.Contains(text, "stderr=") {
					t.Fatal("missing command diagnostics")
				}
			}
		})
	}
}

type brokenMetadata struct{ terminal.Backend }

func (b brokenMetadata) SetSessionMarker(terminal.Conn, string, string) error {
	return errors.New("ordinary failure")
}
func TestUnsupportedMustNotBeOrdinaryFailure(t *testing.T) {
	backend, _ := fixture(t, "pane_metadata")
	c := &checker{backend: brokenMetadata{backend}}
	if _, err := c.metadata(false); err == nil || !strings.Contains(err.Error(), "ordinary failure") {
		t.Fatalf("error=%v", err)
	}
}

func TestConformanceLoadFailureBeforeCreate(t *testing.T) {
	_, root := fixture(t, "")
	dirs := terminal.DefinitionDirs()
	if err := os.WriteFile(filepath.Join(dirs[0].Path, "checkterm.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	terminal.ReloadDefinitions()
	var output bytes.Buffer
	if code := run([]string{"test", "checkterm"}, &output, &output); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); !os.IsNotExist(err) {
		t.Fatal("created before definition validation")
	}
	report := terminal.DefinitionInventory()
	var diagnostic string
	for _, r := range report {
		if r.Err != nil {
			diagnostic = r.Err.Error()
		}
	}
	if diagnostic == "" || !strings.Contains(output.String(), diagnostic) {
		t.Fatal("test and inventory disagree")
	}
	output.Reset()
	if code := run([]string{"list"}, &output, &output); code != 1 || !strings.Contains(output.String(), diagnostic) {
		t.Fatalf("list disagrees: %s", output.String())
	}
	if !strings.Contains(output.String(), "[embedded] tmux") || !strings.Contains(output.String(), "[embedded] herdr") {
		t.Fatal("embedded sources absent")
	}
}

func TestTerminalCommandUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"test"}, {"list", "extra"}, {"test", "tmux", "--bad"}} {
		var output bytes.Buffer
		if code := run(args, &output, &output); code != 2 || !strings.Contains(output.String(), "--keep") {
			t.Fatalf("args=%q code=%d out=%s", args, code, output.String())
		}
	}
}

func TestConformanceFailuresRemainFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real conformance commands require POSIX sh")
	}
	for _, operation := range []string{"version", "wait", "topology", "focus", "key", "close"} {
		t.Run(operation, func(t *testing.T) {
			_, root := fixture(t, "")
			writeFixture(t, root, "failure", operation)
			var output bytes.Buffer
			if code := run([]string{"test", "checkterm"}, &output, &output); code != 1 {
				t.Fatalf("code=%d\n%s", code, output.String())
			}
			text := output.String()
			if !strings.Contains(text, "fail ") || !strings.Contains(text, "intentional failure") {
				t.Fatalf("failure lost: %s", text)
			}
			_, created := os.Stat(filepath.Join(root, "created"))
			closed, _ := os.ReadFile(filepath.Join(root, "closed"))
			switch operation {
			case "version":
				if !os.IsNotExist(created) {
					t.Fatal("preflight failure created a container")
				}
			case "close":
				if !strings.Contains(text, "fail cleanup") || !strings.Contains(text, "Kept container:") || len(closed) != 0 {
					t.Fatalf("cleanup failure hidden: %s", text)
				}
			default:
				if string(closed) != "yes" {
					t.Fatalf("container leaked: %s", text)
				}
			}
		})
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }
func TestConformanceOutputFailureStopsBeforeCreate(t *testing.T) {
	backend, root := fixture(t, "")
	err := Check(backend, Options{}, failedWriter{})
	if err == nil || !strings.Contains(err.Error(), "output closed") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); !os.IsNotExist(err) {
		t.Fatal("created after output failure")
	}
}

func TestConformanceSkipFocusStillDelivers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real conformance commands require POSIX sh")
	}
	_, root := fixture(t, "")
	writeFixture(t, root, "failure", "focus")
	var output bytes.Buffer
	if code := run([]string{"test", "checkterm", "--skip-focus"}, &output, &output); code != 0 {
		t.Fatalf("code=%d\n%s", code, output.String())
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if strings.Contains(string(calls), "focus|") || !strings.Contains(output.String(), "skip Focus --skip-focus") || !strings.Contains(output.String(), "pass DeliverText") {
		t.Fatal(output.String())
	}
}
