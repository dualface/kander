package issue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
	rk "github.com/dualface/kander/internal/statemachine/issueresult"
	dk "github.com/dualface/kander/internal/statemachine/issueresultdecision"
)

// Refinement tests of the issue result kernels: for every entrance that runs a
// kernel, the projection of each record after the write equals the kernel step
// of the projection before it, and a refused entrance leaves the store as it
// was. The entrances are driven on a temporary board with a fake provider; the
// store is read back through ReadResultStore, so the validators run on it.

// savedStore reads the stored state file without the writer lock: the entrances
// replace it atomically, so a provider call sees the save that precedes it.
func savedStore(t *testing.T, root string, repo Repository) ResultStore {
	t.Helper()
	key, err := repo.IssueSourceKey(42)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := board.PrivateDataPath(root, "issue-results", "v1", resultHash(key))
	if err != nil {
		t.Fatal(err)
	}
	data, found, err := fs.ReadRegularFileIfExists(root, filepath.Join(dir, "state.json"))
	if err != nil || !found {
		t.Fatalf("stored state: found=%v err=%v", found, err)
	}
	var store ResultStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	return store
}

// observedProvider reports the stored state at the moment a remote write is
// attempted, which is the save that precedes it.
type observedProvider struct {
	*resultFake
	onWrite func()
}

func (p observedProvider) PostResult(ctx context.Context, repo Repository, n int, body string) (ResultComment, error) {
	p.onWrite()
	return p.resultFake.PostResult(ctx, repo, n, body)
}
func (p observedProvider) CloseResult(ctx context.Context, repo Repository, n int) error {
	p.onWrite()
	return p.resultFake.CloseResult(ctx, repo, n)
}

// testWork is the identity of the current work set the kernel projection
// compares records with; readStore refreshes it.
var testWork uint64

func projectOf(store ResultStore, version string) rk.State {
	return projectRecord(store, version, testWork)
}

