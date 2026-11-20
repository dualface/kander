package board

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestValidIntegrationTarget(t *testing.T) {
	for _, value := range []string{"v2", "release/1.4", "feature-x.y", "a_b", "9", strings.Repeat("a", 100)} {
		if !ValidIntegrationTarget(value) {
			t.Errorf("rejected %q", value)
		}
	}
	for _, value := range []string{"", "HEAD", "refs/heads/v2", "group/x", "-x", ".x", "a..b", "a.lock", "a.lock/b", "a/", "a.", "a/b.", "a b", "a@{1}", "a~1", "a:b", "/a", "a//b", "a\nb", strings.Repeat("a", 101)} {
		if ValidIntegrationTarget(value) {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestIntegrationTargetOfReadsOnlyTheHeader(t *testing.T) {
	header := "# Card\n\n- TYPE: Chore\n- SIZE: small\n- TASK_BRANCH: x\n"
	for _, tc := range []struct {
		name, text, want string
		fails            bool
	}{
		{"none", header + "\n## GOAL\n", "", false},
		{"empty", header + "- INTEGRATION_TARGET:\n\n## GOAL\n", "", false},
		{"set", header + "- INTEGRATION_TARGET: v2\n\n## GOAL\n", "v2", false},
		{"crlf", strings.ReplaceAll(header+"- INTEGRATION_TARGET: v2\n\n## GOAL\n", "\n", "\r\n"), "v2", false},
		{"body line only", header + "\n## GOAL\n\n- INTEGRATION_TARGET: v2\n- INTEGRATION_TARGET: !!\n", "", false},
		{"duplicate", header + "- INTEGRATION_TARGET: v2\n- INTEGRATION_TARGET: v2\n\n## GOAL\n", "", true},
		{"empty duplicate", header + "- INTEGRATION_TARGET:\n- INTEGRATION_TARGET: v2\n\n## GOAL\n", "", true},
		{"invalid", header + "- INTEGRATION_TARGET: group/x\n\n## GOAL\n", "", true},
	} {
		got, err := IntegrationTargetOf(tc.text)
		if (err != nil) != tc.fails || got != tc.want {
			t.Errorf("%s: got %q, %v", tc.name, got, err)
		}
	}
}

func TestNewCardsCarryNoIntegrationTarget(t *testing.T) {
	root := tempBoard(t)
	for _, large := range []bool{false, true} {
		s := transactionCard(t, root, map[bool]string{false: "no-target-small", true: "no-target-large"}[large], large)
		if strings.Contains(s.Text, FieldIntegrationTarget) {
			t.Fatalf("template writes the field:\n%s", s.Text)
		}
	}
}

func TestFrozenContractGainsTheTargetKeyOnlyWhenSet(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "frozen-keys", false)
	before := frozenContract(s.Text)
	if _, ok := before[FieldIntegrationTarget]; ok {
		t.Fatal("a card with no target field gained the key")
	}
	empty := strings.Replace(s.Text, "- RESULT:\n", "- RESULT:\n- INTEGRATION_TARGET:\n", 1)
	if !reflect.DeepEqual(before, frozenContract(empty)) {
		t.Fatal("an empty field changed the frozen record")
	}
	set := strings.Replace(s.Text, "- RESULT:\n", "- RESULT:\n- INTEGRATION_TARGET: v2\n", 1)
	after := frozenContract(set)
	if after[FieldIntegrationTarget] != "v2" {
		t.Fatalf("frozen record %v", after)
	}
	delete(after, FieldIntegrationTarget)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("setting the field changed another key")
	}
}

func withTarget(text, target string) string {
	return strings.Replace(text, "- RESULT:\n", "- RESULT:\n- INTEGRATION_TARGET: "+target+"\n", 1)
}

func TestUpdateValidatesTheTargetField(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "target-update", false)
	update := func(text string) error {
		return UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision})
	}
	for _, text := range []string{
		withTarget(s.Text, "group/x"),
		withTarget(s.Text, "HEAD"),
		withTarget(withTarget(s.Text, "v2"), "v2"),
	} {
		if err := update(text); err == nil {
			t.Fatalf("accepted a card header with %q", text[:strings.Index(text, "\n## ")])
		}
		if got := transactionSnapshot(t, root, s.Entry.TaskID); got.Text != s.Text || got.Revision != s.Revision {
			t.Fatal("a rejected update changed the card")
		}
	}
	// A card that never entered todo takes the field without a decision.
	if err := update(withTarget(s.Text, "v2")); err != nil {
		t.Fatal(err)
	}
	if got := transactionSnapshot(t, root, s.Entry.TaskID); !strings.Contains(got.Text, "- INTEGRATION_TARGET: v2\n") || strings.Contains(got.Text, "CONTRACT_DECISIONS") {
		t.Fatalf("field not written plainly:\n%s", got.Text)
	}
}

