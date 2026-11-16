package issue

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// The validators of a result store were hand-written functions before the issue
// result kernels decided them. The functions below are those originals, kept
// as the oracle of the differential tests: for single violations the kernels
// return exactly their errors, and for several the same verdict.

func legacyValidateResultStore(store ResultStore, sourceKey, cardID string) error {
	if store.Schema != 1 || store.SourceKey != sourceKey || store.TaskID != cardID || store.Records == nil || store.IssueID < 0 {
		return resultError("inconsistent result recovery record")
	}
	if len(store.Records) > 0 && store.IssueID <= 0 {
		return resultError("result records need the confirmed numeric issue identity")
	}
	for version, record := range store.Records {
		if record == nil || record.Version != version || !recordTaskKeyValid(record, cardID) || resultVersion(record.taskKey(cardID), record.Commits, record.Outcomes, record.FullyResolved) != version {
			return resultError("invalid result record")
		}
		if record.ActorID <= 0 || (record.Status == "published" || record.Status == "equivalent") && record.CommentID <= 0 {
			return resultError("result records need confirmed numeric remote identities")
		}
		for _, outcome := range record.Outcomes {
			if outcome != "met" && outcome != "unmet" && outcome != "unknown" || record.FullyResolved && outcome != "met" {
				return resultError("invalid stored criterion outcome")
			}
		}
		switch record.Status {
		case "uncertain", "published", "equivalent", "rejected":
		default:
			return resultError("invalid result status")
		}
		for _, decision := range record.Decisions {
			if decision.StateVersion == "" || decision.UserReference == "" || decision.Decision != "yes" && decision.Decision != "no" {
				return resultError("invalid close decision")
			}
			switch decision.Status {
			case "declined", "uncertain", "closed", "observed-closed", "superseded", "rejected":
			default:
				return resultError("invalid close decision status")
			}
			if decision.Decision == "yes" && decision.Status == "declined" {
				return resultError("a close consent cannot become a refusal")
			}
			if decision.Decision == "no" && decision.Status != "declined" {
				return resultError("close refusal cannot authorize a mutation")
			}
		}
	}
	return nil
}

func legacyValidateResultStoreUpdate(previous, next ResultStore) error {
	if err := legacyValidateResultStore(next, previous.SourceKey, previous.TaskID); err != nil {
		return err
	}
	if previous.IssueID > 0 && next.IssueID != previous.IssueID {
		return resultError("result issue identity changed")
	}
	for version, old := range previous.Records {
		current := next.Records[version]
		if current == nil {
			return resultError("result history cannot be removed")
		}
		if old.Status != "rejected" {
			if old.Status != "uncertain" && current.Status != old.Status {
				return resultError("published result status cannot regress")
			}
			if current.Body != old.Body || current.ActorID != old.ActorID || old.CommentID > 0 && current.CommentID != old.CommentID {
				return resultError("published result identity changed")
			}
		}
		if len(current.Failures) < len(old.Failures) || !slices.Equal(current.Failures[:len(old.Failures)], old.Failures) {
			return resultError("result failures cannot be removed")
		}
		if len(current.Decisions) < len(old.Decisions) {
			return resultError("close history cannot be removed")
		}
		for i, decision := range old.Decisions {
			updated := current.Decisions[i]
			if updated.StateVersion != decision.StateVersion || updated.Decision != decision.Decision || updated.UserReference != decision.UserReference {
				return resultError("close decision identity changed")
			}
			if decision.Status != "uncertain" && updated.Status != decision.Status {
				return resultError("settled close decision cannot regress")
			}
		}
	}
	return nil
}