func readStore(t *testing.T, root string, repo Repository, id string) ResultStore {
	t.Helper()
	store, err := ReadResultStore(context.Background(), root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	work, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	testWork = workID(work)
	return store
}

// The events of an entrance, with the witnesses of the record the entrance wrote.
func intentOf(after ResultStore, version string) rk.Event {
	r := after.Records[version]
	return rk.Intent{Nfull: r.FullyResolved, Nmet: allMet(r.Outcomes), BodyOk: true, Nbod: bodyID(r.Body, r.ActorID),
		Nchk: checksID(r.Checks), Nsound: soundRecord(r, after.TaskID)}
}
func equivalentOf(after ResultStore, version string) rk.Event {
	r := after.Records[version]
	return rk.Equivalent{Nfull: r.FullyResolved, Nmet: allMet(r.Outcomes), Found: true, Nbod: bodyID(r.Body, r.ActorID),
		Ncid: commentID(r.CommentID), Nchk: checksID(r.Checks), Nsound: soundRecord(r, after.TaskID)}
}
func publishOf(after ResultStore, version string) rk.Event {
	return rk.Publish{Ok: true, Ncid: commentID(after.Records[version].CommentID)}
}
func reconcileOf(after ResultStore, version string) rk.Event {
	return rk.Reconcile{Found: true, Ncid: commentID(after.Records[version].CommentID)}
}
func rejectOf(after ResultStore, version string) rk.Event {
	return rk.Reject{Nfh: failuresID(after.Records[version].Failures)}
}
func failOf(after ResultStore, version string) rk.Event {
	return rk.FailClose{Nfh: failuresID(after.Records[version].Failures)}
}

func kernelSteps(t *testing.T, state rk.State, events ...rk.Event) rk.State {
	t.Helper()
	for _, event := range events {
		next, ok := resultStep(state, event)
		if !ok {
			t.Fatalf("kernel refused %#v at %+v", event, state)
		}
		state = next
	}
	return state
}

// othersOf maps the events a write runs on its record to the events the other
// records of the store see: they only see how many records are uncertain.
func othersOf(events []rk.Event) []rk.Event {
	var out []rk.Event
	for _, event := range events {
		switch event.(type) {
		case rk.Intent:
			out = append(out, rk.OtherIntent{})
		case rk.Reject, rk.Publish, rk.Reconcile:
			out = append(out, rk.OtherSettle{})
		}
	}
	return out
}

// assertRefines checks `projection(after) = Step(projection(before), events)` for
// every record of the two stores; `focus` is the record the entrance works on.
func assertRefines(t *testing.T, before, after ResultStore, focus string, events ...rk.Event) {
	t.Helper()
	versions := map[string]bool{focus: true}
	for v := range before.Records {
		versions[v] = true
	}
	for v := range after.Records {
		versions[v] = true
	}
	read := rk.Read{RemoteOk: true, Same: before.IssueID == 42, Nw: testWork}
	for version := range versions {
		want := kernelSteps(t, projectOf(before, version), read)
		if version == focus {
			want = kernelSteps(t, want, events...)
		} else {
			want = kernelSteps(t, want, othersOf(events)...)
		}
		if got := projectOf(after, version); got != want {
			t.Fatalf("record %.8s: projection after the write %+v, kernel step of the projection before %+v", version, got, want)
		}
	}
}

// projectedFacts reads the comparison facts off the kernel projections of a record
// before and after a save, the way the proof of theorem 6 derives them.
func projectedFacts(before, after rk.State) recordFacts {
	return recordFacts{
		identSame:  before.Bod == after.Bod && (before.Cid == 0 || before.Cid == after.Cid),
		prefix:     after.Nf == before.Nf && after.Fh == before.Fh || after.Nf == before.Nf+1 && after.Fp == before.Fh,
		checksSame: before.Chk == after.Chk,
		workSame:   after.Wk == after.Cw,
	}
}

// assertFactsAgree checks that the facts read off the projections are the
// comparisons the shell makes on the records themselves.
func assertFactsAgree(t *testing.T, before, after ResultStore, version string, work ResultWork) {
	t.Helper()
	old, current := before.Records[version], after.Records[version]
	if old == nil || current == nil {
		return
	}
	got := projectedFacts(projectRecord(before, version, workID(work)), projectRecord(after, version, workID(work)))
	want := compareRecords(old, current, work)
	// A new record replaces a rejected one: its text is new, and the update relation skips it.
	if old.Status == "rejected" && current.Status != "rejected" {
		got.identSame, got.checksSame = want.identSame, want.checksSame
	}
	if got != want {
		t.Fatalf("record %.8s: facts of the projections %+v, comparisons of the records %+v", version, got, want)
	}
}

// assertRelations checks that a save satisfies the update and work relations.
func assertRelations(t *testing.T, root string, repo Repository, id string, before, after ResultStore) {
	t.Helper()
	work, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResultStoreUpdate(before, after); err != nil {
		t.Fatalf("update relation: %v", err)
	}
	if err := ValidateResultStoreWork(before, after, work); err != nil {
		t.Fatalf("work relation: %v", err)
	}
	for version := range after.Records {
		assertFactsAgree(t, before, after, version, work)
	}
}

func onlyVersion(t *testing.T, store ResultStore) string {
	t.Helper()
	if len(store.Records) != 1 {
		t.Fatalf("want one record, have %d", len(store.Records))
	}
	for version := range store.Records {
		return version
	}
	return ""
}

// applyObserved runs ApplyResult and returns the store as the intent save left
// it, the one that precedes the remote write.
func applyObserved(t *testing.T, root string, repo Repository, id string, fake *resultFake, proposal ResultProposal) (ResultRecord, ResultStore, error) {
	t.Helper()
	var intent ResultStore
	provider := observedProvider{fake, func() { intent = savedStore(t, root, repo) }}
	record, err := ApplyResult(context.Background(), provider, root, repo, 42, id, proposal)
	return record, intent, err
}

func TestResultPublicationRefinesTheKernels(t *testing.T) {
	ctx := context.Background()
	root, repo, id, fake := resultFixture(t)
	assertSave := func(before, after ResultStore, focus string, events ...rk.Event) {
		t.Helper()
		assertRefines(t, before, after, focus, events...)
		assertRelations(t, root, repo, id, before, after)
	}

	// InspectResult on an empty store: only the issue identity is written (readResult).
	before := readStore(t, root, repo, id)
	i := inspectFixture(t, root, repo, id, fake)
	after := readStore(t, root, repo, id)
	assertSave(before, after, "")
	if after.IssueID != 42 {
		t.Fatalf("issue identity not written: %d", after.IssueID)
	}

	// ApplyResult with a definite provider rejection: an uncertain intent, then rejected.
	fake.postErr = &ResultWriteRejection{Err: errors.New("provider refused")}
	before = after
	if _, intent, err := applyObserved(t, root, repo, id, fake, fixtureProposal(i)); err == nil {
		t.Fatal("rejection not reported")
	} else {
		after = readStore(t, root, repo, id)
		version := onlyVersion(t, after)
		assertSave(before, intent, version, intentOf(intent, version))
		assertSave(intent, after, version, rejectOf(after, version))
		if got := projectOf(after, version); got.St != rk.RstatRejected || got.Nf != 1 || got.Unc != 0 {
			t.Fatalf("rejected record: %+v", got)
		}
	}
	version := onlyVersion(t, after)

	// The rejected record is replaced by a new record of the same version; its failure stays.
	fake.postErr = nil
	before = after
	i = inspectFixture(t, root, repo, id, fake)
	_, intent, err := applyObserved(t, root, repo, id, fake, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertSave(before, intent, version, intentOf(intent, version))
	assertSave(intent, after, version, publishOf(after, version))
	if got := projectOf(after, version); got.St != rk.RstatPublished || got.Nf != 1 || !got.Cpos {
		t.Fatalf("replaced record: %+v", got)
	}

	// A reassessment of a published record writes nothing the kernel tracks.
	before = after
	i = inspectFixture(t, root, repo, id, fake)
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i)); err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertSave(before, after, version, rk.Reassess{})
	if fake.posts != 2 {
		t.Fatalf("a reassessment posted a comment: %d posts", fake.posts)
	}

	// A new delivery is a new version: the first record sees another record become uncertain, then settle.
	path := filepath.Join(root, "done", id, "spec.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), strings.Repeat("a", 40), strings.Repeat("b", 40), 1)), 0600); err != nil {
		t.Fatal(err)
	}
	before = readStore(t, root, repo, id)
	i = inspectFixture(t, root, repo, id, fake)
	_, intent, err = applyObserved(t, root, repo, id, fake, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	var second string
	for v := range after.Records {
		if v != version {
			second = v
		}
	}
	if second == "" {
		t.Fatal("the new delivery did not produce a new version")
	}
	assertSave(before, intent, second, intentOf(intent, second))
	assertSave(intent, after, second, publishOf(after, second))
}

func TestResultUncertainAndEquivalentRefineTheKernels(t *testing.T) {
	ctx := context.Background()

	t.Run("reconcile", func(t *testing.T) {
		root, repo, id, fake := resultFixture(t)
		i := inspectFixture(t, root, repo, id, fake)
		// The provider returns an error that is not a definite rejection and the comment never appears.
		fake.postErr = errors.New("response lost")
		before := readStore(t, root, repo, id)
		_, intent, _ := applyObserved(t, root, repo, id, fake, fixtureProposal(i))
		uncertain := readStore(t, root, repo, id)
		version := onlyVersion(t, uncertain)
		assertRefines(t, before, uncertain, version, intentOf(uncertain, version))
		assertRelations(t, root, repo, id, before, uncertain)
		if projectOf(uncertain, version).St != rk.RstatUncertain || len(intent.Records) != 1 {
			t.Fatal("record not uncertain")
		}
		// While the record is uncertain, nothing is written; an absent comment never clears it.
		i = inspectFixture(t, root, repo, id, fake)
		fake.postErr = nil
		if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i)); err == nil || !strings.Contains(err.Error(), "publication uncertain") {
			t.Fatalf("write while a record is uncertain: %v", err)
		}
		if got := readStore(t, root, repo, id); projectOf(got, version) != projectOf(uncertain, version) {
			t.Fatal("a refused apply changed the record")
		}
		// InspectResult finds the comment in the complete observation.
		fake.remote.Comments = append(fake.remote.Comments, ResultComment{ID: 777, AuthorID: 7, Body: uncertain.Records[version].Body})
		before = readStore(t, root, repo, id)
		inspectFixture(t, root, repo, id, fake)
		after := readStore(t, root, repo, id)
		assertRefines(t, before, after, version, reconcileOf(after, version))
		assertRelations(t, root, repo, id, before, after)
		if after.Records[version].CommentID != 777 {
			t.Fatal("comment ID not recorded")
		}
	})

	t.Run("equivalent", func(t *testing.T) {
		root, repo, id, fake := resultFixture(t)
		fake.remote.Comments = []ResultComment{{ID: 99, AuthorID: 80, Body: "Human verification: fixed, tests pass."}}
		i := inspectFixture(t, root, repo, id, fake)
		before := readStore(t, root, repo, id)
		p := fixtureProposal(i)
		p.EquivalentCommentID, p.Body = 99, ""
		if _, err := ApplyResult(ctx, fake, root, repo, 42, id, p); err != nil {
			t.Fatal(err)
		}
		after := readStore(t, root, repo, id)
		assertRefines(t, before, after, onlyVersion(t, after), equivalentOf(after, onlyVersion(t, after)))
		assertRelations(t, root, repo, id, before, after)
	})
}

