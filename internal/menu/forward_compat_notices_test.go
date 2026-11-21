//go:build unix

package menu

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

// Content only a newer version writes, in the three kinds the loader reports: unknown keys
// (including one in the v2-only server section), an agents entry this version cannot use, and
// roles that name an agent it does not know.
const (
	ignoredFieldCount  = 3 // future_top, tui.future, server.token
	ignoredAgentCount  = 1 // models.kanban.newagent
	unusableEntryCount = 1 // agents.badagent
	unknownRefCount    = 4 // kanban_agent, kanban_agents.large, chat_agent, reviewers.large.PMQA
)

func forwardCompatDocument(t *testing.T, withRefs bool) map[string]any {
	t.Helper()
	base := config.DefaultConfig()
	base.WelcomeComplete = true
	base.Language = "en"
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	config.OverlaySet(doc, map[string]any{"enabled": true}, "future_top")
	config.OverlaySet(doc, "x", "tui", "future")
	config.OverlaySet(doc, "t", "server", "token")
	config.OverlaySet(doc, map[string]any{"large_model": "m"}, "models", "kanban", "newagent")
	config.OverlaySet(doc, map[string]any{"path": "sh", "dialect": "futuredialect"}, "agents", "badagent")
	if withRefs {
		doc["kanban_agent"] = "newagent"
		doc["kanban_agents"] = map[string]any{"large": "newagent", "small": "codex"}
		doc["chat_agent"] = "newagent"
		doc["reviewers"].(map[string]any)["large"].(map[string]any)["PMQA"] = "newagent"
	}
	return doc
}

// cleanTwin is the same configuration without the content of a newer version.
func cleanTwin(t *testing.T) map[string]any {
	t.Helper()
	doc := forwardCompatDocument(t, false)
	delete(doc, "future_top")
	config.OverlayDelete(doc, "tui", "future")
	config.OverlayDelete(doc, "server", "token")
	config.OverlayDelete(doc, "models", "kanban", "newagent")
	config.OverlayDelete(doc, "agents", "badagent")
	return doc
}

type configRun struct {
	code           int
	stdout, stderr string
}

func runConfigCommand(t *testing.T, args ...string) configRun {
	t.Helper()
	code, stdout, stderr := commandtest.Capture(func() int { return commandtest.Run(NewConfigCommand(), args...) })
	return configRun{code, stdout, stderr}
}

func noticeCounts(t *testing.T, notices []command.Notice) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, notice := range notices {
		counts[notice.Code]++
	}
	return counts
}

func TestConfigJSONReportsIgnoredContentAsNotices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	writeDoc(t, path, forwardCompatDocument(t, true))
	run := runConfigCommand(t, "--json")
	if run.code != 0 || run.stderr != "" {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	var result struct {
		Notices []command.Notice `json:"notices"`
	}
	if err := json.Unmarshal([]byte(run.stdout), &result); err != nil {
		t.Fatal(err)
	}
	counts := noticeCounts(t, result.Notices)
	// Every item is one notice: an unusable agents entry and an unknown agent reference are
	// agent_unusable, every other ignored item is config_ignored.
	if counts["config_ignored"] != ignoredFieldCount+ignoredAgentCount || counts["agent_unusable"] != unusableEntryCount+unknownRefCount {
		t.Fatalf("notices=%+v", result.Notices)
	}
	text := ""
	for _, notice := range result.Notices {
		text += notice.Code + " " + notice.Message + "\n"
	}
	for _, want := range []string{"future_top", "tui.future", "server.token", "models.kanban.newagent", "agents.badagent", "kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA"} {
		if strings.Count(text, want) < 1 {
			t.Fatalf("no notice names %q:\n%s", want, text)
		}
	}
	// One notice per item: the unusable agents entry is not also a config_ignored notice.
	for _, notice := range result.Notices {
		if notice.Code == "config_ignored" && strings.Contains(notice.Message, "agents.badagent") {
			t.Fatalf("an unusable agent is reported twice: %+v", result.Notices)
		}
	}
}

// unusableBuiltinDocument is a configuration whose built-in codex entry this version cannot use,
// with the user's own model for it; the twin lacks only that entry.
func unusableBuiltinDocument(t *testing.T, unusable bool) map[string]any {
	t.Helper()
	doc := cleanTwin(t)
	config.OverlaySet(doc, "user-model", "models", "kanban", "codex", "large_model")
	if unusable {
		doc["agents"] = map[string]any{"codex": map[string]any{
			"args":    map[string]any{"start": []any{}, "resume": []any{}},
			"session": map[string]any{"mode": "hook:future-hook"},
		}}
	}
	return doc
}

