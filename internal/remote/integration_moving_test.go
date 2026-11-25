package remote_test

import (
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/remote"
)

// movingFixture starts from a target the reviewed commit has diverged from, so
// the first attempt generates a merge, and returns the foreign target.
func movingFixture(t *testing.T) (*integrationFixture, string) {
	t.Helper()
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	return f, f.advanceTarget("first.txt")
}

// moveOnce runs one foreign push at the n-th call of a kind, counted from the
// first call after `after` was seen (or from the start when after is empty).
func (f *integrationFixture) moveOnce(after, kind string, n int, name string) {
	seen, count := after == "", 0
	f.watch(func(call string) int {
		if call == after {
			seen = true
		}
		if seen && call == kind {
			count++
			if count == n {
				f.foreignPush(name)
			}
		}
		return 0
	})
}

// assertRestarted checks the common outcome of a target that moved while an
// attempt was in flight: the first attempt is retired for the move with its
// lease released and nothing of it on the target, and a second attempt merged
// the moved target.
func (f *integrationFixture) assertRestarted(operation string, pushes int, result api.Integration) {
	f.t.Helper()
	attempts := f.attempts(operation)
	if len(attempts) != 2 || !attempts[0].Retired || attempts[0].RetiredFor != "target_moved" || !attempts[0].Request.ViaAutoMerge || attempts[0].PushAttempts != pushes {
		f.t.Fatalf("attempts: %+v", attempts)
	}
	abandoned, final := attempts[0].Request, f.targetHead()
	if result.Request.ExecutionID != attempts[1].Request.ExecutionID || result.ObservedTarget != final || result.Request.ViaCommit != final {
		f.t.Fatalf("receipt: %+v", result.Request)
	}
	if got := f.parents(final); len(got) != 2 || got[1] != f.commit || got[0] == abandoned.ExpectedTarget {
		f.t.Fatalf("the new attempt did not merge the moved target: %v", got)
	}
	previous, err := f.b.Project.IntegrationExecution(f.t.Context(), api.IntegrationID("refs/heads/v2"), abandoned.ExecutionID)
	if err != nil || previous.Status != api.ClaimReleased {
		f.t.Fatalf("the abandoned attempt kept its lease: %+v %v", previous, err)
	}
	credentials, err := remote.Credentials(f.a.Home)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, credential := range credentials {
		if credential.ExecutionID == abandoned.ExecutionID {
			f.t.Fatalf("the abandoned attempt kept its credential: %+v", credential)
		}
	}
	if history := deliveryGit(f.t, f.first, "rev-list", final); strings.Contains(history, abandoned.ViaCommit) {
		f.t.Fatal("the abandoned merge reached the target")
	}
}

func TestIntegrationRestartsWhenTheTargetMovesDuringPreparation(t *testing.T) {
	f, _ := movingFixture(t)
	f.moveOnce("", "observation", 1, "second.txt")
	options := f.options("moves-during-preparation")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	// The preparation that saw the move never became an attempt.
	attempts := f.attempts(options.OperationID)
	if len(attempts) != 1 || attempts[0].Retired || !result.Request.ViaAutoMerge || f.targetHead() != result.Request.ViaCommit {
		t.Fatalf("attempts: %+v receipt: %+v", attempts, result.Request)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("the abandoned preparation kept its merge ref: %s", refs)
	}
	if got := f.parents(result.Request.ViaCommit); len(got) != 2 || got[1] != f.commit || deliveryGit(t, f.first, "show", result.Request.ViaCommit+":second.txt") == "" {
		t.Fatalf("the new attempt did not merge the moved target: %v", got)
	}
	f.complete(result)
}

func TestIntegrationTargetThatMovedToTheReviewedCommitNeedsNoPush(t *testing.T) {
	f, _ := movingFixture(t)
	// The target is replaced by the reviewed commit itself while the attempt
	// prepares, so the next attempt finds it contained.
	moved := false
	f.watch(func(kind string) int {
		if kind == "observation" && !moved {
			moved = true
			deliveryGit(t, f.first, "push", "--force", "origin", f.commit+":refs/heads/v2")
		}
		return 0
	})
	result, err := f.a.Integrate(t.Context(), f.first, f.options("moves-to-reviewed"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Containment != api.IntegrationContained || !result.Request.AlreadyContained || result.Request.ViaAutoMerge || f.targetHead() != f.commit {
		t.Fatalf("receipt: %+v", result.Request)
	}
	f.complete(result)
}

func TestIntegrationRestartsWhenThePushIntentSeesAMovedTarget(t *testing.T) {
	f, _ := movingFixture(t)
	// The second observation belongs to the push intent: the target moves
	// after the attempt looked and before the intent looks again.
	f.moveOnce("", "observation", 2, "second.txt")
	options := f.options("moves-before-intent")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	f.assertRestarted(options.OperationID, 0, result)
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind: %s", refs)
	}
	f.complete(result)
}

func TestIntegrationRestartsWhenTheTargetMovesBeforeThePush(t *testing.T) {
	f, _ := movingFixture(t)
	f.moveOnce("action:pushing", "renew", 1, "second.txt")
	options := f.options("moves-before-push")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	f.assertRestarted(options.OperationID, 0, result)
	f.complete(result)
}

func TestIntegrationRestartsWhenTheLeaseRefusesThePushOfAMovedTarget(t *testing.T) {
	f, _ := movingFixture(t)
	// The renewals after the push intent are: the one before the observation,
	// then the one right before the push itself.
	f.moveOnce("action:pushing", "renew", 2, "second.txt")
	options := f.options("moves-during-push")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	f.assertRestarted(options.OperationID, 1, result)
	f.complete(result)
}

func TestIntegrationRetriesTheConfirmationOfAMovedTarget(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	// A plain fast-forward: the third observation opens the confirmation and
	// the fourth is the one inside its re-observation. The target moved on past
	// the pushed commit between the two target reads and still holds it.
	f.moveOnce("", "observation", 4, "later.txt")
	options := f.options("moves-before-confirm")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	final := f.targetHead()
	if result.ObservedTarget != final || result.Request.Containment != api.IntegrationFastForward || final == f.commit {
		t.Fatalf("receipt: %+v", result)
	}
	if attempts := f.attempts(options.OperationID); len(attempts) != 1 || attempts[0].Retired {
		t.Fatalf("a confirmation on a moved target needs no new attempt: %+v", attempts)
	}
	f.complete(result)
}

func TestIntegrationConfirmationRefusesATargetThatLostTheCommit(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	moved := 0
	f.watch(func(kind string) int {
		if kind == "observation" {
			moved++
			if moved == 4 {
				deliveryGit(t, f.first, "push", "--force", "origin", f.base+":refs/heads/v2")
			}
		}
		return 0
	})
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("rewound-before-confirm")); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("a target that lost the commit must stay unresolved: %v", err)
	}
}

