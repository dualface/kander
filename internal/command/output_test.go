package command_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

func run(c *cobra.Command, args ...string) (code int, stdout, stderr string) {
	return commandtest.Capture(func() int { return commandtest.Run(c, args...) })
}

func object(t *testing.T, text string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil || strings.Count(text, "\n") != 1 {
		t.Fatalf("not one JSON object line: %q (%v)", text, err)
	}
	return v
}

func TestResultObjectCarriesNotices(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		command.Notify("first", "first remark\n")
		command.Notify("second", "second remark")
		return command.Exit(command.Result(map[string]any{"value": 1}))
	}}
	code, stdout, stderr := run(c, "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	got := object(t, stdout)
	if got["value"] != float64(1) {
		t.Fatalf("result lost: %v", got)
	}
	notices := got["notices"].([]any)
	if len(notices) != 2 || notices[0].(map[string]any)["code"] != "first" || notices[0].(map[string]any)["message"] != "first remark" {
		t.Fatalf("notices: %v", notices)
	}
}

func TestEmptyResultStillHoldsNotices(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		command.Notify("only", "remark")
		return command.Exit(command.Result(struct{}{}))
	}}
	_, stdout, _ := run(c, "--json")
	if got := object(t, stdout); len(got) != 1 || got["notices"] == nil {
		t.Fatalf("got %v", got)
	}
}

func TestTextModeNoticeGoesToStderr(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		command.Notify("first", "remark")
		return nil
	}}
	code, stdout, stderr := run(c)
	if code != 0 || stdout != "" || stderr != "remark\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestFailureWritesOnlyTheErrorObject(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		command.Result(map[string]any{"lost": true})
		command.Notify("note", "remark")
		return command.Exit(command.Fail(command.Coded("probe_failed", errors.New("it broke"))))
	}}
	code, stdout, stderr := run(c, "--json")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	got := object(t, stderr)
	detail := got["error"].(map[string]any)
	if detail["code"] != "probe_failed" || detail["message"] != "it broke" || got["notices"] == nil {
		t.Fatalf("got %v", got)
	}
}

func TestFailureTextMode(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		return command.Exit(command.Fail(errors.New("it broke")))
	}}
	code, stdout, stderr := run(c)
	if code != 1 || stdout != "" || stderr != "kander: it broke\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestUsageErrorIsAnObjectWithStatusTwo(t *testing.T) {
	c := &cobra.Command{Use: "probe", Args: command.Args(0, 0), RunE: func(*cobra.Command, []string) error { return nil }}
	for _, args := range [][]string{{"--json", "extra"}, {"--bogus", "--json"}, {"extra", "--json"}} {
		code, stdout, stderr := run(c, args...)
		if code != 2 || stdout != "" {
			t.Fatalf("%v: code=%d stdout=%q", args, code, stdout)
		}
		if got := object(t, stderr)["error"].(map[string]any); got["code"] != command.CodeUsage {
			t.Fatalf("%v: %v", args, got)
		}
	}
}

func TestInteractiveFormIsRefusedWithStatusTwo(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		return command.Interactive("needs an answer")
	}}
	code, stdout, stderr := run(c, "--json")
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if got := object(t, stderr)["error"].(map[string]any); got["code"] != command.CodeInteractive || got["message"] != "needs an answer" {
		t.Fatalf("got %v", got)
	}
}

func TestResultStatusKeepsTheResultOfAFinishedAnalysis(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		return command.Exit(command.ResultStatus(map[string]any{"status": "fail"}, 1))
	}}
	code, stdout, stderr := run(c, "--json")
	if code != 1 || stderr != "" || object(t, stdout)["status"] != "fail" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStreamingCommandKeepsLinesAndEndsWithAnErrorObject(t *testing.T) {
	c := command.Streaming(&cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		command.Emit(map[string]string{"event": "first"})
		command.Notify("note", "remark")
		return command.Exit(command.Fail(errors.New("later failure")))
	}})
	code, stdout, stderr := run(c, "--json")
	if code != 1 || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) != 2 {
		t.Fatalf("stderr lines: %q", lines)
	}
	if notice := object(t, lines[0]+"\n")["notice"].(map[string]any); notice["code"] != "note" {
		t.Fatalf("notice line: %q", lines[0])
	}
	if got := object(t, lines[1]+"\n")["error"].(map[string]any); got["message"] != "later failure" {
		t.Fatalf("error line: %q", lines[1])
	}
}

