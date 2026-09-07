package liveness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func factsMember(t *testing.T) (string, subscribeOptions, string) {
	t.Helper()
	root := tempBoard(t)
	id, path := makeWorking(t, "facts-member", "Member")
	group := "20260908-facts-group"
	setTaskGroup(t, path, group)
	if _, err := board.MoveEntry(currentEntry(t, root, id), root, "review"); err != nil {
		t.Fatal(err)
	}
	return root, subscribeOptions{Group: group, Members: []string{id}, Refresh: .001, Heartbeat: .02}, id
}

func factsUpdate(root, id string, change func(string) string) error {
	snapshot, err := board.ReadSnapshot(root, id)
	if err != nil {
		return err
	}
	return board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: change(snapshot.Text), ExpectedRevision: snapshot.Revision})
}

func factsEvents(t *testing.T, root string, opts subscribeOptions, onEvent func(groupEvent) error) ([]groupEvent, error) {
	t.Helper()
	// Mutations in the snapshot callback must finish within one scan interval.
	// Output delivery is asynchronous; these facts tests intentionally use a
	// slower scan to exercise changes between observations.
	opts.Refresh = max(opts.Refresh, .25)
	opts.Heartbeat = max(opts.Heartbeat, .5)
	stop := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(stop) }) }
	timer := time.AfterFunc(5*time.Second, finish)
	defer timer.Stop()
	writer := &clockEvents{onEvent: func(event groupEvent) error {
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return err
			}
		}
		if event.Event == "heartbeat" {
			finish()
		}
		return nil
	}}
	err := Subscribe(root, opts, writer, stop)
	for i, event := range writer.events {
		if event.SchemaVersion != 1 || event.Seq != uint64(i+1) || len(event.SubscriptionID) != 32 || event.ObservedAt.IsZero() {
			t.Fatalf("invalid envelope: %+v", event)
		}
		if event.SubscriptionID != writer.events[0].SubscriptionID {
			t.Fatal("subscription ID changed")
		}
	}
	return writer.events, err
}

// Reverse the historical invisible round trip using committed directory moves.
func TestAuditRoundTripIsInvisible(t *testing.T) {
	for _, roundTrip := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-state", true: "round-trip"}[roundTrip], func(t *testing.T) {
			root, opts, id := factsMember(t)
			opts.Refresh, opts.Heartbeat = 1, 1.5
			var dispatch board.Dispatch
			if roundTrip {
				dispatch = prepareSubscriptionDispatch(t, root, id, "sync", time.Minute)
			}
			events, err := factsEvents(t, root, opts, func(event groupEvent) error {
				if event.Event != "snapshot" {
					return nil
				}
				if roundTrip {
					if _, err := board.MoveWithOptions(currentEntry(t, root, id), root, "working", board.MoveOptions{Authorization: dispatch.Authorization}); err != nil {
						return err
					}
				}
				snapshot, err := board.ReadSnapshot(root, id)
				if err != nil {
					return err
				}
				if err := board.UpdateDocument(root, id, board.UpdateOptions{Document: "spec.md", Text: snapshot.Text + "\nDelivery updated.\n", ExpectedRevision: snapshot.Revision, Authorization: dispatch.Authorization}); err != nil {
					return err
				}
				if roundTrip {
					_, err := board.MoveWithOptions(currentEntry(t, root, id), root, "review", board.MoveOptions{Authorization: dispatch.Authorization, DeliveryCommit: strings.Repeat("b", 40)})
					return err
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 3 || events[1].Event != "task-update" || !reflect.DeepEqual(events[1].Updated, []string{id}) || len(events[1].Changed) != 0 {
				t.Fatalf("events: %+v", events)
			}
			delta := uint64(1)
			if roundTrip {
				delta = 3
			}
			if events[1].TaskRevisions[id] != events[0].TaskRevisions[id]+delta || events[1].Tasks[id] != "review" {
				t.Fatalf("revision facts: %+v", events)
			}
			if roundTrip {
				assertSubscriptionReceipt(t, events[1], id, dispatch, "review")
			} else if len(events[1].Dispatches) != 0 {
				t.Fatal("legacy update fabricated business completion")
			}
		})
	}
}

