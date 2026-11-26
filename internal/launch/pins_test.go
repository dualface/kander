package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// pinCard adds header pin lines after RESULT.
func pinCard(t *testing.T, path, lines string) {
	t.Helper()
	text := mustRead(t, path)
	if !strings.Contains(text, "- RESULT:\n") {
		t.Fatalf("no RESULT line in %q", text)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(text, "- RESULT:\n", "- RESULT:\n"+lines, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func workingSpec(root, todoPath string) string {
	return filepath.Join(root, "working", filepath.Base(filepath.Dir(todoPath)), "spec.md")
}

func TestResolveExecutionUnpinnedMatchesConfiguration(t *testing.T) {
	cfg := envConfig("codex", "tmux", map[string]string{"large": "codex", "small": "claude"})
	cards := map[string]string{
		"current": "# Card\n\n- TYPE: Chore\n- SIZE: small\n- OWNER: claude\n- SESSION: claude abc\n- WINDOW: tmux:$1:@2:%3\n- RESULT:\n\n## GOAL\n\n- EXEC_AGENT: grok\n",
		"legacy":  "# 旧卡\n\n- 负责人: claude\n- 会话: claude abc\n- 窗口:\n- 开始时间:\n- 完成时间:\n",
		"no-ids":  "# 旧卡\n\n- 负责人:\n- 开始时间:\n- 完成时间:\n",
	}
	for name, text := range cards {
		for _, kind := range []string{"large", "small"} {
			scale := kindScale(kind)
			configured, _ := config.KanbanAgentFor(cfg, kind)
			for _, tc := range []struct {
				agent string
				mode  execMode
				want  string
			}{
				{"", execStart, configured},
				{"grok", execStart, "grok"},
				{"pi", execTakeover, "pi"},
				{"claude", execContinue, "claude"},
			} {
				choice, err := resolveExecution(cfg, text, kind, tc.agent, tc.mode)
				if err != nil {
					t.Fatalf("%s/%s/%v: %v", name, kind, tc.mode, err)
				}
				entry := cfg.Models.Kanban[tc.want]
				want := execChoice{Agent: tc.want, Model: config.KanbanModelFor(entry, scale), Effort: entry[scale+"_effort"]}
				if choice != want {
					t.Fatalf("%s/%s/%v: choice=%+v want %+v", name, kind, tc.mode, choice, want)
				}
				if choice.record != "" {
					t.Fatalf("%s: unpinned card gained a record: %q", name, choice.record)
				}
			}
		}
	}
}

func TestStartPinnedCardUsesCardValuesAndRecordsSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	cfg := envConfig("codex", "tmux", nil)

	fullID, fullPath := makeTodo(t, root, "pin-full")
	pinCard(t, fullPath, "- EXEC_AGENT: claude\n- EXEC_MODEL: my_model-1\n- EXEC_EFFORT: max\n")
	preview, err := PreviewStart(root, fullID)
	if err != nil || preview.Agent != "claude" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, _, err := capture(t, func() error { return commandStart(root, "", "", fullID) }); err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	if !strings.Contains(cmd, filepath.Join(fakeBin, "claude")) || !strings.Contains(cmd, "--model my_model-1 --effort max") {
		t.Fatalf("command=%s", cmd)
	}
	if card := mustRead(t, workingSpec(root, fullPath)); !strings.Contains(card, "- EXEC_EFFORT: max\n- EXEC_RESOLVED: agent=claude(forced) model=my_model-1(forced) effort=max(forced)\n") {
		t.Fatalf("card=%s", card)
	}

	partialID, partialPath := makeTodo(t, root, "pin-partial")
	pinCard(t, partialPath, "- EXEC_AGENT: claude\n")
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "", partialID) }); err != nil {
		t.Fatal(err)
	}
	entry := cfg.Models.Kanban["claude"]
	want := "- EXEC_RESOLVED: agent=claude(forced) model=" + config.KanbanModelFor(entry, "small") + "(config:small) effort=" + entry["small_effort"] + "(config:small)\n"
	if card := mustRead(t, workingSpec(root, partialPath)); !strings.Contains(card, want) {
		t.Fatalf("card=%s want %s", card, want)
	}

	// A card that pins only a reviewer still records how its executor was chosen.
	cliID, cliPath := makeTodo(t, root, "pin-cli")
	pinCard(t, cliPath, "- REVIEW_PMQA_AGENT: grok\n")
	if _, _, err := capture(t, func() error { return commandStart(root, "cursor", "", cliID) }); err != nil {
		t.Fatal(err)
	}
	cursor := cfg.Models.Kanban["cursor"]
	want = "- EXEC_RESOLVED: agent=cursor(cli) model=" + config.KanbanModelFor(cursor, "small") + "(config:small) effort=N/A\n"
	if card := mustRead(t, workingSpec(root, cliPath)); !strings.Contains(card, want) {
		t.Fatalf("card=%s want %s", card, want)
	}
}

