package board

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// english renders the refusal texts in English so the tests can name the values.
func english(t *testing.T) {
	t.Helper()
	previous := config.CLILanguage()
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Cleanup(func() {
		if previous == "" {
			config.ApplyLanguageArgument([]string{"kander"})
			return
		}
		config.ApplyLanguageArgument([]string{"kander", "--lang", previous})
	})
}

// requireRefusal requires a board error of the given code whose text holds every
// part in want and none in absent.
func requireRefusal(t *testing.T, err error, code string, want []string, absent ...string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want board code %s, got %v", code, err)
	}
	for _, part := range want {
		if !strings.Contains(e.Message, part) {
			t.Fatalf("%q missing from %q", part, e.Message)
		}
	}
	for _, part := range absent {
		if strings.Contains(e.Message, part) {
			t.Fatalf("%q must not appear in %q", part, e.Message)
		}
	}
}

// R1: a close request names what differs from the view it closes.
func TestCloseRequestRefusal(t *testing.T) {
	english(t)
	root := tempBoard(t)
	id := gateCard(t, root, "close-refusal")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	good := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{}}
	if _, _, err = ReviewClosureEdges(v, good); err != nil {
		t.Fatal(err)
	}

	stale := good
	stale.ExpectedRevision = 41
	_, _, err = ReviewClosureEdges(v, stale)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"close target/revision/view CAS", "batch batch", "expected_revision is 41", "revision 1", "kander review aggregate"}, "view_hash is")

	other := good
	other.ViewHash = strings.Repeat("0", 64)
	_, _, err = ReviewClosureEdges(v, other)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"close target/revision/view CAS", "view_hash is " + other.ViewHash, "hashes to " + ReviewViewDigest(v), "stdout bytes"}, "expected_revision is")

	pasted := good
	pasted.ViewHash = "tok-pasted-by-mistake"
	_, _, err = ReviewClosureEdges(v, pasted)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"view_hash is <invalid>", "hashes to " + ReviewViewDigest(v)}, "tok-pasted-by-mistake")

	anyone := good
	anyone.BatchID = "another"
	_, _, err = ReviewClosureEdges(v, anyone)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"close target/revision/view CAS", "batch_id is another", "belongs to batch batch"})

	unsigned := good
	unsigned.Author = " "
	_, _, err = ReviewClosureEdges(v, unsigned)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{"close author required", "author must not be empty"}, "CAS")
}

// R2: a close the kernel refuses with no cause found names the false facts, or the
// state conditions that do not hold.
func TestCloseKernelRefusal(t *testing.T) {
	english(t)
	state := reviewplan.State{Planned: true, Count: 2, Closed: []bool{false, true}, Touched: make([]bool, 2)}
	allTrue := reviewplan.Close{Revision: true, ViewHash: true, Conclusions: true, Git: true, Current: true, PreviousClosed: true, Bound: true}

	facts := allTrue
	facts.Git, facts.Bound = false, false
	requireRefusal(t, closeRefusal("plan-1", "b0", 0, facts, state), "board.review_evidence_invalid", []string{"batch close refused", "close", "Git, Bound"}, "CAS", "plan state")

	requireRefusal(t, closeRefusal("plan-1", "b1", 1, allTrue, state), "board.review_evidence_invalid", []string{"batch close refused", "every fact true", "batch b1 is already closed"}, "CAS")
	requireRefusal(t, closeRefusal("plan-1", "b2", 2, allTrue, state), "board.review_evidence_invalid", []string{"batch position 2 is not below the batch count 2"}, "CAS")
	unplanned := state
	unplanned.Planned = false
	requireRefusal(t, closeRefusal("plan-1", "b0", 0, allTrue, unplanned), "board.review_evidence_invalid", []string{"plan plan-1 is not planned"}, "CAS")
}

