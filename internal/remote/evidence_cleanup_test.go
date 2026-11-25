package remote_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

// evidenceRefs lists the refs of the common remote below refs/heads/.
func (f *integrationFixture) evidenceRefs() []string {
	f.t.Helper()
	return strings.Fields(f.remoteRefs("refs/heads/"))
}

// pushRef writes commit to ref on the common remote from the first clone.
func (f *integrationFixture) pushRef(commit, ref string) {
	f.t.Helper()
	deliveryGit(f.t, f.first, "push", "-q", "origin", commit+":"+ref)
}

// completeCleaning finishes the fixture card the way kander move done does and
// returns the cleanup report.
func (f *integrationFixture) completeCleaning() remote.EvidenceCleanupReport {
	f.t.Helper()
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		f.t.Fatal(err)
	}
	current, err := f.a.Project.Show(f.t.Context(), f.task.TaskID)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.a.Project.PutDocument(f.t.Context(), "", f.task.TaskID, api.Document{Path: "report.md", Text: "Completed and cleaned.", ExpectedRevision: current.Revision}, f.credential.Claim()); err != nil {
		f.t.Fatal(err)
	}
	done, report, err := f.a.MoveTaskCleaningEvidence(f.t.Context(), f.task.TaskID, api.Move{State: "done", Result: "completed"})
	if err != nil || done.State != "done" {
		f.t.Fatalf("completion: %+v %v", done, err)
	}
	return report
}

// unrelatedRefs publishes refs a cleanup of the fixture card must keep: the
// evidence of another card and of another project, a ref below the card's
// prefix that no DeliveryRef names, the source ref of an execution of another
// card, a task branch, a group branch and the mainline branch.
func (f *integrationFixture) unrelatedRefs() []string {
	f.t.Helper()
	p, task := f.a.Info.ID, f.task.TaskID
	refs := []string{
		api.DeliveryRef(p, "20991231-other-card-task", 1, f.base),
		api.DeliveryRef("other-project", task, 1, f.base),
		"refs/heads/kander/" + p + "/" + task + "/notes",
		api.IntegrationSourceRef(p, "foreign-execution", f.base),
		"refs/heads/kander-task/" + task,
		"refs/heads/group/20991231-foreign-group",
	}
	for _, ref := range refs {
		f.pushRef(f.base, ref)
	}
	return append(refs, "refs/heads/v2")
}

