package board

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// CoordinatorAuthority fences checkpoint writers only. It grants no task,
// integration, transport or cleanup authority.
type CoordinatorAuthority struct {
	Owner string `json:"owner"`
	Token string `json:"token"`
	Epoch uint64 `json:"epoch"`
}

// CoordinatorCheckpoint is a cursor over producer-owned facts, not a task state
// or a replacement for review originals. Every path is relative to its task.
type CoordinatorCheckpoint struct {
	ContentHash     string                       `json:"content_hash"`
	Schema          int                          `json:"schema"`
	GroupID         string                       `json:"group_id"`
	Revision        uint64                       `json:"revision"`
	Authority       CoordinatorAuthority         `json:"authority"`
	ClaimHash       string                       `json:"claim_hash"`
	LastRequestHash string                       `json:"last_request_hash,omitempty"`
	Basis           string                       `json:"basis"`
	MemberVersion   string                       `json:"member_version"`
	Members         map[string]CoordinatorMember `json:"members"`
}

type CoordinatorMember struct {
	Revision       uint64               `json:"revision"`
	Cycle          string               `json:"cycle"`
	AwaitingStart  bool                 `json:"awaiting_start,omitempty"`
	StartAttempt   string               `json:"start_attempt,omitempty"`
	State          string               `json:"state"`
	DeliveryCommit string               `json:"delivery_commit,omitempty"`
	Dispatch       *CoordinatorDispatch `json:"dispatch,omitempty"`
	Review         *CoordinatorReview   `json:"review,omitempty"`
}

type CoordinatorDispatch struct {
	ID                  string             `json:"dispatch_id"`
	Epoch               uint64             `json:"epoch"`
	Base                string             `json:"base"`
	Kind                string             `json:"kind"`
	Revision            uint64             `json:"revision"`
	State               DispatchState      `json:"state"`
	PendingConfirmation bool               `json:"pending_confirmation"`
	PendingDelivery     bool               `json:"pending_delivery"`
	PendingWrapUp       bool               `json:"pending_wrap_up"`
	Intent              ArtifactReference  `json:"intent"`
	Integration         *ArtifactReference `json:"integration,omitempty"`
	WrapUpAuthority     *ArtifactReference `json:"wrap_up_authority,omitempty"`
}

type CoordinatorReview struct {
	PlanID  string              `json:"plan_id"`
	Status  string              `json:"status"`
	Batches []string            `json:"batch_ids"`
	Runs    []ArtifactReference `json:"runs"`
}

type CoordinatorClaim struct {
	GroupID          string   `json:"group_id"`
	ExpectedRevision uint64   `json:"expected_revision"`
	ExpectedEpoch    uint64   `json:"expected_epoch"`
	Owner            string   `json:"owner"`
	Token            string   `json:"token"`
	Basis            string   `json:"basis"`
	Members          []string `json:"members"`
}

// CoordinatorObservation names exactly the round the caller is reconciling.
// A new current dispatch is never adopted merely because an event mentions it.
type CoordinatorObservation struct {
	Revision       uint64 `json:"revision"`
	DispatchID     string `json:"dispatch_id,omitempty"`
	Epoch          uint64 `json:"epoch,omitempty"`
	Base           string `json:"base,omitempty"`
	DeliveryCommit string `json:"delivery_commit,omitempty"`
}

type CoordinatorReconcile struct {
	CWD              string                            `json:"cwd,omitempty"`
	GroupID          string                            `json:"group_id"`
	ExpectedRevision uint64                            `json:"expected_revision"`
	Authority        CoordinatorAuthority              `json:"authority"`
	Members          map[string]CoordinatorObservation `json:"members"`
}

// CoordinatorGitFacts passes validated originals to the Git-aware consumer.
// Checkpoints retain only relative references, never these copied reports.
type CoordinatorGitFacts struct {
	Integration DispatchIntegration
	Closures    []ReviewClosure
	Delivery    *CoordinatorDelivery
}

// CoordinatorDelivery binds a first delivery to the card's recorded branch and
// current revision. It is not evidence that the group received that commit.
type CoordinatorDelivery struct {
	TaskID string
	Branch string
	Commit string
}

func coordinatorError(detail string) error { return kanbanError("board.coordinator_conflict", detail) }
func coordinatorHash(v any) string         { b, _ := json.Marshal(v); return messageHash(string(b)) }

