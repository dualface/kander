package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// legacyRoundFile rewrites one schema 2 dispatch file the way the old binary wrote it.
func legacyRoundFile(t *testing.T, path string, change func(*Dispatch)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d Dispatch
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	d.Schema, d.Cycle, d.PreviousID = 1, "", ""
	if change != nil {
		change(&d)
	}
	if err = os.WriteFile(path, []byte(reviewJSON(d)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func legacyRound(t *testing.T, root, id, name string, change func(*Dispatch)) {
	t.Helper()
	dir := filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "dispatches", name)
	legacyRoundFile(t, filepath.Join(dir, "state.json"), change)
	legacyRoundFile(t, filepath.Join(dir, "intent.json"), nil)
}

// silentRound copies a prepared round into a cancelled schema 1 round with no
// receipt, which is what an old binary left when it replaced the round.
func silentRound(t *testing.T, root, id string, from Dispatch, name string, created time.Time) {
	t.Helper()
	base := filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "dispatches")
	target := filepath.Join(base, name)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"state.json", "intent.json"} {
		d := from
		d.Input.ID, d.Input.CreatedAt, d.Input.ConfirmBy = name, created, created.Add(time.Minute)
		d.Authorization.DispatchID = name
		if file == "state.json" {
			d.State = DispatchCancelled
		}
		if err := os.WriteFile(filepath.Join(target, file), []byte(reviewJSON(d)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	legacyRound(t, root, id, name, func(d *Dispatch) { d.Accepted, d.Completed, d.Release, d.Settlement = nil, nil, nil, nil })
}

func datedRound(s Snapshot, name string, created time.Time) DispatchInput {
	in := dispatchInput(s, name)
	in.CreatedAt, in.ConfirmBy = created, time.Now().Add(time.Hour)
	return in
}

// roundNames lists the rounds of a card in the order the board reads them.
func roundNames(t *testing.T, root, id string) []string {
	t.Helper()
	var names []string
	err := WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(id)
		if e != nil {
			return e
		}
		rounds, e := dispatchRounds(tx, s)
		for _, d := range rounds {
			names = append(names, d.Input.ID)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// silentRoundsCard is a member with the real rounds first and last, a silent round
// for each name between them, and the last round bound. The checkpoint saw the first.
func silentRoundsCard(t *testing.T, root, slug string, silent ...string) (string, CoordinatorCheckpoint) {
	t.Helper()
	s := coordinatorCard(t, root, slug)
	id := s.Entry.TaskID
	c := coordinatorClaim(t, root, id)
	begin := time.Now().Add(-time.Hour).Truncate(time.Second).UTC()
	first := prepareTestDispatch(t, root, datedRound(s, "round-first", begin))
	legacyRound(t, root, id, first.Input.ID, nil)
	if _, err := dispatchMove(t, root, first, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchMove(t, root, first, "review"); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDispatch(root, id, first.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = SettleDispatch(root, id, first.Input.ID, read.Revision); err != nil {
		t.Fatal(err)
	}
	c = coordinatorReconcile(t, root, c)
	for i, name := range silent {
		silentRound(t, root, id, first, name, begin.Add(time.Duration(i+1)*time.Minute))
	}
	s = transactionSnapshot(t, root, id)
	last := prepareTestDispatch(t, root, datedRound(s, "round-last", begin.Add(time.Duration(len(silent)+1)*time.Minute)))
	legacyRound(t, root, id, last.Input.ID, nil)
	return id, c
}

func TestSilentRoundKeepsItsPlace(t *testing.T) {
	for name, silent := range map[string][]string{"one": {"silent-a"}, "several": {"silent-a", "silent-b", "silent-c"}} {
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id, c := silentRoundsCard(t, root, "silent-place", silent...)
			want := slices.Concat([]string{"round-first"}, silent, []string{"round-last"})
			if got := roundNames(t, root, id); !slices.Equal(got, want) {
				t.Fatalf("rounds %v, want %v", got, want)
			}
			m := coordinatorReconcile(t, root, c).Members[id]
			if m.Blocked != nil || m.Dispatch == nil || m.Dispatch.ID != "round-last" {
				t.Fatalf("mirror %+v", m)
			}
		})
	}
}

func TestPrepareDispatchFollowsSilentRounds(t *testing.T) {
	root := tempBoard(t)
	id, _ := silentRoundsCard(t, root, "silent-prepare", "silent-a", "silent-b")
	last, err := ReadDispatch(root, id, "round-last")
	if err != nil {
		t.Fatal(err)
	}
	if err = EndDispatch(root, id, last.Input.ID, last.Revision, DispatchCancelled, "next round"); err != nil {
		t.Fatal(err)
	}
	next := prepareTestDispatch(t, root, dispatchInput(transactionSnapshot(t, root, id), "round-next"))
	if next.PreviousID != "round-last" {
		t.Fatalf("round %+v", next)
	}
}

func TestOrderLegacyDispatchesPlacesRoundsWithoutReceipts(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2026, 10, 1, 12, minute, 0, 0, time.UTC) }
	receipted := func(id string, revision uint64, created time.Time) Dispatch {
		return Dispatch{Schema: 1, Input: DispatchInput{ID: id, CreatedAt: created}, Accepted: &DispatchReceipt{CardRevision: revision}}
	}
	silent := func(id string, created time.Time) Dispatch {
		return Dispatch{Schema: 1, Input: DispatchInput{ID: id, CreatedAt: created}}
	}
	cases := []struct {
		name    string
		records []Dispatch
		want    string
	}{
		{"middle", []Dispatch{receipted("a", 3, at(1)), receipted("c", 9, at(3)), silent("b", at(2))}, "a b c"},
		{"first", []Dispatch{receipted("b", 3, at(2)), silent("a", at(1))}, "a b"},
		{"last", []Dispatch{silent("b", at(2)), receipted("a", 3, at(1))}, "a b"},
		{"several", []Dispatch{silent("c", at(3)), receipted("d", 9, at(4)), silent("b", at(2)), receipted("a", 3, at(1))}, "a b c d"},
		{"a release is a receipt", []Dispatch{{Schema: 1, Input: DispatchInput{ID: "a", CreatedAt: at(9)}, Release: &DispatchRelease{CardRevision: 3}}, receipted("b", 9, at(1))}, "a b"},
		{"equal times beside receipts only", []Dispatch{receipted("a", 3, at(1)), receipted("b", 9, at(1)), silent("c", at(2))}, "a b c"},
		{"missing time", []Dispatch{receipted("a", 3, at(1)), silent("b", time.Time{})}, ""},
		{"same time as the next round", []Dispatch{receipted("a", 3, at(1)), silent("b", at(2)), receipted("c", 9, at(2))}, ""},
		{"same time as the previous round", []Dispatch{receipted("a", 3, at(2)), silent("b", at(2)), receipted("c", 9, at(3))}, ""},
		{"same time as another silent round", []Dispatch{silent("a", at(1)), silent("b", at(1))}, ""},
		{"times disagree with the receipts", []Dispatch{receipted("a", 3, at(5)), receipted("c", 9, at(1)), silent("b", at(3))}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ordered, err := orderLegacyDispatches("task", c.records)
			if c.want == "" {
				if err == nil || !strings.Contains(err.Error(), "legacy dispatch order unknown") {
					t.Fatalf("ordered %+v err=%v", ordered, err)
				}
				return
			}
			var got []string
			for _, d := range ordered {
				got = append(got, d.Input.ID)
			}
			if err != nil || strings.Join(got, " ") != c.want {
				t.Fatalf("ordered %v, want %s, err=%v", got, c.want, err)
			}
		})
	}
}