func TestObjectIsTheSameWithAndWithoutJSON(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		return command.Exit(command.Object(map[string]any{"k": "v"}))
	}}
	for _, args := range [][]string{{}, {"--json"}} {
		code, stdout, stderr := run(c, args...)
		if code != 0 || stderr != "" || object(t, stdout)["k"] != "v" {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

func TestResultMustBeAnObject(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		return command.Exit(command.Result([]int{1}))
	}}
	if code, stdout, _ := run(c, "--json"); code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestJSONIsGlobalAndFalseTurnsItOff(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		if command.JSON() {
			return command.Exit(command.Result(struct{}{}))
		}
		command.Notify("text", "plain")
		return nil
	}}
	for args, want := range map[string]bool{"--json": true, "--json=true": true, "--json=false": false, "": false} {
		var a []string
		if args != "" {
			a = []string{args}
		}
		_, stdout, _ := run(c, a...)
		if (stdout != "") != want {
			t.Fatalf("%q: stdout=%q", args, stdout)
		}
	}
}

// A --json value that strconv.ParseBool refuses is a usage error, and the values it accepts
// turn JSON on or off exactly as the error scan of an unparsed command line reads them.
func TestJSONFlagValueIsParsedLikeABool(t *testing.T) {
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		if command.JSON() {
			return command.Exit(command.Result(struct{}{}))
		}
		return nil
	}}
	for value, want := range map[string]bool{"1": true, "t": true, "T": true, "TRUE": true, "0": false, "f": false, "F": false, "FALSE": false} {
		_, stdout, _ := run(c, "--json="+value)
		if (stdout != "") != want {
			t.Fatalf("--json=%s: stdout=%q", value, stdout)
		}
	}
	for _, value := range []string{"flase", "yes", "", "2"} {
		code, stdout, stderr := run(c, "--json="+value)
		if code != 2 || stdout != "" || stderr == "" {
			t.Fatalf("--json=%s: code=%d stdout=%q stderr=%q", value, code, stdout, stderr)
		}
	}
}

// A result that cannot be written to stdout is a failure, not a success with a missing object.
func TestResultWriteFailureIsAFailure(t *testing.T) {
	closed, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
		// The result is written when the command ends, so stdout goes bad before that.
		os.Stdout = closed
		return command.Exit(command.Result(map[string]string{"k": "v"}))
	}}
	code, _, stderr := commandtest.Capture(func() int { return commandtest.Run(c, "--json") })
	if code != 1 || !strings.Contains(stderr, `"error"`) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

// A streaming command keeps its remarks with the command until its first event reaches stdout:
// a failure before it is one error object that holds them; afterwards each remark is a line.
func TestStreamingRemarksWaitForTheFirstEvent(t *testing.T) {
	probe := func(run func() error) *cobra.Command {
		return command.Streaming(&cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return run() }})
	}
	t.Run("failure before the first event", func(t *testing.T) {
		c := probe(func() error {
			command.Notify("early", "first remark")
			command.Notify("early", "second remark")
			return errors.New("boom")
		})
		code, stdout, stderr := run(c, "--json")
		if code != 1 || stdout != "" || strings.Count(stderr, "\n") != 1 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var object struct {
			Error   command.Notice   `json:"error"`
			Notices []command.Notice `json:"notices"`
		}
		if err := json.Unmarshal([]byte(stderr), &object); err != nil || object.Error.Message != "boom" || len(object.Notices) != 2 || object.Notices[0].Message != "first remark" {
			t.Fatalf("stderr=%q %v", stderr, err)
		}
	})
	t.Run("failure after the first event", func(t *testing.T) {
		c := probe(func() error {
			command.Notify("early", "before")
			if code := command.Emit(map[string]string{"event": "snapshot"}); code != 0 {
				return command.Exit(code)
			}
			command.Notify("late", "after")
			return errors.New("boom")
		})
		code, stdout, stderr := run(c, "--json")
		lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
		if code != 1 || strings.Count(stdout, "\n") != 1 || len(lines) != 3 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		for i, want := range []string{`"notice"`, `"notice"`, `"error"`} {
			if !strings.Contains(lines[i], want) {
				t.Fatalf("line %d %q lacks %s", i, lines[i], want)
			}
		}
		if strings.Contains(lines[2], `"notices"`) {
			t.Fatalf("the error object repeats a remark that was already written: %q", lines[2])
		}
	})
	t.Run("end before the first event", func(t *testing.T) {
		c := probe(func() error {
			command.Notify("early", "only remark")
			return nil
		})
		code, stdout, stderr := run(c, "--json")
		if code != 0 || stdout != "" || !strings.Contains(stderr, `{"notice":`) || strings.Count(stderr, "\n") != 1 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})
}