func TestTargetFieldFreezesWithTheContract(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "target-freeze", false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	id := todo.TaskID
	update := func(text, decision string) error {
		cur := transactionSnapshot(t, root, id)
		return UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: cur.Revision, ContractDecision: decision})
	}
	cur := func() string { return transactionSnapshot(t, root, id).Text }
	check := func(label string) {
		t.Helper()
		if err := update(withTarget(cur(), "v2"), ""); err == nil {
			t.Fatalf("%s: added without a decision", label)
		}
		if err := update(withTarget(cur(), "v2"), "user decision: v2"); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if err := update(strings.Replace(cur(), "- INTEGRATION_TARGET: v2", "- INTEGRATION_TARGET: v3", 1), ""); err == nil {
			t.Fatalf("%s: modified without a decision", label)
		}
		if err := update(strings.Replace(cur(), "- INTEGRATION_TARGET: v2\n", "", 1), ""); err == nil {
			t.Fatalf("%s: deleted without a decision", label)
		}
		if err := update(strings.Replace(cur(), "- INTEGRATION_TARGET: v2", "- INTEGRATION_TARGET: v3", 1), "user decision: v3"); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if err := update(strings.Replace(cur(), "- INTEGRATION_TARGET: v3\n", "", 1), "user decision: none"); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		text := cur()
		for _, want := range []string{`"INTEGRATION_TARGET": "v2"`, `"INTEGRATION_TARGET": "v3"`} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: decision record lacks %s", label, want)
			}
		}
		if strings.Contains(text, "- INTEGRATION_TARGET") {
			t.Fatalf("%s: field still in the header", label)
		}
	}
	check("todo")
	// A card withdrawn to backlog stays frozen.
	s = transactionSnapshot(t, root, id)
	if _, err = MoveEntry(s.Entry, root, "backlog"); err != nil {
		t.Fatal(err)
	}
	check("backlog after todo")
}

