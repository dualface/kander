//go:build unix

package review

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// reviewArgvFor runs a custom reviewer whose args.review passes the rules
// directory behind a flag, and returns the argv the fake reviewer received.
func reviewArgvFor(t *testing.T, h *reviewHarness) []string {
	t.Helper()
	t.Setenv("FAKE_CUSTOM_MODE", "file")
	writeCustomReviewerConfig(t, h, map[string]any{
		"path": h.fake,
		"args": map[string]any{
			"start": []string{}, "resume": []string{},
			"review": []string{"--out", "{output}", "--add-dir", "{rules_dir}", "{instruction}"},
		},
		"session": map[string]any{"mode": "none"},
		"review": map[string]any{
			"cwd": "runtime", "output_name": "result.txt", "home_policy": "optional", "stdin": "none",
			"output": map[string]any{"source": "file", "parse": "raw"},
		},
	})
	code, out, errText := h.review("helper", "PMQA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s out=%s", code, errText, out)
	}
	return strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
}

func TestRulesDirReachesTheReviewerArgv(t *testing.T) {
	h := newCustomHarness(t)
	rulesDir := filepath.Join(h.home, ".agents", "kander")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argv := reviewArgvFor(t, h)
	i := slices.Index(argv, "--add-dir")
	if i < 0 || i+1 >= len(argv) || argv[i+1] != rulesDir {
		t.Fatalf("argv=%v, want --add-dir %s", argv, rulesDir)
	}
}

func TestMissingRulesDirDropsTheElementAndItsFlag(t *testing.T) {
	h := newCustomHarness(t)
	argv := reviewArgvFor(t, h)
	if slices.Contains(argv, "--add-dir") {
		t.Fatalf("argv=%v keeps the flag before the missing rules directory", argv)
	}
	for _, arg := range argv {
		if strings.Contains(arg, ".agents") {
			t.Fatalf("argv=%v names the missing rules directory", argv)
		}
	}
	if !slices.Contains(argv, "--out") {
		t.Fatalf("argv=%v lost the unrelated arguments", argv)
	}
}