// With ignored content the output of kander config is the output of the same configuration
// without that content, apart from the notices: unknown keys, an unusable custom agent, an
// unusable built-in agent (whose models stay in the output), and roles that name unknown agents
// (which show the value they have without the key).
func TestConfigOutputEqualsThatWithoutTheIgnoredContent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		dirty, clean func(t *testing.T) map[string]any
		notices      int
	}{
		{"unknown keys and an unusable custom agent", func(t *testing.T) map[string]any { return forwardCompatDocument(t, false) }, cleanTwin, ignoredFieldCount + ignoredAgentCount + unusableEntryCount},
		// The entry, the three models.* entries of codex it takes out, and the eight roles that name codex.
		{"an unusable built-in agent", func(t *testing.T) map[string]any { return unusableBuiltinDocument(t, true) }, func(t *testing.T) map[string]any { return unusableBuiltinDocument(t, false) }, 12},
		{"unknown agent references and the rest", func(t *testing.T) map[string]any { return forwardCompatDocument(t, true) }, cleanTwin, ignoredFieldCount + ignoredAgentCount + unusableEntryCount + unknownRefCount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			t.Setenv(config.EnvConfig, path)
			writeDoc(t, path, tc.dirty(t))
			dirtyJSON, dirtyText := runConfigCommand(t, "--json"), runConfigCommand(t)
			writeDoc(t, path, tc.clean(t))
			cleanJSON, cleanText := runConfigCommand(t, "--json"), runConfigCommand(t)
			for name, run := range map[string]configRun{"dirty --json": dirtyJSON, "clean --json": cleanJSON, "clean": cleanText, "dirty": dirtyText} {
				if run.code != 0 {
					t.Fatalf("%s: code=%d stderr=%q", name, run.code, run.stderr)
				}
			}
			// With --json the notices are a field of the result and stderr stays empty.
			if dirtyJSON.stderr != "" || cleanJSON.stderr != "" {
				t.Fatalf("stderr with --json: %q %q", dirtyJSON.stderr, cleanJSON.stderr)
			}
			var dirtyObject, cleanObject map[string]any
			if err := json.Unmarshal([]byte(dirtyJSON.stdout), &dirtyObject); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(cleanJSON.stdout), &cleanObject); err != nil {
				t.Fatal(err)
			}
			if _, ok := cleanObject["notices"]; ok {
				t.Fatalf("a configuration without ignored content has notices: %s", cleanJSON.stdout)
			}
			notices, _ := dirtyObject["notices"].([]any)
			if len(notices) != tc.notices {
				t.Fatalf("notices=%d want %d: %s", len(notices), tc.notices, dirtyJSON.stdout)
			}
			delete(dirtyObject, "notices")
			if !jsonEqual(t, dirtyObject, cleanObject) {
				t.Fatalf("stdout differs apart from notices:\n%s\n%s", dirtyJSON.stdout, cleanJSON.stdout)
			}
			// Without --json the same lines go to stderr and stdout is unchanged.
			if dirtyText.stdout != cleanText.stdout {
				t.Fatalf("stdout changed by ignored content:\n%s\n---\n%s", dirtyText.stdout, cleanText.stdout)
			}
			if cleanText.stderr != "" {
				t.Fatalf("stderr of a clean configuration: %q", cleanText.stderr)
			}
			if lines := strings.Split(strings.TrimSpace(dirtyText.stderr), "\n"); len(lines) != tc.notices {
				t.Fatalf("stderr lines=%d want %d:\n%s", len(lines), tc.notices, dirtyText.stderr)
			}
		})
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(left) == string(right)
}

func TestConfigTextReportsUnknownAgentReferencesOnStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	writeDoc(t, path, forwardCompatDocument(t, true))
	run := runConfigCommand(t)
	if run.code != 0 {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	for _, want := range []string{"kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA", "newagent"} {
		if !strings.Contains(run.stderr, want) {
			t.Fatalf("stderr misses %q:\n%s", want, run.stderr)
		}
	}
	if strings.Contains(run.stdout, "future_top") {
		t.Fatalf("ignored content reached stdout:\n%s", run.stdout)
	}
}