// R9: seal and append refusals the shell found no cause for are no CAS refusals.
func TestSealAndAppendKernelRefusal(t *testing.T) {
	english(t)
	state := reviewplan.State{Planned: true, Sealed: true, Count: 3, Closed: make([]bool, 3), Touched: make([]bool, 3)}
	params := reviewplan.Params{MaxBatch: 3}

	requireRefusal(t, sealRefusal("plan-1", state, reviewplan.Seal{Cas: true, AllAssigned: true, NoDropOwed: true}), "board.review_evidence_invalid",
		[]string{"plan extension refused", "seal", "plan plan-1 is sealed"}, "CAS")
	requireRefusal(t, sealRefusal("plan-1", state, reviewplan.Seal{Cas: true, NoDropOwed: true}), "board.review_evidence_invalid",
		[]string{"AllAssigned"}, "CAS", "NoDropOwed")

	open := state
	open.Sealed = false
	full := reviewplan.Append{Cas: true, Binding: true, MembersActive: true, PreviousClosed: true, Requirements: true}
	requireRefusal(t, appendRefusal("plan-1", open, params, full), "board.review_evidence_invalid",
		[]string{"plan extension refused", "append", "the plan holds 3 batches and the kernel allows at most 3"}, "CAS", "sealed")
	requireRefusal(t, appendRefusal("plan-1", state, reviewplan.Params{MaxBatch: 5}, full), "board.review_evidence_invalid",
		[]string{"plan plan-1 is sealed"}, "CAS")

	gap := full
	gap.Binding, gap.PreviousClosed = false, false
	requireRefusal(t, appendRefusal("plan-1", open, params, gap), "board.review_evidence_invalid", []string{"Binding, PreviousClosed"}, "CAS", "allows at most")
	sealing := full
	sealing.Seal = true
	requireRefusal(t, appendRefusal("plan-1", open, params, sealing), "board.review_evidence_invalid", []string{"AllAssigned, NoDropOwed"}, "CAS")
}

// R10: a rebind request names the members it omits, the ones it names without a
// change, and the ones with another cycle.
func TestRebindRefusal(t *testing.T) {
	english(t)
	d1, d2, d3, d4, d5 := strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64), strings.Repeat("4", 64), strings.Repeat("5", 64)
	a, b, c, d := "20260101-a-task", "20260101-b-task", "20260101-c-task", "20260101-d-task"
	changed := map[string]string{a: d1, b: d2, c: d3}
	requested := map[string]string{a: d1, c: d4, d: d5}
	err := rebindRefusal(changed, requested)
	requireRefusal(t, err, "board.review_evidence_invalid", []string{
		"cycle rebind CAS requires exactly all changed members",
		"copy rebind_cycles",
		b + "=" + d2,
		"did not change: " + d,
		c + ": " + d4 + ", " + d3,
		"kander review progress",
	}, a+"=")

	// A requested cycle that is no digest is not written back.
	err = rebindRefusal(map[string]string{a: d1}, map[string]string{a: "tok-pasted-by-mistake"})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{a + ": <invalid>, " + d1}, "tok-pasted-by-mistake")
}

// R22: a review over a member that is not in working or review names the member
// and its state, and offers remove-member only to a card of a group that has not
// started.
func TestMemberStateRefusal(t *testing.T) {
	english(t)
	for _, tc := range []struct {
		state, group string
		remove       bool
		wait         bool
	}{
		{"backlog", "group-1", true, false},
		{"todo", "group-1", true, false},
		{"todo", "", false, true},
		{"done", "group-1", false, false},
		{"archived", "group-1", false, false},
		{"trash", "", false, false},
	} {
		t.Run(tc.state+"/"+tc.group, func(t *testing.T) {
			err := memberStateRefusal("task-m", tc.state, tc.group)
			want := []string{"task-m: working/review", "member task-m is in " + tc.state}
			var absent []string
			if tc.remove {
				want = append(want, "wait until task-m starts", "kander coordinator remove-member", "rules/snippets/commands/coordinator.md")
			} else {
				absent = append(absent, "remove-member")
			}
			if tc.wait {
				want = append(want, "wait until task-m starts")
			}
			requireRefusal(t, err, "board.review_evidence_invalid", want, absent...)
		})
	}
}

// R22 through the entrance: a plan over a card that has not started names it.
func TestReviewCardsRefusesMemberOutsideWorkingAndReview(t *testing.T) {
	english(t)
	root := tempBoard(t)
	s := transactionCard(t, root, "not-started", false)
	err := WithTransaction(root, reviewScope([]string{s.Entry.TaskID}, true), func(tx *Transaction) error {
		_, e := reviewCards(tx, []string{s.Entry.TaskID}, "en", "")
		return e
	})
	requireRefusal(t, err, "board.review_evidence_invalid", []string{s.Entry.TaskID + ": working/review", "member " + s.Entry.TaskID + " is in backlog"}, "remove-member")
}

func num(v uint64) string { return strconv.FormatUint(v, 10) }