func TestStartPinConflictsFailBeforeSideEffects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	cases := []struct {
		slug, pins, agent, want string
	}{
		{"cli-conflict", "- EXEC_AGENT: claude\n", "codex", "--agent"},
		{"model-without-agent", "- EXEC_MODEL: m1\n", "", "EXEC_AGENT"},
		{"effort-unsupported", "- EXEC_AGENT: cursor\n- EXEC_EFFORT: high\n", "", "EXEC_EFFORT"},
		{"model-unsupported", "- EXEC_AGENT: droid\n- EXEC_MODEL: m1\n", "", "EXEC_MODEL"},
		{"unknown-agent", "- EXEC_AGENT: my_agent\n", "", "my_agent"},
		{"option-like-model", "- EXEC_AGENT: claude\n- EXEC_MODEL: --yolo\n", "", "--yolo"},
		{"duplicate", "- EXEC_AGENT: claude\n- EXEC_AGENT: codex\n", "", "EXEC_AGENT"},
	}
	before := mustRead(t, filepath.Join(root, "tmux.log"))
	for _, tc := range cases {
		id, path := makeTodo(t, root, tc.slug)
		pinCard(t, path, tc.pins)
		original := mustRead(t, path)
		_, _, err := capture(t, func() error { return commandStart(root, tc.agent, "", id) })
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v", tc.slug, err)
		}
		if mustRead(t, path) != original {
			t.Fatalf("%s: card changed", tc.slug)
		}
	}
	if mustRead(t, filepath.Join(root, "tmux.log")) != before {
		t.Fatal("a rejected start reached the launcher")
	}
}

func TestExecutionRecordMarksAModelTheAgentDropsNotApplicable(t *testing.T) {
	cfg := envConfig("codex", "tmux", nil)
	cfg.Models.Kanban["droid"] = map[string]string{"large_model": "never-passed", "small_model": ""}
	text := "# Card\n\n- SIZE: large\n- RESULT:\n- EXEC_AGENT: droid\n\n## GOAL\n"
	choice, err := resolveExecution(cfg, text, "large", "", execStart)
	if err != nil {
		t.Fatal(err)
	}
	if choice.record != "agent=droid(forced) model=N/A effort=N/A" {
		t.Fatalf("record=%q", choice.record)
	}
}

func TestPinnedCustomAgentWithUnderscoreIsAccepted(t *testing.T) {
	cfg := envConfig("codex", "tmux", nil)
	cfg.Agents = map[string]config.AgentDefinition{"my_agent": {Path: "my-agent", Args: &config.AgentArgs{Start: []string{"--model", "{model}", "--effort", "{effort}"}}}}
	text := "# Card\n\n- SIZE: large\n- RESULT:\n- EXEC_AGENT: my_agent\n- EXEC_MODEL: org_model.v2\n- EXEC_EFFORT: high\n\n## GOAL\n"
	choice, err := resolveExecution(cfg, text, "large", "", execStart)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Agent != "my_agent" || choice.Model != "org_model.v2" || choice.Effort != "high" {
		t.Fatalf("choice=%+v", choice)
	}
	if choice.record != "agent=my_agent(forced) model=org_model.v2(forced) effort=high(forced)" {
		t.Fatalf("record=%q", choice.record)
	}
}