// decisionsOf returns the statuses of the decisions of the only record.
func decisionsOf(t *testing.T, root string, repo Repository, id string) []string {
	t.Helper()
	store := readStore(t, root, repo, id)
	var out []string
	for _, d := range store.Records[onlyVersion(t, store)].Decisions {
		out = append(out, d.Status)
	}
	return out
}

// decisionFixture publishes a fully resolved result and returns what a decision needs.
func decisionFixture(t *testing.T) (root string, repo Repository, id string, fake *resultFake, version string, decide func(decision string, reconsider bool) (ResultStore, ResultStore, error)) {
	t.Helper()
	root, repo, id, fake = resultFixture(t)
	i := inspectFixture(t, root, repo, id, fake)
	record, err := ApplyResult(context.Background(), fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	version = record.Version
	// decide runs one DecideResult after a fresh inspection and returns the store
	// before it, the store as the decision save left it (when the provider was
	// asked to close), and the error.
	decide = func(decision string, reconsider bool) (ResultStore, ResultStore, error) {
		i := inspectFixture(t, root, repo, id, fake)
		before := readStore(t, root, repo, id)
		var mid ResultStore
		provider := observedProvider{fake, func() { mid = savedStore(t, root, repo) }}
		_, err := DecideResult(context.Background(), provider, root, repo, 42, id, ResultDecision{Token: i.Token, Version: version,
			Decision: decision, UserReference: "User: decision " + decision, Reconsider: reconsider})
		return before, mid, err
	}
	return
}

func TestResultDecisionsRefineTheKernels(t *testing.T) {
	root, repo, id, fake, version, decide := decisionFixture(t)
	appended := rk.Decide{DigestOk: true, Scan: rk.ScanOpen, FieldsOk: true}
	assertSave := func(before, after ResultStore, events ...rk.Event) {
		t.Helper()
		assertRefines(t, before, after, version, events...)
		assertRelations(t, root, repo, id, before, after)
	}

	// A consent is recorded, and the provider definitely rejects the close request.
	fake.closeErr = &ResultWriteRejection{Err: errors.New("close refused")}
	before, mid, err := decide("yes", false)
	if err == nil {
		t.Fatal("close rejection not reported")
	}
	after := readStore(t, root, repo, id)
	assertSave(before, mid, appended)
	assertSave(mid, after, failOf(after, version))
	if got := decisionsOf(t, root, repo, id); len(got) != 1 || got[0] != "rejected" {
		t.Fatalf("decisions: %v", got)
	}

	// A rejected consent is the first decision of its state version: it blocks a refusal that
	// does not reconsider, and the first blocker decides, so it blocks the same refusal again below.
	before = after
	if _, _, err := decide("no", false); err == nil || !strings.Contains(err.Error(), "old close consent cannot be reused") {
		t.Fatalf("refusal after a rejected consent: %v", err)
	}
	if got := readStore(t, root, repo, id); projectOf(got, version) != projectOf(before, version) {
		t.Fatal("a refused decision changed the record")
	}
	before, _, err = decide("no", true)
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertSave(before, after, appended)
	if got := decisionsOf(t, root, repo, id); len(got) != 2 || got[1] != "declined" {
		t.Fatalf("decisions: %v", got)
	}
	if _, _, err := decide("no", false); err == nil || !strings.Contains(err.Error(), "old close consent cannot be reused") {
		t.Fatalf("the first blocker must decide: %v", err)
	}

	// The consent closes the issue.
	fake.closeErr = nil
	before, mid, err = decide("yes", true)
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertSave(before, mid, appended)
	assertSave(mid, after)
	if got := decisionsOf(t, root, repo, id); len(got) != 3 || got[2] != "closed" {
		t.Fatalf("decisions: %v", got)
	}

	// A closed remote returns the record and writes nothing.
	before, _, err = decide("no", false)
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertRefines(t, before, after, version, rk.Keep{DigestOk: true, Closed: true})
}

func TestResultRefusalFirstThenConsent(t *testing.T) {
	root, repo, id, fake, version, decide := decisionFixture(t)
	before, _, err := decide("no", false)
	if err != nil {
		t.Fatal(err)
	}
	after := readStore(t, root, repo, id)
	assertRefines(t, before, after, version, rk.Decide{DigestOk: true, Scan: rk.ScanOpen, FieldsOk: true})
	assertRelations(t, root, repo, id, before, after)

	// The same refusal returns the record and writes nothing (the scan returns).
	before, _, err = decide("no", false)
	if err != nil {
		t.Fatal(err)
	}
	after = readStore(t, root, repo, id)
	assertRefines(t, before, after, version, rk.Keep{DigestOk: true, Scan: rk.ScanReturned})
	if len(decisionsOf(t, root, repo, id)) != 1 {
		t.Fatal("a repeated refusal wrote a decision")
	}

	// A consent after a refusal needs reconsideration; without it the scan refuses and nothing changes.
	before, _, err = decide("yes", false)
	if err == nil || !strings.Contains(err.Error(), "refusal recorded") || fake.closes != 0 {
		t.Fatalf("consent without reconsideration: %v", err)
	}
	if got := readStore(t, root, repo, id); projectOf(got, version) != projectOf(before, version) || len(decisionsOf(t, root, repo, id)) != 1 {
		t.Fatal("a refused decision changed the record")
	}
}

func TestResultInspectReconcilesUncertainDecisions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*resultFake)
		want   string
	}{
		{"remote closed", func(f *resultFake) { f.remote.State, f.remote.StateVersion = "closed", "42:closed" }, "observed-closed"},
		{"state version changed", func(f *resultFake) { f.remote.StateVersion = "42:reopened" }, "superseded"},
		{"nothing changed", func(f *resultFake) {}, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repo, id, fake, _, decide := decisionFixture(t)
			// The close request is lost without an effect: the consent stays uncertain.
			fake.closeErr = errors.New("timeout")
			if _, _, err := decide("yes", false); err == nil {
				t.Fatal("lost close reported success")
			}
			tc.mutate(fake)
			inspectFixture(t, root, repo, id, fake)
			got := decisionsOf(t, root, repo, id)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("decisions: %v, want %s", got, tc.want)
			}
			// The decision kernel agrees: the same event on an uncertain consent.
			next, ok := decisionStep(dk.State{Ds: dk.DstatUncertain}, dk.Reconcile{Closed: fake.remote.State == "closed", Same: fake.remote.StateVersion == "42:initial"})
			if tc.want == "uncertain" {
				if ok {
					t.Fatal("kernel reconciled an open remote with the same state version")
				}
			} else if !ok || decisionStatusOf(next.Ds) != tc.want {
				t.Fatalf("kernel: %+v %v", next, ok)
			}
		})
	}
}

