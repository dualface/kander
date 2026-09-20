package launch

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// orchestrateGroupBoard builds a board holding a two-member group plus one card
// whose task group ownership is broken in the given way.
func orchestrateGroupBoard(t *testing.T, brokenMetadata, brokenDiscussion string) (string, string) {
	t.Helper()
	root, _, _ := setupBoard(t)
	loadEffective = func() (*config.Config, error) {
		return envConfig("codex", "tmux", map[string]string{"large": "grok", "small": "codex"}), nil
	}
	group := time.Now().Format("20060102") + "-orchestrate-group"
	for _, slug := range []string{"orchestrate-member-a", "orchestrate-member-b"} {
		_, document := makeBacklog(t, root, slug, true)
		replaceInFile(t, document, "- TASK_GROUP:", "- TASK_GROUP: "+group)
	}
	_, broken := makeBacklog(t, root, "orchestrate-broken", true)
	replaceInFile(t, broken, "- TASK_GROUP:", strings.ReplaceAll(brokenMetadata, "{{g}}", group))
	if brokenDiscussion != "" {
		replaceInFile(t, broken, "## DISCUSSION", "## DISCUSSION\n\n"+strings.ReplaceAll(brokenDiscussion, "{{g}}", group))
	}
	return root, group
}

func TestStartOrchestratorIgnoresCardsOfOtherGroups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	cases := []struct {
		name       string
		metadata   string
		discussion string
	}{
		{name: "invalid syntax", metadata: "- TASK_GROUP: not a group id"},
		{
			name:     "duplicate lines of other groups",
			metadata: "- TASK_GROUP: 20260101-other-group\n- TASK_GROUP: 20260101-third-group",
		},
		{
			name:       "duplicate empty lines with a legacy value of another group",
			metadata:   "- TASK_GROUP:\n- TASK_GROUP:",
			discussion: "TASK_GROUP: 20260101-other-group",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root, group := orchestrateGroupBoard(t, testCase.metadata, testCase.discussion)
			loaded, err := board.LoadBoard(root)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.GroupMembership().ErrFor() == nil {
				t.Fatal("the card under test is not actually broken")
			}
			tasks, _, err := resolveOrchestrateTasks(loaded, []string{group})
			if err != nil {
				t.Fatalf("an unrelated broken card blocked the plan: %v", err)
			}
			if len(tasks) != 2 {
				t.Fatalf("members changed under narrowing: %+v", tasks)
			}
			for _, task := range tasks {
				if task.TaskGroup != group {
					t.Fatalf("unexpected member: %+v", task)
				}
			}
		})
	}
}

func TestStartOrchestratorStillBlocksOnOwnershipItCannotRuleOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	cases := []struct {
		name       string
		metadata   string
		discussion string
	}{
		{
			name:     "duplicate lines naming the group",
			metadata: "- TASK_GROUP: 20260101-other-group\n- TASK_GROUP: {{g}}",
		},
		{
			// The discussion value is what TaskGroupFrom resolves when the
			// metadata lines are empty, so this card may still be a member.
			name:       "duplicate empty lines with a legacy value of the group",
			metadata:   "- TASK_GROUP:\n- TASK_GROUP:",
			discussion: "TASK_GROUP: {{g}}",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root, group := orchestrateGroupBoard(t, testCase.metadata, testCase.discussion)
			loaded, err := board.LoadBoard(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = resolveOrchestrateTasks(loaded, []string{group}); err == nil {
				t.Fatal("ownership that could hide a member did not block")
			}
		})
	}
}