func legacyValidateResultStoreWork(previous, next ResultStore, work ResultWork) error {
	for version, current := range next.Records {
		old := previous.Records[version]
		if old != nil && old.Status == "rejected" && reflect.DeepEqual(old, current) {
			continue
		}
		fresh := old == nil || old.Status == "rejected"
		if !fresh {
			if !reflect.DeepEqual(current.Checks, old.Checks) {
				return resultError("published verification evidence changed")
			}
			continue
		}
		if current.Status != "uncertain" && current.Status != "equivalent" {
			return resultError("publication needs its preceding uncertain intent")
		}
		for oldVersion, record := range previous.Records {
			if oldVersion != version && record.Status == "uncertain" {
				return resultError("another publication outcome is still uncertain")
			}
		}
		if current.CardDigest != work.Digest || current.EvidenceVersion != work.EvidenceVersion || !slices.Equal(current.TaskIDs, work.taskIDs()) || !slices.Equal(current.Commits, work.Commits) {
			return resultError("result assessment differs from the current work set")
		}
		calculated, err := proposalVersion(work, ResultProposal{Outcomes: current.Outcomes, Checks: current.Checks, FullyResolved: current.FullyResolved, ResolutionEvidence: current.ResolutionEvidence})
		if err != nil {
			return err
		}
		if calculated != version {
			return resultError("result version differs from the current assessment")
		}
	}
	return nil
}

