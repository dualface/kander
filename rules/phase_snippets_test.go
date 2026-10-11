package rules

import (
	"regexp"
	"strings"
	"testing"
)

// The phase snippets this card moved out of the kanban and git rules.
var phaseSnippets = []string{
	"snippets/execute-single.md", "snippets/orchestrate-sessions.md", "snippets/failure-recovery.md",
	"snippets/coordinator-checkpoints.md", "snippets/review-evidence-gate.md", "snippets/git-sync.md",
	"snippets/git-integrate.md", "snippets/report-orchestration.md",
	"snippets/execute-group-member.md", "snippets/orchestrate-start.md", "snippets/orchestrate-review.md",
	"snippets/orchestrate-integrate.md", "snippets/self-executed-group.md", "snippets/fix-round.md", "snippets/wrap-up.md",
}

func words(t *testing.T, name string) int {
	t.Helper()
	data, err := File(name)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func TestPhaseSnippetsExistWithSource(t *testing.T) {
	for _, name := range phaseSnippets {
		data, err := File(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		if !strings.HasPrefix(lines[0], "# ") {
			t.Fatalf("%s must start with a level-one heading: %q", name, lines[0])
		}
		if !regexp.MustCompile("(?m)^Source: moved from `KANDER-[A-Z-]+\\.md` \"[^\"]+\"").MatchString(string(data)) {
			t.Fatalf("%s lacks a Source line naming the file and section it came from", name)
		}
	}
}

func TestSplitRuleFilesStayShort(t *testing.T) {
	for name, limit := range map[string]int{"KANDER-KANBAN-RULES.md": 4000, "KANDER-GIT-RULES.md": 900, "KANDER-LOADING-RULES.md": 1000, "KANDER-TASK-GROUP-RULES.md": 2000} {
		if n := words(t, name); n > limit {
			t.Fatalf("%s has %d words, limit %d", name, n, limit)
		}
	}
	kanban, _ := File("KANDER-KANBAN-RULES.md")
	for _, title := range []string{"## Claiming, Starting, and Coordination", "## Orchestrator Sessions", "## Execution and Completion", "## Termination and Cleanup", "## Failure Recovery", "## Coordinator Checkpoints", "## Review Evidence Completion Gate"} {
		if strings.Contains(string(kanban), title) {
			t.Fatalf("KANDER-KANBAN-RULES.md still carries %s", title)
		}
	}
	git, _ := File("KANDER-GIT-RULES.md")
	for _, title := range []string{"## Keeping a Task Branch Current", "## Integration and Cleanup"} {
		if strings.Contains(string(git), title) {
			t.Fatalf("KANDER-GIT-RULES.md still carries %s", title)
		}
	}
	loading, _ := File("KANDER-LOADING-RULES.md")
	text := string(loading)
	if strings.Contains(text, "Reading Map "+"by Role") || !strings.Contains(text, "## Phase Snippets") {
		t.Fatal("KANDER-LOADING-RULES.md must replace the reading map with the phase snippet list")
	}
	if !strings.Contains(text, "instance facts") || !strings.Contains(text, "the snippets") {
		t.Fatal("Rule Precedence must place Kander prompts: instance facts from the prompt, protocol from rules and snippets")
	}
	for _, name := range phaseSnippets {
		if !strings.Contains(text, "`"+name+"`") {
			t.Fatalf("Phase Snippets does not list %s", name)
		}
	}
	if strings.Contains(text, "(pending)") {
		t.Fatal("Phase Snippets must list no pending rows")
	}
	group, _ := File("KANDER-TASK-GROUP-RULES.md")
	for _, title := range []string{"## Git Branches and Worktrees", "## Group Integration Branch", "## Task Orchestration", "### Self-Executed Groups", "### Starting and Subscribing", "### Review Batches and Dispatch-Back", "### Integration and Wrap-Up", "## Machine Aggregation and Author Boundaries"} {
		if strings.Contains(string(group), title) {
			t.Fatalf("KANDER-TASK-GROUP-RULES.md still carries %s", title)
		}
	}
	review, _ := File("KANDER-REVIEW-RULES.md")
	for _, title := range []string{"## Group-Level Review for Task Groups", "**Verification Split Between Orchestrator and Executing Agent**", "## Main Agent Verification Duty", "## Controlled Plans, Author Dispositions, and Batch Closure", "## Evidence Command JSON"} {
		if strings.Contains(string(review), title) {
			t.Fatalf("KANDER-REVIEW-RULES.md still carries %s", title)
		}
	}
	member, _ := File("snippets/execute-group-member.md")
	if !strings.Contains(string(member), "`snippets/orchestrate-start.md`") || !strings.Contains(string(member), "`snippets/execute-single.md`") {
		t.Fatal("execute-group-member must point at execute-single and at orchestrate-start for the orchestrator entry")
	}
	start, _ := File("snippets/orchestrate-start.md")
	if !strings.Contains(string(start), "`snippets/orchestrate-sessions.md`") {
		t.Fatal("orchestrate-start must build on orchestrate-sessions")
	}
	for _, name := range Names() {
		if strings.HasPrefix(name, "snippets/") || strings.HasPrefix(name, "templates/") {
			continue
		}
		data, _ := File(name)
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "\"Self-Executed Groups\"") && !strings.Contains(line, "snippets/self-executed-group.md") {
				t.Fatalf("%s references Self-Executed Groups without the snippet path: %s", name, line)
			}
		}
	}
}

func TestEntryKeepsLoadingStepAndAddsReadSentence(t *testing.T) {
	agents, _ := File("KANDER-AGENTS.md")
	text := string(agents)
	if !strings.Contains(text, "3. Read `KANDER-LOADING-RULES.md`") {
		t.Fatal("step 3 must still send the session to KANDER-LOADING-RULES.md")
	}
	if !strings.Contains(text, "\n4. Before running any `kander` command in this session") {
		t.Fatal("step 4 must carry the conditional read sentence")
	}
}

func TestOrchestrationSummaryTemplateIsASnippet(t *testing.T) {
	snippet, err := File("snippets/report-orchestration.md")
	if err != nil {
		t.Fatal(err)
	}
	fields := 0
	for _, line := range strings.Split(string(snippet), "\n") {
		if strings.HasPrefix(line, "- ") {
			fields++
		}
	}
	if fields != 9 || !strings.Contains(string(snippet), "# Kanban Orchestration Summary") {
		t.Fatalf("orchestration summary template must keep its nine fields: %d", fields)
	}
	reporting, _ := File("KANDER-REPORTING-RULES.md")
	if strings.Contains(string(reporting), "# Kanban Orchestration Summary") || !strings.Contains(string(reporting), "`snippets/report-orchestration.md`") {
		t.Fatal("KANDER-REPORTING-RULES.md must point to the snippet instead of carrying the block")
	}
}
