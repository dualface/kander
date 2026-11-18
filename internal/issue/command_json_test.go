package issue

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/command/commandtest"
)

// jsonObject parses text as one JSON object and a newline.
func jsonObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var object map[string]any
	if strings.Count(text, "\n") != 1 || !strings.HasSuffix(text, "\n") || json.Unmarshal([]byte(text), &object) != nil {
		t.Fatalf("not one JSON object and a newline: %q", text)
	}
	return object
}

// A failure with --json is the error object on stderr, with the code of the issue error kind
// and the localized text, and nothing on stdout.
func TestIssueFailureWithJSONIsAnErrorObject(t *testing.T) {
	stub := &stubResolver{err: &Error{Kind: ErrorAmbiguousRemotes, Op: "remote", Candidates: []string{"github.com/a/b (origin)", "gitlab.com/c/d (mirror)"}}}
	code, stdout, stderr := runIssue(t, stub, "repo", "--json")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	detail := jsonObject(t, stderr)["error"].(map[string]any)
	if detail["code"] != "issue_ambiguous_remotes" || !strings.Contains(detail["message"].(string), "github.com/a/b (origin)") {
		t.Fatalf("stderr=%s", stderr)
	}
	// The failure of a query before any provider call keeps its exit status.
	code, stdout, stderr = runIssue(t, &stubResolver{}, "list", "--state", "bogus", "--json")
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if detail := jsonObject(t, stderr)["error"].(map[string]any); detail["code"] != "issue_invalid_query" {
		t.Fatalf("stderr=%s", stderr)
	}
	// A failure that is not an issue error is a plain failure.
	code, _, stderr = runIssue(t, &stubResolver{issueErr: errPlain}, "show", "42", "--json")
	if detail := jsonObject(t, stderr)["error"].(map[string]any); code != 1 || detail["code"] != "failed" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestIssueResultObjectsOfTheQueryCommands(t *testing.T) {
	repository := importTestRepository()
	stub := &stubResolver{repository: repository, snapshot: importTestSnapshot(repository, 42), page: IssuePage{Repository: repository, Limit: 30}}
	for name, tc := range map[string]struct {
		args   []string
		fields []string
	}{
		"repo": {[]string{"repo", "--json"}, []string{"host", "owner", "name", "url", "private", "remote"}},
		"list": {[]string{"list", "--json"}, []string{"repository", "issues", "limit", "more"}},
		"show": {[]string{"show", "42", "--json"}, []string{"repository", "issue"}},
	} {
		code, stdout, stderr := runIssue(t, stub, tc.args...)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: code=%d stderr=%q", name, code, stderr)
		}
		object := jsonObject(t, stdout)
		for _, field := range tc.fields {
			if _, ok := object[field]; !ok {
				t.Fatalf("%s: no %q in %s", name, field, stdout)
			}
		}
	}
}

func TestIssueImportAndTriageResultObjects(t *testing.T) {
	importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{repository: repository, snapshot: importTestSnapshot(repository, 42)}
	code, stdout, stderr := runIssue(t, stub, "import", "42", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("import: code=%d stderr=%q", code, stderr)
	}
	imported := jsonObject(t, stdout)
	for _, field := range []string{"task_id", "state", "path", "existing", "source_key", "source_url", "comments_loaded"} {
		if _, ok := imported[field]; !ok {
			t.Fatalf("import: no %q in %s", field, stdout)
		}
	}

	old := triageStarter
	t.Cleanup(func() { triageStarter = old })
	triageStarter = func(TriageLaunch) (TriageOutcome, error) {
		return TriageOutcome{Agent: "claude", Launcher: "tmux", Address: "session:win:pane", Warnings: []string{"first warning"}}, nil
	}
	code, stdout, stderr = runIssue(t, stub, "triage", "42", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("triage: code=%d stderr=%q", code, stderr)
	}
	triage := jsonObject(t, stdout)
	if triage["agent"] != "claude" || triage["launcher"] != "tmux" || triage["issue"] != "dualface/kander#42" || triage["address"] != "session:win:pane" {
		t.Fatalf("triage: %s", stdout)
	}
	notices, _ := triage["notices"].([]any)
	if len(notices) != 1 || notices[0].(map[string]any)["code"] != "launch_warning" || notices[0].(map[string]any)["message"] != "first warning" {
		t.Fatalf("triage notices: %s", stdout)
	}

	triageStarter = func(TriageLaunch) (TriageOutcome, error) {
		return TriageOutcome{}, errors.New("simulated terminal failure")
	}
	code, stdout, stderr = runIssue(t, stub, "triage", "42", "--json")
	if code != 1 || stdout != "" {
		t.Fatalf("failed triage: code=%d stdout=%q", code, stdout)
	}
	if detail := jsonObject(t, stderr)["error"].(map[string]any); detail["code"] != "failed" || !strings.Contains(detail["message"].(string), "simulated terminal failure") {
		t.Fatalf("failed triage stderr=%s", stderr)
	}
}

func TestResultCommandWithJSON(t *testing.T) {
	_, repo, id, fake := resultFixture(t)
	provider := &resultCommandProvider{stubResolver: &stubResolver{repository: repo}, resultFake: fake}
	factory := func() IssueProvider { return provider }
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, diagnostics bytes.Buffer
		base := []string{"result", "42", "--card", id, "--repo", "github.com/dualface/kander", "--json"}
		return commandtest.Capture(func() int {
			return commandtest.Run(newCommand(factory, &out, &diagnostics), append(base, args...)...)
		})
	}
	code, stdout, stderr := run("--action", "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect: code=%d stderr=%q", code, stderr)
	}
	var inspection ResultInspection
	if err := json.Unmarshal([]byte(stdout), &inspection); err != nil || jsonObject(t, stdout) == nil {
		t.Fatalf("inspect: %q %v", stdout, err)
	}

	old := triageStarter
	t.Cleanup(func() { triageStarter = old })
	triageStarter = func(TriageLaunch) (TriageOutcome, error) {
		return TriageOutcome{}, errors.New("simulated terminal failure")
	}
	code, stdout, stderr = run()
	if code != 1 || stdout != "" {
		t.Fatalf("start: code=%d stdout=%q", code, stdout)
	}
	if detail := jsonObject(t, stderr)["error"].(map[string]any); detail["code"] != "failed" || !strings.Contains(detail["message"].(string), "simulated terminal failure") {
		t.Fatalf("start stderr=%s", stderr)
	}
}