func cloneStore(t *testing.T, store ResultStore) ResultStore {
	t.Helper()
	raw, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	var out ResultStore
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// oracleStore is a published record with a refusal, a closed consent, and a
// failure: every field the validators compare exists.
func oracleStore(t *testing.T) (root string, repo Repository, id string, previous ResultStore, version string, work ResultWork) {
	t.Helper()
	root, repo, id, fake, version, decide := decisionFixture(t)
	fake.closeErr = &ResultWriteRejection{Err: context.Canceled}
	_, _, _ = decide("yes", false)
	fake.closeErr = nil
	if _, _, err := decide("no", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decide("yes", true); err != nil {
		t.Fatal(err)
	}
	previous = readStore(t, root, repo, id)
	work, err := ReadResultWork(root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	return
}

type storeMutation struct {
	name   string
	mutate func(*ResultStore, *ResultRecord)
}

// onDecision mutates the decision at an index when the record still has it.
func onDecision(i int, f func(*ResultCloseRecord)) func(*ResultStore, *ResultRecord) {
	return func(_ *ResultStore, r *ResultRecord) {
		if i < len(r.Decisions) {
			f(&r.Decisions[i])
		}
	}
}

var storeMutations = []storeMutation{
	{"none", func(*ResultStore, *ResultRecord) {}},
	{"issue identity cleared", func(s *ResultStore, _ *ResultRecord) { s.IssueID = 0 }},
	{"issue identity changed", func(s *ResultStore, _ *ResultRecord) { s.IssueID = 43 }},
	{"record removed", func(s *ResultStore, r *ResultRecord) { delete(s.Records, r.Version) }},
	{"actor not positive", func(_ *ResultStore, r *ResultRecord) { r.ActorID = 0 }},
	{"actor changed", func(_ *ResultStore, r *ResultRecord) { r.ActorID = 99 }},
	{"comment not positive", func(_ *ResultStore, r *ResultRecord) { r.CommentID = 0 }},
	{"comment changed", func(_ *ResultStore, r *ResultRecord) { r.CommentID++ }},
	{"body changed", func(_ *ResultStore, r *ResultRecord) { r.Body += " changed" }},
	{"outcome invalid", func(_ *ResultStore, r *ResultRecord) { r.Outcomes = append([]string{"bad"}, r.Outcomes[1:]...) }},
	{"resolved with an unmet outcome", func(_ *ResultStore, r *ResultRecord) { r.Outcomes[0] = "unmet" }},
	{"status unknown", func(_ *ResultStore, r *ResultRecord) { r.Status = "weird" }},
	{"status regressed", func(_ *ResultStore, r *ResultRecord) { r.Status = "uncertain" }},
	{"status equivalent", func(_ *ResultStore, r *ResultRecord) { r.Status = "equivalent" }},
	{"status rejected", func(_ *ResultStore, r *ResultRecord) { r.Status = "rejected" }},
	{"failures removed", func(_ *ResultStore, r *ResultRecord) { r.Failures = nil }},
	{"failures rewritten", func(_ *ResultStore, r *ResultRecord) {
		if len(r.Failures) > 0 {
			r.Failures[0].Detail += "x"
		}
	}},
	{"failures extended", func(_ *ResultStore, r *ResultRecord) {
		r.Failures = append(r.Failures, ResultFailure{Action: "close", Detail: "x"})
	}},
	{"decisions removed", func(_ *ResultStore, r *ResultRecord) { r.Decisions = r.Decisions[:min(1, len(r.Decisions))] }},
	{"decisions extended", func(_ *ResultStore, r *ResultRecord) {
		r.Decisions = append(r.Decisions, ResultCloseRecord{StateVersion: "s", Decision: "no", UserReference: "u", Status: "declined"})
	}},
	{"decision identity changed", onDecision(0, func(d *ResultCloseRecord) { d.UserReference += "x" })},
	{"decision state version changed", onDecision(1, func(d *ResultCloseRecord) { d.StateVersion = "other" })},
	{"settled decision regressed", onDecision(2, func(d *ResultCloseRecord) { d.Status = "uncertain" })},
	{"decision status unknown", onDecision(0, func(d *ResultCloseRecord) { d.Status = "weird" })},
	{"consent declined", onDecision(0, func(d *ResultCloseRecord) { d.Status = "declined" })},
	{"refusal closed", onDecision(1, func(d *ResultCloseRecord) { d.Status = "closed" })},
	{"decision kind unknown", onDecision(0, func(d *ResultCloseRecord) { d.Decision = "maybe" })},
	{"decision reference empty", onDecision(0, func(d *ResultCloseRecord) { d.UserReference = "" })},
	{"checks changed", func(_ *ResultStore, r *ResultRecord) { r.Checks = nil }},
}

// TestResultStoreValidatorsAgreeWithTheOriginals compares the kernel-backed
// validators with the original functions on every single mutation and every
// pair of mutations of a store: the verdict is the same, and for one violation
// the error is the same.
func TestResultStoreValidatorsAgreeWithTheOriginals(t *testing.T) {
	_, _, id, previous, version, work := oracleStore(t)
	if err := legacyValidateResultStore(previous, previous.SourceKey, id); err != nil {
		t.Fatal(err)
	}
	apply := func(ms ...storeMutation) ResultStore {
		next := cloneStore(t, previous)
		record := next.Records[version]
		for _, m := range ms {
			m.mutate(&next, record)
		}
		return next
	}
	check := func(label string, ms ...storeMutation) {
		next := apply(ms...)
		single := len(ms) == 1
		// The comparison facts read off the kernel projections are the comparisons of the records, in
		// both directions; the failure log is a prefix by one append in the projections, so it is
		// compared only when the new log is at most one entry longer.
		if next.Records[version] != nil {
			ids := newIdentities()
			got := projectedFacts(projectRecord(previous, version, ids, work), projectRecord(next, version, ids, work))
			old, current := previous.Records[version], next.Records[version]
			want := compareRecords(old, current, work)
			if grown := len(current.Failures) - len(old.Failures); grown < 0 || grown > 1 {
				if got.prefix && !want.prefix {
					t.Errorf("%s: the projections say the failures extend the old ones", label)
				}
				got.prefix = want.prefix
			}
			if got != want {
				t.Errorf("%s: facts of the projections %+v, comparisons of the records %+v", label, got, want)
			}
		}
		for name, pair := range map[string][2]error{
			"store":  {ValidateResultStore(next, previous.SourceKey, id), legacyValidateResultStore(next, previous.SourceKey, id)},
			"update": {ValidateResultStoreUpdate(previous, next), legacyValidateResultStoreUpdate(previous, next)},
			"work":   {ValidateResultStoreWork(previous, next, work), legacyValidateResultStoreWork(previous, next, work)},
		} {
			got, want := pair[0], pair[1]
			if (got == nil) != (want == nil) || single && errText(got) != errText(want) {
				t.Errorf("%s (%s): kernel %q, original %q", label, name, errText(got), errText(want))
			}
		}
	}
	for _, m := range storeMutations {
		check(m.name, m)
	}
	for _, a := range storeMutations {
		for _, b := range storeMutations {
			check(a.name+" + "+b.name, a, b)
		}
	}
}

// TestResultStoreWorkAgreesWithTheOriginal covers the work relation of a new
// record: it is written beside the existing one with every status, work digest,
// evidence, member, commit, and version fact, and while the existing record is
// published or uncertain.
func TestResultStoreWorkAgreesWithTheOriginal(t *testing.T) {
	_, _, _, published, version, work := oracleStore(t)
	for _, oldStatus := range []string{"published", "uncertain", "rejected"} {
		previous := cloneStore(t, published)
		previous.Records[version].Status = oldStatus
		previous.Records[version].Decisions = nil
		for _, status := range []string{"uncertain", "published", "equivalent", "rejected", "weird"} {
			for variant := 0; variant < 8; variant++ {
				next := cloneStore(t, previous)
				outcomes := []string{"met", "met", "met", "unmet"}
				record := *next.Records[version]
				record.Outcomes, record.FullyResolved, record.Status = outcomes, false, status
				record.Decisions, record.Failures = nil, nil
				record.Version = resultVersion(work.TaskKey, work.Commits, outcomes, false)
				if variant&1 != 0 {
					record.CardDigest = "other"
				}
				if variant&2 != 0 {
					record.EvidenceVersion = "other"
				}
				if variant&4 != 0 {
					record.Commits = []string{"c"}
				}
				next.Records[record.Version] = &record
				label := oldStatus + "/" + status + "/" + strconv.Itoa(variant)
				got, want := ValidateResultStoreWork(previous, next, work), legacyValidateResultStoreWork(previous, next, work)
				if errText(got) != errText(want) {
					t.Errorf("%s: kernel %q, original %q", label, errText(got), errText(want))
				}
				got, want = ValidateResultStoreUpdate(previous, next), legacyValidateResultStoreUpdate(previous, next)
				if errText(got) != errText(want) {
					t.Errorf("%s (update): kernel %q, original %q", label, errText(got), errText(want))
				}
			}
		}
	}
}

// TestResultIdentitiesAreInjective covers the collision of a 32-bit digest the
// review of the first version found: the two verification records below had the
// same truncated SHA-256, so the projection said they were equal. Identities are
// positions in a table, so different values never share one.
func TestResultIdentitiesAreInjective(t *testing.T) {
	a := []ResultCheck{{Command: "go test ./pkg/check17299", Status: "pass"}}
	b := []ResultCheck{{Command: "go test ./pkg/check135313", Status: "pass"}}
	ids := newIdentities()
	if ids.checks(a) == ids.checks(b) {
		t.Fatal("different verification records share an identity")
	}
	if ids.checks(a) != ids.checks(append([]ResultCheck{}, a...)) || ids.checks(nil) == ids.checks([]ResultCheck{}) {
		t.Fatal("equal values must share an identity, and nil and empty slices are different values")
	}
	// A store whose verification evidence was rewritten to such a value is seen as changed by the facts.
	_, _, _, previous, version, work := oracleStore(t)
	next := cloneStore(t, previous)
	previous.Records[version].Checks, next.Records[version].Checks = a, b
	got := projectedFacts(projectRecord(previous, version, ids, work), projectRecord(next, version, ids, work))
	if got.checksSame || compareRecords(previous.Records[version], next.Records[version], work).checksSame {
		t.Fatalf("rewritten verification evidence looks unchanged: %+v", got)
	}
	// Distinct bodies, comments, failure logs, work sets, and decisions get distinct identities too.
	for _, pair := range [][2]uint64{
		{ids.body("a", 1), ids.body("a", 2)}, {ids.body("a", 1), ids.body("b", 1)}, {ids.comment(1), ids.comment(2)},
		{ids.failures([]ResultFailure{{Action: "x"}}), ids.failures([]ResultFailure{{Action: "y"}})},
		{ids.work(ResultWork{Digest: "a"}), ids.work(ResultWork{Digest: "b"})},
		{ids.decision(ResultCloseRecord{UserReference: "a"}), ids.decision(ResultCloseRecord{UserReference: "b"})},
	} {
		if pair[0] == pair[1] || pair[0] == 0 || pair[1] == 0 {
			t.Fatalf("identities %v", pair)
		}
	}
}
