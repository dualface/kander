package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const membershipTargetGroup = "20260920-target-group"

// writeGroupCard writes a directory card whose metadata and discussion bodies
// are given verbatim, so a test can produce ambiguous group ownership that the
// controlled update path would reject.
func writeGroupCard(t *testing.T, root, state, id, metadata, discussion string) {
	t.Helper()
	dir := filepath.Join(root, state, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "# " + id + "\n\n- TYPE: Chore\n- SIZE: small\n" + metadata +
		"\n## GOAL\n\ngoal\n\n## DISCUSSION\n\n" + discussion + "\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// memberOfTarget is a well-formed member of the group under test.
func memberOfTarget(t *testing.T, root, id string) {
	t.Helper()
	writeGroupCard(t, root, "todo", id, "- TASK_GROUP: "+membershipTargetGroup+"\n", "notes")
}

func TestErrForRulesOutCardsOfOtherGroups(t *testing.T) {
	resetLang(t)
	cases := []struct {
		name       string
		metadata   string
		discussion string
		blocks     bool
	}{
		{
			name:       "invalid syntax cannot name a legal group",
			metadata:   "- TASK_GROUP: not a group id\n",
			discussion: "notes",
		},
		{
			name:       "duplicate lines naming other groups",
			metadata:   "- TASK_GROUP: 20260920-other-group\n- TASK_GROUP: 20260920-third-group\n",
			discussion: "notes",
		},
		{
			name:       "duplicate lines naming the target group",
			metadata:   "- TASK_GROUP: 20260920-other-group\n- TASK_GROUP: " + membershipTargetGroup + "\n",
			discussion: "notes",
			blocks:     true,
		},
		{
			// TaskGroupFrom falls back to the discussion value when the metadata
			// value is empty, so an empty duplicate line still hides a member.
			name:       "duplicate empty lines with a legacy discussion group",
			metadata:   "- TASK_GROUP:\n- TASK_GROUP:\n",
			discussion: "TASK_GROUP: " + membershipTargetGroup,
			blocks:     true,
		},
		{
			name:       "duplicate empty lines with a legacy value of another group",
			metadata:   "- TASK_GROUP:\n- TASK_GROUP:\n",
			discussion: "TASK_GROUP: 20260920-other-group",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tempBoard(t)
			memberOfTarget(t, root, "20260920-member-one-task")
			memberOfTarget(t, root, "20260920-member-two-task")
			writeGroupCard(t, root, "backlog", "20260920-broken-task", testCase.metadata, testCase.discussion)

			scanned, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			membership := scanned.GroupMembership()
			if membership.ErrFor() == nil {
				t.Fatal("unscoped check lost the problem")
			}
			err = membership.ErrFor(membershipTargetGroup)
			if testCase.blocks {
				if err == nil {
					t.Fatal("ownership that could hide a member did not block")
				}
				return
			}
			if err != nil {
				t.Fatalf("unrelated card blocked the expansion: %v", err)
			}
			members := membership.Groups[membershipTargetGroup]
			if len(members) != 2 || members[0] != "20260920-member-one-task" || members[1] != "20260920-member-two-task" {
				t.Fatalf("members changed under narrowing: %v", members)
			}
		})
	}
}

func TestErrForKeepsUnknownOwnershipBlocking(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	memberOfTarget(t, root, "20260920-member-one-task")
	// A directory card without spec.md is a scan problem: its ownership cannot
	// be read, so it could still be a member of any group.
	if err := os.MkdirAll(filepath.Join(root, "backlog", "20260920-empty-task"), 0o755); err != nil {
		t.Fatal(err)
	}
	scanned, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	err = scanned.GroupMembership().ErrFor(membershipTargetGroup)
	if err == nil {
		t.Fatal("unknown ownership did not block")
	}
	if !strings.Contains(err.Error(), "20260920-empty-task") {
		t.Fatalf("diagnostics lost: %v", err)
	}
}

func TestErrForKeepsDuplicateTaskIDsBlocking(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	memberOfTarget(t, root, "20260920-member-one-task")
	writeGroupCard(t, root, "backlog", "20260920-copied-task", "- TASK_GROUP: 20260920-other-group\n", "notes")
	writeGroupCard(t, root, "todo", "20260920-copied-task", "- TASK_GROUP: 20260920-other-group\n", "notes")
	scanned, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	err = scanned.GroupMembership().ErrFor(membershipTargetGroup)
	if err == nil {
		t.Fatal("a duplicate task ID did not block")
	}
	if !strings.Contains(err.Error(), "20260920-copied-task") {
		t.Fatalf("diagnostics lost: %v", err)
	}
}