type doctorRun struct {
	code    int
	healthy bool
	lines   []doctorLine
	stdout  string
	stderr  string
}

func runDoctorWith(t *testing.T, h *harness, doc map[string]any) doctorRun {
	t.Helper()
	h.writeConfig(doc)
	old := confirmationsOf
	confirmationsOf = func(TerminalTools, []install.PathKanderBinary) []string { return nil }
	t.Cleanup(func() { confirmationsOf = old })
	code, stdout, stderr := runDoctorJSON(t)
	var result struct {
		Healthy bool         `json:"healthy"`
		Lines   []doctorLine `json:"lines"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("doctor --json: code=%d stdout=%q stderr=%q: %v", code, stdout, stderr, err)
	}
	return doctorRun{code, result.Healthy, result.Lines, stdout, stderr}
}

func doctorText(run doctorRun) string {
	var text []string
	for _, line := range run.lines {
		text = append(text, line.Level+" "+line.Message)
	}
	return strings.Join(text, "\n")
}

func TestDoctorListsIgnoredContentAndStaysHealthy(t *testing.T) {
	h := newHarness(t)
	// A fixed probe environment: the three agents of the fake directory are installed and are the
	// only ones PATH reaches, so the verdict does not depend on the machine.
	h.installFake(false)
	t.Setenv("PATH", h.fakeBin)
	doc := forwardCompatDocument(t, true)
	doc["launcher"] = "foreground"
	dirty := runDoctorWith(t, h, doc)
	if dirty.stderr != "" {
		t.Fatalf("stderr with --json: %q", dirty.stderr)
	}
	joined := doctorText(dirty)
	for _, want := range []string{"future_top", "tui.future", "server.token", "models.kanban.newagent", "agents.badagent", "kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("doctor lines miss %q:\n%s", want, joined)
		}
	}
	// Ignored content never makes the installation unhealthy. In the fixed probe environment the
	// control (the same configuration without the ignored keys and the unusable entry, which
	// keeps the unknown references) and the configuration under test must both be healthy.
	twin := cleanTwin(t)
	twin["launcher"] = "foreground"
	twin["kanban_agent"], twin["chat_agent"] = "newagent", "newagent"
	twin["kanban_agents"] = map[string]any{"large": "newagent", "small": "codex"}
	twin["reviewers"].(map[string]any)["large"].(map[string]any)["PMQA"] = "newagent"
	twinRun := runDoctorWith(t, h, twin)
	if !twinRun.healthy {
		t.Fatalf("the probe environment is not healthy even without ignored content: code=%d\n%s", twinRun.code, doctorText(twinRun))
	}
	if !dirty.healthy || dirty.code != 0 {
		t.Fatalf("ignored content made the installation unhealthy: code=%d\n%s", dirty.code, joined)
	}
	// Unknown keys are info lines, an unusable agent and unknown references are warnings.
	levels := map[string]string{}
	for _, line := range dirty.lines {
		for _, key := range []string{"future_top", "agents.badagent", "reviewers.large.PMQA"} {
			if strings.Contains(line.Message, key) {
				levels[key] = line.Level
			}
		}
	}
	if levels["future_top"] != LevelInfo || levels["agents.badagent"] != LevelWarning || levels["reviewers.large.PMQA"] != LevelWarning {
		t.Fatalf("levels=%v\n%s", levels, joined)
	}
	if !slices.ContainsFunc(dirty.lines, func(l doctorLine) bool { return l.Level == LevelOK }) {
		t.Fatalf("doctor produced no ok line:\n%s", joined)
	}
}

func TestDoctorTextListsIgnoredContent(t *testing.T) {
	h := newHarness(t)
	doc := forwardCompatDocument(t, true)
	doc["launcher"] = "foreground"
	h.writeConfig(doc)
	code, stdout, stderr := commandtest.Capture(func() int { return commandtest.Run(NewDoctorCommand()) })
	if code != 0 && code != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	text := stdout + stderr
	for _, want := range []string{"future_top", "tui.future", "server.token", "models.kanban.newagent", "agents.badagent", "kanban_agent", "kanban_agents.large", "chat_agent", "reviewers.large.PMQA"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor text misses %q:\n%s", want, text)
		}
	}
}
