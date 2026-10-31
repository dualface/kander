package board

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const legacyAttemptID = "old-attempt"

// legacyRunningCard is a card 1.0.0 started: a succeeded schema 1 attempt and no
// transition.
func legacyRunningCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := legacyStartedCard(t, root, slug)
	legacyAttempt(t, root, s, legacyAttemptID, "succeeded", s.Revision)
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

// legacyTakenOverCard adds what a 1.0.0 takeover left: new OWNER, SESSION and
// WINDOW on the card, and the attempt as it was.
func legacyTakenOverCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := legacyRunningCard(t, root, slug)
	if err := writeLegacyTakeover(root, s, "claude", "claude old-takeover", "tmux:old"); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

var legacyFixtures = []struct {
	name  string
	build func(*testing.T, string, string) Snapshot
}{
	{"started", legacyRunningCard},
	{"taken over", legacyTakenOverCard},
}

// legacyFirstWrites holds every caller of putCurrentRecord. s carries a bound cursor.
var legacyFirstWrites = []struct {
	name  string
	write func(t *testing.T, root string, s Snapshot) error
}{
	{"window", func(_ *testing.T, root string, s Snapshot) error {
		_, err := RecordExecutionWindow(root, s.Entry, "tmux:drifted", "")
		return err
	}},
	{"session", func(t *testing.T, root string, s Snapshot) error {
		_, err := RecordExecutionSession(t.Context(), root, s.Entry, MetadataFrom(s.Text, FieldSession), "repaired session")
		return err
	}},
	{"process", func(_ *testing.T, root string, s Snapshot) error {
		_, err := RecordExecutionProcess(root, s.Entry, ExecutionProcess{Launcher: "foreground", AgentPID: 7})
		return err
	}},
	{"window restore", func(_ *testing.T, root string, s Snapshot) error {
		text, err := setMetadata(s.Text, FieldWindow, "tmux:restored")
		if err != nil {
			return err
		}
		return RollbackDocument(root, s.Entry, text, "")
	}},
}

func boundLegacy(t *testing.T, root, id string) Snapshot {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	return s
}

func legacyOriginals(t *testing.T, root, id string) map[string]string {
	t.Helper()
	originals := map[string]string{}
	for _, name := range []string{"pending.json", "result.json"} {
		data, err := os.ReadFile(filepath.Join(control(root, "groups", taskStartGroup, id), legacyAttemptID, name))
		if err != nil {
			t.Fatal(err)
		}
		originals[name] = string(data)
	}
	return originals
}

// requireRecordMatchesCard reads the current record and compares it with the card.
func requireRecordMatchesCard(t *testing.T, root, id string) ExecutionRecord {
	t.Helper()
	r, exists, err := CurrentExecution(root, id)
	if err != nil || !exists {
		t.Fatalf("current record: exists=%v %v", exists, err)
	}
	owner, session, window := cardExecutionFacts(transactionSnapshot(t, root, id))
	if r.Owner != owner || r.Session != session || r.Window != window {
		t.Fatalf("record %+v, card %q %q %q", r, owner, session, window)
	}
	return r
}

func TestFirstWriteKeepsALegacyCardWorking(t *testing.T) {
	for _, fixture := range legacyFixtures {
		for _, first := range legacyFirstWrites {
			t.Run(fixture.name+"/"+first.name, func(t *testing.T) {
				root := tempBoard(t)
				id := fixture.build(t, root, "legacy-first").Entry.TaskID
				originals := legacyOriginals(t, root, id)
				c := coordinatorClaim(t, root, id)
				if err := first.write(t, root, boundLegacy(t, root, id)); err != nil {
					t.Fatal(err)
				}
				requireRecordMatchesCard(t, root, id)
				if c = coordinatorReconcile(t, root, c); c.Members[id].AwaitingStart {
					t.Fatalf("mirror %+v", c.Members[id])
				}
				if _, err := RecordExecutionWindow(root, boundLegacy(t, root, id).Entry, "tmux:second", ""); err != nil {
					t.Fatal("second write: ", err)
				}
				requireRecordMatchesCard(t, root, id)
				prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "first-write-dispatch"))
				if got := legacyOriginals(t, root, id); !reflect.DeepEqual(got, originals) {
					t.Fatalf("the attempt originals changed: %v", got)
				}
			})
		}
	}
}

