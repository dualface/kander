//go:build unix

package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func jsonObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var object map[string]any
	if !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 1 || json.Unmarshal([]byte(text), &object) != nil {
		t.Fatalf("not one JSON object and a newline: %q", text)
	}
	return object
}

// A review run with --json is one object that holds the report, with nothing on stderr; a run
// that failed before it published evidence is the error object.
func TestReviewRunWithJSON(t *testing.T) {
	h := newCodexHarness(t)
	code, stdout, stderr := h.review("codex", "PMQA", "确认改动正确", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	result := jsonObject(t, stdout)
	if result["exit_code"] != float64(0) || strings.TrimSpace(result["report"].(string)) == "" {
		t.Fatalf("result=%v", result)
	}
	// A role that does not exist fails before any reviewer starts: the error object, the status of the text form.
	code, stdout, stderr = h.review("codex", "Nobody", "确认改动正确", "--json")
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	detail := jsonObject(t, stderr)["error"].(map[string]any)
	if detail["code"] != "review_failed" || !strings.Contains(detail["message"].(string), "Nobody") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestReviewSchemaWithJSON(t *testing.T) {
	h := newCodexHarness(t)
	_ = h
	code, stdout, stderr := captureRun(t, []string{"plan", "--schema", "--json"})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	result := jsonObject(t, stdout)
	if result["command"] != "plan" || !strings.HasPrefix(result["schema"].(string), "command: plan\n") {
		t.Fatalf("result=%v", result)
	}
}