// TestResultEntrancesRefuseThroughTheKernels drives the refusals of the three
// entrances. Each one returns the error it always returned, leaves the store
// as it was, and the kernel refuses the event with the facts of the entrance.
func TestResultEntrancesRefuseThroughTheKernels(t *testing.T) {
	ctx := context.Background()
	root, repo, id, fake := resultFixture(t)
	i := inspectFixture(t, root, repo, id, fake)
	published, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	before := readStore(t, root, repo, id)
	state := projectOf(before, published.Version)
	refused := func(name string, err error, want string, event rk.Event, on rk.State) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %v, want %q", name, err, want)
		}
		if got := readStore(t, root, repo, id); !reflect.DeepEqual(got, before) {
			t.Fatalf("%s: the store changed", name)
		}
		if next, ok := resultStep(on, event); ok || next != on {
			t.Fatalf("%s: the kernel accepted %#v", name, event)
		}
	}
	apply := func(mutate func(*ResultProposal)) error {
		i := inspectFixture(t, root, repo, id, fake)
		p := fixtureProposal(i)
		mutate(&p)
		_, err := ApplyResult(ctx, fake, root, repo, 42, id, p)
		return err
	}
	decide := func(token string, d ResultDecision) error {
		d.Token = token
		_, err := DecideResult(ctx, fake, root, repo, 42, id, d)
		return err
	}
	fresh := rk.State{Iid: true}

	// The completion evidence is already published: another version of it is not posted again.
	err = apply(func(p *ResultProposal) { p.Outcomes[3], p.FullyResolved, p.ResolutionEvidence = "unmet", false, "" })
	refused("duplicate evidence", err, "completion evidence unchanged", rk.Intent{Dup: true, BodyOk: true, Nbod: 1, Nchk: 1, Nsound: true}, fresh)
	// A body with private data is not posted.
	fake2root, fake2repo, fake2id, fake2 := resultFixture(t)
	i2 := inspectFixture(t, fake2root, fake2repo, fake2id, fake2)
	bad := fixtureProposal(i2)
	bad.Body = "see /home/user/secret"
	_, err = ApplyResult(ctx, fake2, fake2root, fake2repo, 42, fake2id, bad)
	if err == nil || fake2.posts != 0 || len(readStore(t, fake2root, fake2repo, fake2id).Records) != 0 {
		t.Fatalf("private text: %v", err)
	}
	if _, ok := resultStep(rk.State{Iid: true}, rk.Intent{BodyOk: false, Nbod: 1, Nchk: 1, Nsound: true}); ok {
		t.Fatal("the kernel accepted an invalid body")
	}
	// A comment that is not in the observation is not an equivalent result.
	err = apply(func(p *ResultProposal) {
		p.EquivalentCommentID, p.Outcomes[3], p.FullyResolved, p.ResolutionEvidence = 12345, "unmet", false, ""
	})
	refused("absent equivalent comment", err, "equivalent comment absent", rk.Equivalent{Found: false, Nbod: 1, Ncid: 1, Nchk: 1, Nsound: true}, fresh)

	// A decision needs a record of the current work that is published or equivalent and fully resolved.
	i = inspectFixture(t, root, repo, id, fake)
	err = decide(i.Token, ResultDecision{Version: "unknown", Decision: "no", UserReference: "User: no."})
	refused("unknown record", err, "no current fully resolved assessment", rk.Keep{DigestOk: false, Closed: true}, rk.State{Iid: true})
	// The decision fields are valid.
	for _, d := range []ResultDecision{{Decision: "maybe", UserReference: "User: maybe."}, {Decision: "yes", UserReference: "  "}} {
		d.Version = published.Version
		err = decide(i.Token, d)
		refused("decision fields", err, "explicit user decision and reference required", rk.Decide{DigestOk: true, FieldsOk: false}, state)
	}
	// The token binds the inspection.
	if err := decide("stale", ResultDecision{Version: published.Version, Decision: "no", UserReference: "User: no."}); err == nil {
		t.Fatal("stale token accepted")
	}
	// A consent whose close outcome is uncertain blocks every later decision of its state version.
	fake.closeErr = errors.New("timeout")
	if err := decide(i.Token, ResultDecision{Version: published.Version, Decision: "yes", UserReference: "User: close."}); err == nil {
		t.Fatal("lost close reported success")
	}
	fake.closeErr = nil
	before = readStore(t, root, repo, id)
	state = projectOf(before, published.Version)
	err = decide(i.Token, ResultDecision{Version: published.Version, Decision: "no", UserReference: "User: no.", Reconsider: true})
	refused("uncertain consent", err, "close outcome uncertain", rk.Decide{DigestOk: true, Scan: rk.ScanUncertain, FieldsOk: true}, state)
}