// The old takeover gets a record of its own, linked after the attempt it replaced.
func TestFirstWriteLinksAnOldTakeover(t *testing.T) {
	root := tempBoard(t)
	s := legacyTakenOverCard(t, root, "legacy-owner")
	id := s.Entry.TaskID
	if view := ledgerOf(t, root, id); view.current.ID == legacyAttemptID || view.current.PreviousID != legacyAttemptID || view.current.Owner != "claude" {
		t.Fatalf("view %+v", view.current)
	}
	if _, err := RecordExecutionWindow(root, boundLegacy(t, root, id).Entry, "tmux:drifted", ""); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, root, id)
	if l.current.ID == legacyAttemptID || l.current.PreviousID != legacyAttemptID || l.current.Kind != ExecutionTakeover || l.current.Owner != "claude" {
		t.Fatalf("record %+v", l.current)
	}
	if got := strings.Join(chainKinds(l), " "); got != "start confirm handoff window-update" {
		t.Fatalf("chain %s", got)
	}
	handoff := l.chain[l.synthesized]
	if handoff.FromRecord != legacyAttemptID || handoff.ToRecord != l.current.ID || handoff.OldOwner != "codex" || handoff.NewOwner != "claude" {
		t.Fatalf("handoff %+v", handoff)
	}
}

// A takeover on a card an old takeover changed is itself a first write.
func TestTakeoverAroundAFirstWrite(t *testing.T) {
	cases := map[string]bool{"before the first write": false, "after the first write": true}
	for name, written := range cases {
		for _, fixture := range legacyFixtures {
			t.Run(fixture.name+" "+name, func(t *testing.T) {
				root := tempBoard(t)
				id := fixture.build(t, root, "legacy-next").Entry.TaskID
				c := coordinatorClaim(t, root, id)
				if written {
					if _, err := RecordExecutionWindow(root, boundLegacy(t, root, id).Entry, "tmux:drifted", ""); err != nil {
						t.Fatal(err)
					}
				}
				s := boundLegacy(t, root, id)
				if _, err := BeginTakeover(root, s.Entry, TakeoverInput{Owner: "grok", Session: "grok next"}); err != nil {
					t.Fatal(err)
				}
				if err := ConfirmTaskStart(root, s.Entry); err != nil {
					t.Fatal(err)
				}
				if r := requireRecordMatchesCard(t, root, id); r.Owner != "grok" {
					t.Fatalf("record %+v", r)
				}
				if c = coordinatorReconcile(t, root, c); c.Members[id].AwaitingStart {
					t.Fatalf("mirror %+v", c.Members[id])
				}
			})
		}
	}
}

// The stored links are numbered after the synthesized prefix, so a prefix that
// followed OWNER would break the chain at the next change of OWNER.
func TestSynthesizedPrefixIgnoresTheCardOwner(t *testing.T) {
	builds := map[string]func(*testing.T, string, string) Snapshot{"attempt": legacyRunningCard, "no attempt": legacyStartedCard}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			s := build(t, root, "legacy-prefix")
			id := s.Entry.TaskID
			if _, err := RecordExecutionWindow(root, boundLegacy(t, root, id).Entry, "tmux:drifted", ""); err != nil {
				t.Fatal(err)
			}
			before := ledgerOf(t, root, id)
			next := boundLegacy(t, root, id)
			if _, err := BeginTakeover(root, next.Entry, TakeoverInput{Owner: "grok", Session: "grok next"}); err != nil {
				t.Fatal(err)
			}
			after := ledgerOf(t, root, id)
			if before.synthesized == 0 || after.synthesized != before.synthesized || !reflect.DeepEqual(after.chain[:after.synthesized], before.chain[:before.synthesized]) {
				t.Fatalf("prefix moved:\n%+v\n%+v", before.chain[:before.synthesized], after.chain[:after.synthesized])
			}
		})
	}
	t.Run("old takeover", func(t *testing.T) {
		root := tempBoard(t)
		s := legacyRunningCard(t, root, "legacy-old-owner")
		id := s.Entry.TaskID
		before := ledgerOf(t, root, id)
		if err := writeLegacyTakeover(root, s, "claude", "claude old-takeover", "tmux:old"); err != nil {
			t.Fatal(err)
		}
		if after := ledgerOf(t, root, id); !reflect.DeepEqual(after.chain, before.chain) {
			t.Fatalf("prefix moved:\n%+v\n%+v", before.chain, after.chain)
		}
	})
}

// The first write adds no tolerance: a stored record that lost a fact stays damage.
func TestFirstWriteKeepsTheLedgerChecks(t *testing.T) {
	root := tempBoard(t)
	id := legacyRunningCard(t, root, "legacy-tamper").Entry.TaskID
	if _, err := RecordExecutionWindow(root, boundLegacy(t, root, id).Entry, "tmux:drifted", ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(control(root, "groups", taskStartGroup, id), "current.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record ExecutionRecord
	if err = DecodeReviewJSON(good, &record); err != nil {
		t.Fatal(err)
	}
	tamper := map[string]struct {
		change func(*ExecutionRecord)
		want   string
	}{
		"finished revision": {func(r *ExecutionRecord) { r.FinishedRevision = 0 }, "start result original"},
		"owner":             {func(r *ExecutionRecord) { r.Owner = "intruder" }, "start attempt original"},
	}
	for name, c := range tamper {
		t.Run(name, func(t *testing.T) {
			bad := record
			c.change(&bad)
			if err := os.WriteFile(path, []byte(reviewJSON(bad)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := CurrentExecution(root, id); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}