func checkpointIDs(c CoordinatorCheckpoint) []string {
	ids := make([]string, 0, len(c.Members))
	for id := range c.Members {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func readCheckpoint(tx *Transaction, group string) (c CoordinatorCheckpoint, exists bool, err error) {
	b, exists, err := tx.ReadGroup(group, "checkpoint.json")
	if err != nil || !exists {
		return c, exists, err
	}
	if err = DecodeReviewJSON(b, &c); err != nil {
		return c, true, err
	}
	if c.Schema != 1 || c.GroupID != group || c.Revision == 0 || c.Authority.Epoch == 0 || strings.TrimSpace(c.Authority.Owner) == "" || !validDispatchID(c.Authority.Token) || c.ClaimHash == "" || c.Basis == "" || len(c.Members) == 0 || c.MemberVersion != coordinatorHash(checkpointIDs(c)) {
		return c, true, coordinatorError("checkpoint identity or membership")
	}
	if _, err = orderedIDs(checkpointIDs(c), false); err != nil {
		return c, true, err
	}
	copy := c
	copy.ContentHash = ""
	if c.ContentHash != coordinatorHash(copy) {
		return c, true, coordinatorError("checkpoint content digest")
	}
	history, ok, e := tx.ReadGroup(group, "checkpoints/"+strconv.FormatUint(c.Revision, 10)+".json")
	if e != nil {
		return c, true, e
	}
	if !ok || string(history) != reviewJSON(c) {
		return c, true, coordinatorError("checkpoint history binding")
	}
	return c, true, nil
}

func putCheckpoint(tx *Transaction, c *CoordinatorCheckpoint) error {
	c.ContentHash = ""
	c.ContentHash = coordinatorHash(*c)
	name := "checkpoints/" + strconv.FormatUint(c.Revision, 10) + ".json"
	if _, exists, err := tx.ReadGroup(c.GroupID, name); err != nil {
		return err
	} else if exists {
		return coordinatorError("checkpoint history already exists")
	}
	if err := tx.PutGroup(c.GroupID, name, reviewJSON(c)); err != nil {
		return err
	}
	return tx.PutGroup(c.GroupID, "checkpoint.json", reviewJSON(c))
}

// ReadCoordinatorCheckpoint reads only a committed cursor; pending transactions
// fail explicitly and require the existing init maintenance protocol.
func ReadCoordinatorCheckpoint(root, group string) (c CoordinatorCheckpoint, err error) {
	return ReadCoordinatorCheckpointContext(context.Background(), root, group)
}

// ReadCoordinatorCheckpointContext bounds contention during recovery reads.
func ReadCoordinatorCheckpointContext(ctx context.Context, root, group string) (c CoordinatorCheckpoint, err error) {
	err = WithTransactionContext(ctx, root, LockScope{Groups: []string{group}, ReadOnly: true}, func(tx *Transaction) error {
		var exists bool
		c, exists, err = readCheckpoint(tx, group)
		if err == nil && !exists {
			return coordinatorError("checkpoint missing")
		}
		return err
	})
	return
}

// ClaimCoordinator creates a checkpoint or explicitly fences an earlier writer
// under group CAS. Replaying an identical claim preserves its epoch and cursor.
func ClaimCoordinator(ctx context.Context, root string, r CoordinatorClaim) (c CoordinatorCheckpoint, err error) {
	ids, err := orderedIDs(r.Members, false)
	if err != nil {
		return c, err
	}
	if len(ids) == 0 || len(ids) != len(r.Members) || strings.TrimSpace(r.Owner) == "" || !validDispatchID(r.Token) || strings.TrimSpace(r.Basis) == "" {
		return c, coordinatorError("claim identity, basis or members")
	}
	r.Members = ids
	err = WithTransactionContext(ctx, root, LockScope{Groups: []string{r.GroupID, taskStartGroup}, Tasks: ids, ExclusiveBoard: true}, func(tx *Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var exists bool
		c, exists, err = readCheckpoint(tx, r.GroupID)
		if err != nil {
			return err
		}
		if exists && c.ClaimHash == coordinatorHash(r) {
			return nil
		}
		if c.Revision != r.ExpectedRevision || c.Authority.Epoch != r.ExpectedEpoch || c.Revision == ^uint64(0) || c.Authority.Epoch == ^uint64(0) || exists && c.Authority.Token == r.Token {
			return coordinatorError("claim CAS or reused token")
		}
		if exists && !slices.Equal(checkpointIDs(c), ids) {
			return coordinatorError("claim cannot change membership")
		}
		if err := coordinatorMembership(tx, r.GroupID, ids); err != nil {
			return err
		}
		if !exists {
			c = CoordinatorCheckpoint{Schema: 1, GroupID: r.GroupID, MemberVersion: coordinatorHash(ids), Members: map[string]CoordinatorMember{}}
		}
		for _, id := range ids {
			if _, ok := c.Members[id]; ok {
				continue
			}
			s, e := tx.Snapshot(id)
			if e != nil {
				return e
			}
			m := CoordinatorMember{Revision: s.Revision, Cycle: planCycle(s), State: s.Entry.State, AwaitingStart: MetadataFrom(s.Text, FieldStartedAt) == "" && (s.Entry.State == "todo" || s.Entry.State == "backlog")}
			if a, exists, e := readTaskStart(tx, id, ""); e != nil {
				return e
			} else if exists {
				m.StartAttempt = a.ID
				if a.Status == "pending" {
					m.Cycle, m.AwaitingStart = ReviewDigest([]byte(id+"\n")), true
				}
			}
			c.Members[id] = m
		}
		c.Revision++
		c.Authority = CoordinatorAuthority{Owner: r.Owner, Token: r.Token, Epoch: c.Authority.Epoch + 1}
		c.ClaimHash, c.LastRequestHash, c.Basis = coordinatorHash(r), "", r.Basis
		return putCheckpoint(tx, &c)
	})
	return
}

// coordinatorMembership runs under the exclusive board lock. This freezes
// topology while metadata is read and rejects partial or ambiguous membership.
func coordinatorMembership(tx *Transaction, group string, ids []string) error {
	b, err := scan(tx.root)
	if err != nil {
		return err
	}
	if len(b.Problems) != 0 {
		return coordinatorError("board membership integrity")
	}
	actual := []string{}
	for id, entry := range b.Entries {
		text, e := readDocument(entry)
		if e != nil {
			return e
		}
		if TaskGroupFrom(text) == group {
			actual = append(actual, id)
		}
	}
	slices.Sort(actual)
	if !slices.Equal(actual, ids) {
		return coordinatorError("group membership changed; reconcile the explicit member set")
	}
	return nil
}

// ReconcileCoordinator uses the same path for snapshots and every later event.
// The verifier is the Git-aware consumer; it must be read-only and must not
// reenter board APIs while this transaction holds the declared locks.
func ReconcileCoordinator(ctx context.Context, root string, r CoordinatorReconcile, verify func(context.Context, CoordinatorGitFacts) error) (c CoordinatorCheckpoint, err error) {
	ids := make([]string, 0, len(r.Members))
	for id := range r.Members {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		return c, coordinatorError("members required")
	}
	err = WithTransactionContext(ctx, root, LockScope{Groups: []string{r.GroupID, reviewControlGroup, dispatchRegistry, taskStartGroup}, Tasks: ids, ExclusiveBoard: true}, func(tx *Transaction) error {
		var exists bool
		c, exists, err = readCheckpoint(tx, r.GroupID)
		if err != nil {
			return err
		}
		if !exists || c.Authority != r.Authority {
			return coordinatorError("stale coordinator authority")
		}
		if c.Revision != r.ExpectedRevision {
			if c.LastRequestHash != coordinatorHash(r) {
				return coordinatorError("checkpoint CAS")
			}
			// A lost response may retry the same request, but evidence is still
			// revalidated before returning its existing result.
		}
		if !slices.Equal(ids, checkpointIDs(c)) {
			return coordinatorError("unknown or missing member")
		}
		if err := coordinatorMembership(tx, r.GroupID, ids); err != nil {
			return err
		}
		members := map[string]CoordinatorMember{}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			m, e := reconcileCoordinatorMember(ctx, tx, id, c.Members[id], r.Members[id], verify)
			if e != nil {
				return e
			}
			members[id] = m
		}
		if reflect.DeepEqual(c.Members, members) {
			return nil
		}
		if c.Revision == ^uint64(0) {
			return coordinatorError("checkpoint revision overflow")
		}
		c.Revision++
		c.Members, c.LastRequestHash = members, coordinatorHash(r)
		return putCheckpoint(tx, &c)
	})
	return
}