// A done standalone card loses the task evidence of every generation and the
// source ref of every integration execution, including an attempt the moved
// target retired. Everything else on the remote stays.
func TestCompletionCleanupDeletesTheEvidenceOfAStandaloneCard(t *testing.T) {
	f, _ := movingFixture(t)
	ctx, p, task := t.Context(), f.a.Info.ID, f.task.TaskID
	// The baseline of the current generation, written the way PublishBaseline
	// writes it, and an earlier generation that left its own evidence.
	baseline, earlier := api.DeliveryRef(p, task, f.credential.Generation, f.base), api.DeliveryRef(p, task, 7, f.base)
	f.pushRef(f.base, baseline)
	f.pushRef(f.base, earlier)
	kept := f.unrelatedRefs()
	f.moveOnce("", "observation", 2, "second.txt")
	options := f.options("cleanup-after-rotation")
	result, err := f.a.Integrate(ctx, f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	attempts := f.attempts(options.OperationID)
	if len(attempts) != 2 || !attempts[0].Retired {
		t.Fatalf("attempts: %+v", attempts)
	}
	owned := []string{earlier, baseline, api.DeliveryRef(p, task, f.credential.Generation, f.commit)}
	for _, attempt := range attempts {
		owned = append(owned, api.IntegrationSourceRef(p, attempt.Request.ExecutionID, attempt.Request.Commit))
	}
	before := f.evidenceRefs()
	for _, ref := range owned {
		if !slices.Contains(before, ref) {
			t.Fatalf("fixture lacks %s: %v", ref, before)
		}
	}
	if result.SourceRef != owned[len(owned)-1] {
		t.Fatalf("source ref of the final attempt: %s", result.SourceRef)
	}
	report := f.completeCleaning()
	if report.Err != nil || len(report.Kept) != 0 || report.Warning() != "" {
		t.Fatalf("cleanup: %+v", report)
	}
	slices.Sort(owned)
	if !slices.Equal(report.Deleted, owned) {
		t.Fatalf("deleted %v, want %v", report.Deleted, owned)
	}
	after := f.evidenceRefs()
	for _, ref := range owned {
		if slices.Contains(after, ref) {
			t.Fatalf("%s survived the cleanup", ref)
		}
	}
	for _, ref := range kept {
		if !slices.Contains(after, ref) {
			t.Fatalf("the cleanup deleted %s", ref)
		}
	}
	// Deleting again finds nothing and reports nothing.
	if again := f.a.CleanTaskEvidence(ctx, task, f.first); again.Err != nil || len(again.Deleted) != 0 || again.Warning() != "" {
		t.Fatalf("repeated cleanup: %+v", again)
	}
}

// A refused deletion keeps the card done and names every ref that stayed; a
// later run of the same cleanup, as a replayed bound move runs it, finishes it.
func TestCompletionCleanupRefusedByTheRemoteOnlyWarns(t *testing.T) {
	f := newIntegrationFixture(t)
	ctx, p, task := t.Context(), f.a.Info.ID, f.task.TaskID
	if _, err := f.a.Integrate(ctx, f.first, f.options("refused-cleanup")); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(f.bare, "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do\n  case \"$new\" in 0000000000000000000000000000000000000000) echo \"deletion refused\" >&2; exit 1;; esac\ndone\nexit 0\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	report := f.completeCleaning()
	delivery := api.DeliveryRef(p, task, f.credential.Generation, f.commit)
	if report.Err != nil || len(report.Deleted) != 0 || len(report.Kept) != 2 {
		t.Fatalf("refused cleanup: %+v", report)
	}
	warning := report.Warning()
	if !strings.Contains(warning, delivery) || !strings.Contains(warning, "deletion refused") {
		t.Fatalf("warning %q", warning)
	}
	if card, err := f.a.Project.Show(ctx, task); err != nil || card.State != "done" {
		t.Fatalf("card after a refused cleanup: %+v %v", card.State, err)
	}
	if !slices.Contains(f.evidenceRefs(), delivery) {
		t.Fatal("a refused deletion removed the delivery ref")
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if again := f.a.CleanTaskEvidence(ctx, task, f.first); again.Err != nil || len(again.Kept) != 0 || len(again.Deleted) != 2 {
		t.Fatalf("repeated cleanup: %+v", again)
	}
	if slices.Contains(f.evidenceRefs(), delivery) {
		t.Fatal("the replayed cleanup kept the delivery ref")
	}
}

// A checkout whose origin is not the registered remote deletes nothing.
func TestCompletionCleanupNeedsTheRegisteredRemote(t *testing.T) {
	f := newIntegrationFixture(t)
	ctx, p, task := t.Context(), f.a.Info.ID, f.task.TaskID
	if _, err := f.a.Integrate(ctx, f.first, f.options("foreign-push-url")); err != nil {
		t.Fatal(err)
	}
	report := f.completeCleaningWith(func() {
		other := filepath.Join(f.root, "other.git")
		deliveryGit(t, f.root, "init", "-q", "--bare", other)
		deliveryGit(t, f.first, "remote", "set-url", "--push", "origin", other)
		t.Chdir(f.root)
	})
	if report.Err == nil || len(report.Deleted) != 0 || report.Warning() == "" {
		t.Fatalf("cleanup through a foreign push URL: %+v", report)
	}
	if !slices.Contains(f.evidenceRefs(), api.DeliveryRef(p, task, f.credential.Generation, f.commit)) {
		t.Fatal("the delivery ref was deleted")
	}
}

// completeCleaningWith runs before after the completion gate verified the
// integration, then the cleanup.
func (f *integrationFixture) completeCleaningWith(before func()) remote.EvidenceCleanupReport {
	f.t.Helper()
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		f.t.Fatal(err)
	}
	current, err := f.a.Project.Show(f.t.Context(), f.task.TaskID)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.a.Project.PutDocument(f.t.Context(), "", f.task.TaskID, api.Document{Path: "report.md", Text: "Completed.", ExpectedRevision: current.Revision}, f.credential.Claim()); err != nil {
		f.t.Fatal(err)
	}
	if done, err := f.a.MoveTask(f.t.Context(), f.task.TaskID, api.Move{State: "done", Result: "completed"}); err != nil || done.State != "done" {
		f.t.Fatalf("completion: %+v %v", done, err)
	}
	before()
	return f.a.CleanTaskEvidence(f.t.Context(), f.task.TaskID, f.first)
}

// completeMemberCleaning moves member i to done on its current receipt the way
// kander move done does and returns the cleanup report.
func (g *redeliverGroup) completeMemberCleaning(t *testing.T, i int) remote.EvidenceCleanupReport {
	t.Helper()
	session, id := g.session(i), g.ids[i]
	g.owners[i].Worktree = g.dir(i)
	if err := session.SaveCredential(g.owners[i]); err != nil {
		t.Fatal(err)
	}
	current, err := session.Project.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.PutDocument(t.Context(), "", id, api.Document{Path: "report.md", Text: "Completed through a group integration.", ExpectedRevision: current.Revision}, g.owners[i].Claim()); err != nil {
		t.Fatal(err)
	}
	done, report, err := session.MoveTaskCleaningEvidence(t.Context(), id, api.Move{State: "done", Result: "completed"})
	if err != nil || done.State != "done" {
		t.Fatalf("completion of %s: %+v %v", id, done, err)
	}
	return report
}

// reconcileGroup reconciles the group through the service. Each wrap-up fact
// is verified in Git from the second clone, as the coordinator's client does;
// it returns the number of facts that needed Git.
func (g *redeliverGroup) reconcileGroup(t *testing.T, bound map[string]board.CoordinatorObservation) (board.CoordinatorCheckpoint, int) {
	t.Helper()
	ctx, s := t.Context(), g.f.a
	view, err := s.Project.Group(ctx, g.group)
	if err != nil || view.Checkpoint == nil {
		t.Fatalf("group: %+v %v", view, err)
	}
	request := board.CoordinatorReconcile{GroupID: g.group, ExpectedRevision: view.Checkpoint.Revision, Authority: view.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for id := range view.Checkpoint.Members {
		card, err := s.Project.Show(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		observation := bound[id]
		observation.Revision = card.Revision
		request.Members[id] = observation
	}
	facts, err := s.Project.CoordinatorFacts(ctx, g.group, request)
	if err != nil {
		t.Fatal(err)
	}
	checks := []api.CoordinatorGitCheck{}
	for _, fact := range facts {
		check := api.CoordinatorGitCheck{Facts: fact}
		if fact.Delivery != nil {
			t.Fatalf("unexpected delivery fact: %+v", fact.Delivery)
		}
		if _, err := g.f.b.VerifyWrapUpIntegrationWitness(ctx, g.f.second, fact.Integration); err != nil {
			check.Error = remote.ServerText(err)
		}
		checks = append(checks, check)
	}
	proof, err := s.CoordinatorAuthority(g.group, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.Project.CoordinatorAction(ctx, "", g.group, "reconcile", api.GroupReconcile{Request: request, Checks: checks}, proof)
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint, len(facts)
}

// The evidence of a task group stays until the group is settled: through the
// first done member, an archived member, and the wrap-up of the last member,
// whose first Git verification by reconcile still reads the deliveries of all
// members and both integration sources. The reconcile that records that
// verification settles the group and its cleanup deletes the evidence of the
// done members; a later reconcile needs no Git and blocks no member.
func TestCompletionCleanupWaitsForTheSettledGroup(t *testing.T) {
	g := newRedeliverGroup(t, "20261125-cleanup-group")
	f, ids, ctx, p := g.f, g.ids, t.Context(), g.f.a.Info.ID
	foreign := api.IntegrationSourceRef(p, "foreign-group-execution", f.base)
	f.pushRef(f.base, foreign)
	first, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil || first.Phase != "integrated" {
		t.Fatalf("group integration: %+v %v", first, err)
	}
	group := []string{api.IntegrationSourceRef(p, first.Request.ExecutionID, first.Request.Commit)}
	for i, id := range ids {
		group = append(group, api.DeliveryRef(p, id, g.owners[i].Generation, f.commit))
	}
	present := func(refs ...string) {
		t.Helper()
		listed := f.evidenceRefs()
		for _, ref := range refs {
			if !slices.Contains(listed, ref) {
				t.Fatalf("%s is gone: %v", ref, listed)
			}
		}
	}
	quiet := func(report remote.EvidenceCleanupReport) {
		t.Helper()
		if report.Err != nil || len(report.Deleted) != 0 || len(report.Kept) != 0 {
			t.Fatalf("cleanup of an unsettled group: %+v", report)
		}
	}
	present(group...)

	quiet(g.completeMemberCleaning(t, 0))
	present(group...)
	if _, err := f.b.Project.Move(ctx, "", ids[1], api.Move{State: "archived", Result: "cancelled", Reason: "the user dropped this member", Decision: "explicit fixture decision"}, g.owners[1].Claim()); err != nil {
		t.Fatal(err)
	}
	quiet(f.a.CleanTaskEvidence(ctx, ids[1], f.first))
	present(group...)

	next := g.takeOver(t, 2)
	g.redeliver(t, ids[2], next)
	g.options.OperationID = "cleanup-reintegration"
	second, err := f.a.Integrate(ctx, f.first, g.options)
	if err != nil || second.Phase != "integrated" {
		t.Fatalf("re-integration: %+v %v", second, err)
	}
	group = append(group, api.IntegrationSourceRef(p, second.Request.ExecutionID, second.Request.Commit), api.DeliveryRef(p, ids[2], next.Generation, f.commit))
	present(group...)

	proof := *next.Claim()
	card, err := f.b.Project.Show(ctx, ids[2])
	if err != nil {
		t.Fatal(err)
	}
	wrap := board.DispatchIntegration{IntegrationID: second.ID, IntegrationExecution: second.Request.ExecutionID, IntegrationGeneration: second.Generation, DispatchID: "cleanup-wrap-up", TaskID: ids[2], CWD: "project:" + p, SourceCommit: f.commit, ReviewTarget: f.commit, ReviewBase: f.base, TargetCommit: second.ObservedTarget, TargetRef: second.Request.TargetRef, Author: "coordinator", Basis: "receipt of the new generation", VerifiedAt: time.Now().UTC()}
	input := board.DispatchInput{ID: wrap.DispatchID, TaskID: ids[2], Kind: "wrap-up", Message: "Record completion and clean the execution workspace", Base: f.commit, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: ids[2], Path: "dispatches/" + wrap.DispatchID + "/integration.json"}, Git: wrap}}}
	prepared, err := f.b.Project.DispatchAction(ctx, "", ids[2], "prepare", api.DispatchCommand{Input: &input, CardRevision: card.Revision}, proof)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := f.b.Project.Move(ctx, "", ids[2], api.Move{State: "working", Authorization: prepared.Authorization}, &proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Project.PutDocument(ctx, "", ids[2], api.Document{Path: "report.md", Text: "Completed by the new generation.", ExpectedRevision: accepted.Revision, Authorization: prepared.Authorization}, &proof); err != nil {
		t.Fatal(err)
	}
	completion := api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: second.Request.Commit}
	done, report, err := f.b.MoveTaskCleaningEvidence(ctx, ids[2], completion)
	if err != nil || done.State != "done" {
		t.Fatalf("wrap-up of the last member: %+v %v", done, err)
	}
	// Every member is finished, but the completed wrap-up has no record yet.
	quiet(report)
	present(group...)

	bound := map[string]board.CoordinatorObservation{ids[2]: {DispatchID: prepared.Input.ID, Epoch: prepared.Authorization.Epoch, Base: prepared.Input.Base, DeliveryCommit: second.Request.Commit}}
	checkpoint, facts := g.reconcileGroup(t, bound)
	if facts == 0 || len(checkpoint.BlockedMembers()) != 0 {
		t.Fatalf("first reconcile: %d facts, blocked %v", facts, checkpoint.BlockedMembers())
	}
	cleaned := f.a.CleanGroupEvidence(ctx, g.group, f.first)
	archived := api.DeliveryRef(p, ids[1], g.owners[1].Generation, f.commit)
	want := slices.DeleteFunc(slices.Clone(group), func(ref string) bool { return ref == archived })
	slices.Sort(want)
	if cleaned.Err != nil || len(cleaned.Kept) != 0 || !slices.Equal(cleaned.Deleted, want) {
		t.Fatalf("group cleanup: %+v, want %v", cleaned, want)
	}
	listed := f.evidenceRefs()
	for _, ref := range want {
		if slices.Contains(listed, ref) {
			t.Fatalf("%s survived the group cleanup", ref)
		}
	}
	present(archived, foreign, "refs/heads/v2")

	checkpoint, facts = g.reconcileGroup(t, bound)
	if facts != 0 || len(checkpoint.BlockedMembers()) != 0 {
		t.Fatalf("reconcile after the cleanup: %d facts, blocked %v", facts, checkpoint.BlockedMembers())
	}
	// A replayed completion runs the cleanup again and finds nothing left.
	replay, report, err := f.b.MoveTaskCleaningEvidence(ctx, ids[2], completion)
	if err != nil || replay.MoveReceipt == nil || !replay.MoveReceipt.Replayed {
		t.Fatalf("replayed completion: %+v %v", replay.MoveReceipt, err)
	}
	if report.Err != nil || len(report.Deleted) != 0 || len(report.Kept) != 0 {
		t.Fatalf("cleanup of the replay: %+v", report)
	}
}

// A push to origin reaches every push URL it has. A checkout whose origin also
// pushes to a mirror deletes nothing, in neither repository, and the warning
// names the refs to check.
func TestCompletionCleanupRefusesAnExtraPushURL(t *testing.T) {
	f := newIntegrationFixture(t)
	ctx, p, task := t.Context(), f.a.Info.ID, f.task.TaskID
	if _, err := f.a.Integrate(ctx, f.first, f.options("extra-push-url")); err != nil {
		t.Fatal(err)
	}
	delivery := api.DeliveryRef(p, task, f.credential.Generation, f.commit)
	mirror := filepath.Join(f.root, "mirror.git")
	deliveryGit(t, f.root, "init", "-q", "--bare", mirror)
	deliveryGit(t, f.first, "push", "-q", mirror, f.commit+":"+delivery)
	report := f.completeCleaningWith(func() {
		deliveryGit(t, f.first, "remote", "set-url", "--add", "--push", "origin", f.bare)
		deliveryGit(t, f.first, "remote", "set-url", "--add", "--push", "origin", mirror)
		t.Chdir(f.root)
	})
	if report.Err == nil || len(report.Deleted) != 0 || len(report.Kept) != 0 {
		t.Fatalf("cleanup through two push URLs: %+v", report)
	}
	if warning := report.Warning(); !strings.Contains(warning, "refs/heads/kander/"+p+"/"+task+"/*") {
		t.Fatalf("warning %q does not name the scope", warning)
	}
	if !slices.Contains(f.evidenceRefs(), delivery) {
		t.Fatal("the registered remote lost the delivery ref")
	}
	if got := deliveryGit(t, mirror, "for-each-ref", "--format=%(refname)", delivery); got != delivery {
		t.Fatalf("the mirror lost the delivery ref: %q", got)
	}
}

// A deletion whose push fails and whose result cannot be listed again keeps
// every listed ref in the warning as unconfirmed, with both errors, and the
// card stays done.
func TestCompletionCleanupKeepsUnconfirmedRefsWhenTheRemoteGoesAway(t *testing.T) {
	f := newIntegrationFixture(t)
	ctx, p, task := t.Context(), f.a.Info.ID, f.task.TaskID
	if _, err := f.a.Integrate(ctx, f.first, f.options("unconfirmed-cleanup")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.root, "deletion-attempted")
	t.Setenv("KANDER_TEST_DELETION_MARKER", marker)
	f.gitWrapper(`for a do
 case "$a" in
  --delete) : > "$KANDER_TEST_DELETION_MARKER"; echo "fixture push failure" >&2; exit 1;;
  ls-remote) if [ -e "$KANDER_TEST_DELETION_MARKER" ]; then echo "fixture listing failure" >&2; exit 128; fi;;
 esac
done
exec "$KANDER_TEST_REAL_GIT" "$@"
`)
	report := f.completeCleaning()
	delivery := api.DeliveryRef(p, task, f.credential.Generation, f.commit)
	if report.Err != nil || len(report.Deleted) != 0 || len(report.Kept) != 2 {
		t.Fatalf("unconfirmed cleanup: %+v", report)
	}
	for _, kept := range report.Kept {
		if !kept.Unconfirmed || !strings.Contains(kept.Reason, "fixture push failure") || !strings.Contains(kept.Reason, "fixture listing failure") {
			t.Fatalf("kept ref %+v", kept)
		}
	}
	warning := report.Warning()
	if !strings.Contains(warning, delivery) || !strings.Contains(warning, "could not confirm") || !strings.Contains(warning, "deletion unconfirmed") {
		t.Fatalf("warning %q", warning)
	}
	if card, err := f.a.Project.Show(ctx, task); err != nil || card.State != "done" {
		t.Fatalf("card after an unconfirmed cleanup: %+v %v", card.State, err)
	}
}