func TestIntegrationPushFailureWithoutAMovedTargetKeepsItsError(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.gitWrapper(`for a do case "$a" in *:refs/heads/v2) exit 1;; esac; done
exec "$KANDER_TEST_REAL_GIT" "$@"
`)
	options := f.options("push-refused")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("a refused push on a still target keeps the unresolved error: %v", err)
	}
	if attempts := f.attempts(options.OperationID); len(attempts) != 1 || attempts[0].Retired {
		t.Fatalf("a push failure without a moved target must not retire the attempt: %+v", attempts)
	}
	if got := f.targetHead(); got != f.base {
		t.Fatal("the refused push moved the target")
	}
}

func TestIntegrationTargetThatKeepsMovingAsksForARetryOfTheSameOperation(t *testing.T) {
	cases := map[string]string{"during preparation": "observation", "at the push intent": "intent"}
	for name, point := range cases {
		t.Run(name, func(t *testing.T) {
			f, _ := movingFixture(t)
			passes, enabled := 0, true
			f.watch(func(kind string) int {
				if !enabled || kind != "observation" {
					return 0
				}
				passes++
				// Every preparation observation is the odd one; the intent observation is the even one.
				if point == "observation" && passes%2 == 1 || point == "intent" && passes%2 == 0 {
					f.foreignPush("move-" + string(rune('a'+passes)) + ".txt")
				}
				return 0
			})
			options := f.options("keeps-moving")
			before := f.targetHead()
			_, err := f.a.Integrate(t.Context(), f.first, options)
			if err == nil || !strings.Contains(err.Error(), "operation ID") {
				t.Fatalf("a target that never settles must ask for a retry: %v", err)
			}
			if f.targetHead() == before && point == "intent" {
				t.Fatal("the foreign pushes did not land")
			}
			for _, attempt := range f.attempts(options.OperationID) {
				if !attempt.Retired || attempt.RetiredFor != "target_moved" {
					t.Fatalf("an attempt survived the moving target: %+v", attempt)
				}
			}
			if refs := f.privateRefs(); refs != "" {
				t.Fatalf("private ref left behind: %s", refs)
			}
			enabled = false
			result, err := f.a.Integrate(t.Context(), f.first, options)
			if err != nil {
				t.Fatalf("the same operation ID must continue with a new attempt: %v", err)
			}
			f.complete(result)
		})
	}
}

func TestIntegrationOtherRetirementsKeepTheUnresolvedError(t *testing.T) {
	f := newIntegrationFixture(t)
	revision := 0
	// Every attempt finds the task changed right after it claims the target.
	f.watch(func(kind string) int {
		if kind != "integration" {
			return 0
		}
		revision++
		current, err := f.a.Project.Show(t.Context(), f.task.TaskID)
		if err != nil {
			t.Error(err)
			return 0
		}
		text := strings.Repeat("changed ", revision) + "while integrating."
		if _, err := f.a.Project.PutDocument(t.Context(), "", f.task.TaskID, api.Document{Path: "report.md", Text: text, ExpectedRevision: current.Revision}, f.credential.Claim()); err != nil {
			t.Error(err)
		}
		return 0
	})
	options := f.options("scope-keeps-changing")
	_, err := f.a.Integrate(t.Context(), f.first, options)
	if err == nil || !strings.Contains(err.Error(), "unresolved") || strings.Contains(err.Error(), "operation ID") {
		t.Fatalf("another retirement reason keeps its error: %v", err)
	}
	for _, attempt := range f.attempts(options.OperationID) {
		if !attempt.Retired || attempt.RetiredFor != "scope_invalid" {
			t.Fatalf("attempt: %+v", attempt)
		}
	}
}