func TestTodoGateAndPickRejectABadTargetField(t *testing.T) {
	root := tempBoard(t)
	for slug, text := range map[string]func(string) string{
		"gate-invalid": func(s string) string { return withTarget(s, "refs/heads/v2") },
		"gate-duplicate": func(s string) string {
			return strings.Replace(s, "- RESULT:\n", "- RESULT:\n- INTEGRATION_TARGET: v2\n- INTEGRATION_TARGET: v3\n", 1)
		},
	} {
		s := transactionCard(t, root, slug, false)
		bad := text(readyText(s))
		// The update gate refuses these too, so the card is written like a hand edit.
		if err := fixtureWrite(root, s.Entry, bad); err != nil {
			t.Fatal(err)
		}
		s = transactionSnapshot(t, root, s.Entry.TaskID)
		if _, err := MoveEntry(s.Entry, root, "todo"); err == nil {
			t.Fatalf("%s: moved to todo", slug)
		}
		if got := transactionSnapshot(t, root, s.Entry.TaskID); got.Entry.State != "backlog" {
			t.Fatalf("%s: card is in %s", slug, got.Entry.State)
		}
	}
	ok := transactionCard(t, root, "gate-valid", false)
	if err := fixtureWrite(root, ok.Entry, withTarget(readyText(ok), "release/1.4")); err != nil {
		t.Fatal(err)
	}
	ok = transactionSnapshot(t, root, ok.Entry.TaskID)
	if _, err := MoveEntry(ok.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
}

// groupCard puts a ready card into todo with a task group and a target field.
func groupCard(t *testing.T, root, slug, group, target string) string {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := strings.Replace(readyText(s), "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	if target != "" {
		text = withTarget(text, target)
	}
	if err := fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	moved, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	return moved.TaskID
}

func checkProblems(t *testing.T, root string, ids ...string) (int, string) {
	t.Helper()
	code, _, stderr, err := CheckBoard(root, ids, false)
	if err != nil {
		t.Fatal(err)
	}
	return code, strings.Join(stderr, "\n")
}

func TestCheckReportsGroupTargetMismatch(t *testing.T) {
	root := tempBoard(t)
	const group = "20260901-target-group"
	first := groupCard(t, root, "group-first", group, "v2")
	second := groupCard(t, root, "group-second", group, "v2")
	if code, out := checkProblems(t, root); code != 0 {
		t.Fatalf("a consistent group was reported: %s", out)
	}
	// Mixed values and a member with no field are each one problem for the group.
	for label, edit := range map[string]func(string) string{
		"other value": func(s string) string {
			return strings.Replace(s, "- INTEGRATION_TARGET: v2", "- INTEGRATION_TARGET: v3", 1)
		},
		"no field": func(s string) string { return strings.Replace(s, "- INTEGRATION_TARGET: v2\n", "", 1) },
	} {
		entry := transactionSnapshot(t, root, second)
		if err := fixtureWrite(root, entry.Entry, edit(entry.Text)); err != nil {
			t.Fatal(err)
		}
		code, out := checkProblems(t, root)
		if code == 0 || strings.Count(out, "INTEGRATION_TARGET") != 1 || !strings.Contains(out, group) || !strings.Contains(out, first) {
			t.Fatalf("%s: code=%d output=%q", label, code, out)
		}
		// Naming one member still finds the mismatch with its sibling.
		if code, out = checkProblems(t, root, first); code == 0 || !strings.Contains(out, group) {
			t.Fatalf("%s: single-member check code=%d output=%q", label, code, out)
		}
		if err := fixtureWrite(root, entry.Entry, entry.Text); err != nil {
			t.Fatal(err)
		}
	}
	if code, out := checkProblems(t, root, first); code != 0 {
		t.Fatalf("restored group reported: %s", out)
	}
}

func TestCheckReportsDuplicateAndInvalidTargets(t *testing.T) {
	root := tempBoard(t)
	id := groupCard(t, root, "single-target", "", "v2")
	entry := transactionSnapshot(t, root, id)
	for label, text := range map[string]string{
		"duplicate": strings.Replace(entry.Text, "- INTEGRATION_TARGET: v2\n", "- INTEGRATION_TARGET: v2\n- INTEGRATION_TARGET: v2\n", 1),
		"invalid":   strings.Replace(entry.Text, "- INTEGRATION_TARGET: v2", "- INTEGRATION_TARGET: group/v2", 1),
	} {
		if err := fixtureWrite(root, entry.Entry, text); err != nil {
			t.Fatal(err)
		}
		if code, out := checkProblems(t, root); code == 0 || strings.Count(out, "INTEGRATION_TARGET") != 1 {
			t.Fatalf("%s: code=%d output=%q", label, code, out)
		}
	}
	// A line with the same spelling in the body is ordinary text.
	body := strings.Replace(entry.Text, "- INTEGRATION_TARGET: v2\n", "", 1)
	body = strings.Replace(body, "## DISCUSSION\n", "## DISCUSSION\n\n- INTEGRATION_TARGET: group/v2\n- INTEGRATION_TARGET: x\n", 1)
	if err := fixtureWrite(root, entry.Entry, body); err != nil {
		t.Fatal(err)
	}
	if code, out := checkProblems(t, root); code != 0 {
		t.Fatalf("body lines reported: %s", out)
	}
}

// bindTargetFixture returns a board whose review card has a sealed N/A plan and a
// wrap-up intent, as bindWrapUpFixture builds it, for a card with the given
// target field.
func bindTargetFixture(t *testing.T, slug, target string, bodyLine bool) (string, DispatchInput) {
	t.Helper()
	root := tempBoard(t)
	s := dispatchCard(t, root, slug)
	text := s.Text
	if target != "" {
		text = regexp.MustCompile(`(?m)^(- TASK_BRANCH:.*)$`).ReplaceAllString(text, "$1\n- INTEGRATION_TARGET: "+target)
	}
	if bodyLine {
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\n- INTEGRATION_TARGET: v2\n", 1)
	}
	if err := fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	in := dispatchInput(transactionSnapshot(t, root, s.Entry.TaskID), slug)
	bindWrapUpFixture(t, root, &in)
	return root, in
}

func TestWrapUpStructuralCheckFollowsTheCardTarget(t *testing.T) {
	const v2Local, v2Origin = "refs/heads/v2", "refs/remotes/origin/v2"
	for _, tc := range []struct {
		name, target string
		bodyLine     bool
		accepted     []string
		rejected     []string
	}{
		{"target v2", "v2", false, []string{v2Local, v2Origin}, []string{"refs/heads/develop", "refs/remotes/origin/main", "refs/heads/v3", "refs/heads/group/v2"}},
		{"no field", "", false, []string{"refs/heads/develop", "refs/remotes/origin/main"}, []string{v2Local, v2Origin}},
		{"body line is text", "", true, []string{"refs/heads/develop"}, []string{v2Local}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, ref := range tc.rejected {
				root, in := bindTargetFixture(t, "reject-"+tc.name[:2]+string(rune('a'+i)), tc.target, tc.bodyLine)
				in.Evidence.WrapUp.Git.TargetRef = ref
				if _, err := PrepareDispatch(root, in); err == nil {
					t.Errorf("accepted %s", ref)
				}
			}
			for i, ref := range tc.accepted {
				root, in := bindTargetFixture(t, "accept-"+tc.name[:2]+string(rune('a'+i)), tc.target, tc.bodyLine)
				in.Evidence.WrapUp.Git.TargetRef = ref
				if _, err := PrepareDispatch(root, in); err != nil {
					t.Errorf("rejected %s: %v", ref, err)
				}
				// A completed round (historical) is held to the same rule.
				if err := WithTransaction(root, LockScope{Tasks: []string{in.TaskID}, Groups: []string{reviewControlGroup}, ReadOnly: true}, func(tx *Transaction) error {
					return validateDispatchWrapUp(tx, in, false, true)
				}); err != nil {
					t.Errorf("historical check rejected %s: %v", ref, err)
				}
			}
			for i, ref := range tc.rejected {
				root, in := bindTargetFixture(t, "history-"+tc.name[:2]+string(rune('a'+i)), tc.target, tc.bodyLine)
				in.Evidence.WrapUp.Git.TargetRef = ref
				err := WithTransaction(root, LockScope{Tasks: []string{in.TaskID}, Groups: []string{reviewControlGroup}, ReadOnly: true}, func(tx *Transaction) error {
					return validateDispatchWrapUp(tx, in, false, true)
				})
				if err == nil || !strings.Contains(err.Error(), "integration binding") {
					t.Errorf("historical check accepted %s: %v", ref, err)
				}
			}
		})
	}
}