func addFactsExternal(t *testing.T, root, slug, group string) string {
	t.Helper()
	_, err := board.NewTask(root, "chore", slug, "External", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = factsUpdate(root, todayID(slug), func(text string) string { return strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1) }); err != nil {
		t.Fatal(err)
	}
	return todayID(slug)
}

// Reverse frozen group expansion and compare a restarted snapshot to dependencies.
func TestAuditWatchedGroupMembershipIsFrozen(t *testing.T) {
	root, opts, id := factsMember(t)
	group := "20260908-facts-external-group"
	first := addFactsExternal(t, root, "facts-first", group)
	opts.Watch = []string{group}
	var added string
	events, err := factsEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			added = addFactsExternal(t, root, "facts-added", group)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[1].Event != "membership-change" || len(events[1].Watched) != 2 || events[1].Tasks[added] != "backlog" || events[1].TaskRevisions[added] == 0 || events[1].Tasks[first] != "backlog" {
		t.Fatalf("events: %+v", events)
	}
	if events[0].MembershipVersions[group] == events[1].MembershipVersions[group] || !reflect.DeepEqual(events[1].WatchReferences, opts.Watch) {
		t.Fatal("missing membership version/reference")
	}
	restarted, err := factsEvents(t, root, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	scanned, err := board.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	text, err := scanned.Document(id)
	if err != nil {
		t.Fatal(err)
	}
	// Dependency resolution consumes the same immutable membership; only the
	// source contract is supplied separately to avoid changing a frozen card.
	docs := map[string]string{id: strings.Replace(text, "## DISCUSSION", "## DISCUSSION\n\n```text\nPREREQUISITES: "+group+"\n```", 1)}
	deps, err := board.TaskDependenciesOf(scanned.Entries[id], scanned, docs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted[0].Watched, deps.ExpandedTaskIDs) {
		t.Fatalf("restart %v dependencies %v", restarted[0].Watched, deps.ExpandedTaskIDs)
	}
}

func TestSubscribeMembershipRemovalRequiresReconciliation(t *testing.T) {
	for _, mode := range []string{"removed", "regrouped"} {
		t.Run(mode, func(t *testing.T) {
			root, opts, _ := factsMember(t)
			group := "20260908-removal-group"
			external := addFactsExternal(t, root, "facts-removal", group)
			opts.Watch = []string{group}
			events, err := factsEvents(t, root, opts, func(event groupEvent) error {
				if event.Event != "snapshot" {
					return nil
				}
				if mode == "removed" {
					return os.RemoveAll(currentEntry(t, root, external).Path)
				}
				return factsUpdate(root, external, func(text string) string { return strings.Replace(text, group, "20260908-another-group", 1) })
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 3 || events[1].Event != "membership-change" || !reflect.DeepEqual(events[1].Removed, []string{external}) || len(events[1].Memberships[group]) != 0 {
				t.Fatalf("events: %+v", events)
			}
			if !events[1].ReconciliationRequired || !events[2].ReconciliationRequired || !events[2].MembershipComplete {
				t.Fatal("removal silently released dependency")
			}
		})
	}
}

// Reverse silent omission, both on startup and after the initial snapshot.
func TestAuditWatchedGroupSilentlyOmitsUnreadableMember(t *testing.T) {
	for _, atStart := range []bool{true, false} {
		t.Run(map[bool]string{true: "startup", false: "refresh"}[atStart], func(t *testing.T) {
			root, opts, _ := factsMember(t)
			group := "20260908-unreadable-group"
			addFactsExternal(t, root, "facts-readable", group)
			bad := addFactsExternal(t, root, "facts-unreadable", group)
			opts.Watch = []string{group}
			corrupt := func() error { return os.WriteFile(currentEntry(t, root, bad).Document, []byte{0xff}, 0600) }
			if atStart {
				if err := corrupt(); err != nil {
					t.Fatal(err)
				}
			}
			events, err := factsEvents(t, root, opts, func(event groupEvent) error {
				if !atStart && event.Event == "snapshot" {
					return corrupt()
				}
				return nil
			})
			if err == nil {
				t.Fatal("unreadable member accepted")
			}
			last := events[len(events)-1]
			if last.Event != "membership-unknown" || last.MembershipComplete || !last.ReconciliationRequired || last.ReadStatus != "invalid" {
				t.Fatalf("unsafe event: %+v", last)
			}
			scanned, err := board.Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			if scanned.GroupMembership().Err() == nil {
				t.Fatal("lost document failure")
			}
		})
	}
}

func TestSubscribeRejectsDuplicateTargetsAndUnknownStructure(t *testing.T) {
	for _, mode := range []string{"member-alias", "watch-alias", "overlap", "group-overlap", "duplicate-card", "missing-spec", "unrelated-invalid", "own-regrouped", "invalid-group", "duplicate-group"} {
		t.Run(mode, func(t *testing.T) {
			root, opts, id := factsMember(t)
			group := "20260908-duplicates-group"
			external := addFactsExternal(t, root, "facts-external", group)
			opts.Watch = []string{group}
			switch mode {
			case "member-alias":
				opts.Members = append(opts.Members, id+".md")
			case "watch-alias":
				opts.Watch = []string{external, external + ".md"}
			case "overlap":
				opts.Watch = []string{id}
			case "group-overlap":
				opts.Watch = append(opts.Watch, external)
			case "duplicate-card":
				data, err := os.ReadFile(currentEntry(t, root, external).Document)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, "todo", external+".md"), data, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-spec":
				if err := os.Remove(currentEntry(t, root, external).Document); err != nil {
					t.Fatal(err)
				}
			case "unrelated-invalid":
				if err := os.WriteFile(filepath.Join(root, "backlog", "unknown.txt"), []byte("unknown owner"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-group", "duplicate-group":
				path := currentEntry(t, root, external).Document
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				if mode == "invalid-group" {
					text = strings.Replace(text, group, "not-a-group", 1)
				} else {
					text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: 20260908-other-group\n- TASK_GROUP:", 1)
				}
				if err = os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			case "own-regrouped":
				opts.Group = "20260908-wrong-group"
			}
			_, err := factsEvents(t, root, opts, nil)
			if err == nil {
				t.Fatal("invalid membership accepted")
			}
		})
	}
}

func TestSubscribeExplicitTargetsIgnoreUnrelatedKnownProblems(t *testing.T) {
	root, opts, _ := factsMember(t)
	if err := os.WriteFile(filepath.Join(root, "backlog", "unrelated.txt"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	events, err := factsEvents(t, root, opts, nil)
	if err != nil || len(events) != 2 {
		t.Fatalf("unrelated scan: %+v %v", events, err)
	}
	// Exercise the actual wire shape, including explicit completeness flags.
	raw, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["task_revisions"] == nil || wire["membership_complete"] != true || wire["read_status"] != "committed" {
		t.Fatalf("wire: %s", raw)
	}
}

func TestSubscribeRegroupWithinWatchedUnionRequiresReconciliation(t *testing.T) {
	root, opts, _ := factsMember(t)
	first, second := "20260908-first-group", "20260908-second-group"
	moved := addFactsExternal(t, root, "facts-regroup", first)
	addFactsExternal(t, root, "facts-stays", second)
	opts.Watch = []string{first, second}
	events, err := factsEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			return factsUpdate(root, moved, func(text string) string { return strings.Replace(text, first, second, 1) })
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Event == "membership-change" {
			found = true
			if len(event.Watched) != 2 || !reflect.DeepEqual(event.Removed, []string{moved}) || !event.ReconciliationRequired {
				t.Fatalf("ownership loss hidden by union: %+v", event)
			}
		}
	}
	if !found {
		t.Fatal("missing ownership change")
	}
}