func TestIntegrationTargetRefTable(t *testing.T) {
	for ref, want := range map[string]bool{"refs/remotes/upstream/v2": false, "v2": false, "refs/heads/v2/x": false} {
		if got := integrationTargetRef(ref, "v2"); got != want {
			t.Errorf("v2 target, %s: %v", ref, got)
		}
	}
	if !integrationTargetRef("refs/heads/main", "") {
		t.Error("an empty target must keep the mainline rule")
	}
}

// The orchestrator checks a group while its cards are still in backlog, and a
// named card is checked whatever its state; both must see the new problems.
func TestCheckSeesTargetsOfBacklogAndNamedCards(t *testing.T) {
	root := tempBoard(t)
	const group = "20260901-backlog-group"
	var ids []string
	for i, target := range []string{"v2", ""} {
		s := transactionCard(t, root, "backlog-member-"+string(rune('a'+i)), false)
		text := strings.Replace(s.Text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		if target != "" {
			text = withTarget(text, target)
		}
		if err := fixtureWrite(root, s.Entry, text); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.Entry.TaskID)
	}
	if code, out := checkProblems(t, root, ids[0]); code == 0 || !strings.Contains(out, group) {
		t.Fatalf("backlog mismatch: code=%d output=%q", code, out)
	}
	// A named card is checked for its own field too.
	s := transactionSnapshot(t, root, ids[1])
	text := strings.Replace(s.Text, "- RESULT:\n", "- RESULT:\n- INTEGRATION_TARGET: group/x\n", 1)
	if err := fixtureWrite(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	if code, out := checkProblems(t, root, ids[1]); code == 0 || !strings.Contains(out, "INTEGRATION_TARGET") {
		t.Fatalf("named card: code=%d output=%q", code, out)
	}
}
