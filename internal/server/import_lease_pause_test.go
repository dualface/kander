package server_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// importHeld is longer than the 90 second TTL of the fixture.
const importHeld = 90*time.Second + time.Minute

// importSource builds a v1 board with one backlog card per slug.
func importSource(t *testing.T, slugs ...string) (root string, ids []string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "kanban")
	if err := board.InitStorage(root); err != nil {
		t.Fatal(err)
	}
	for _, slug := range slugs {
		path, err := board.NewTask(root, "feature", slug, slug, "en", false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, filepath.Base(path))
	}
	return root, ids
}

// importHolding imports source into the demo project and advances the clock
// by held while the import holds the project lock: the scan phase is reported
// under that lock.
func importHolding(t *testing.T, f *fixture, source string, held time.Duration) (api.ImportResult, error) {
	t.Helper()
	var once sync.Once
	return f.srv.ImportWithProgress(t.Context(), "demo", "", source, func(p api.ImportProgress) {
		if p.Phase == api.ImportPhaseScan {
			once.Do(func() { f.clock.advance(held) })
		}
	})
}

// heldLeases is one active lease of each kind in one project: the task claim
// of an integration member, a coordinator lease, and an integration target
// lease.
type heldLeases struct {
	r           *integrationRig
	groupInput  api.GroupClaim
	group       api.GroupGrant
	integration api.IntegrationGrant
}

func newHeldLeases(t *testing.T) *heldLeases {
	t.Helper()
	l := &heldLeases{r: newIntegrationRig(t, "paused-member")}
	l.groupInput = groupRequest(t, l.r.a)
	var err error
	if l.group, err = l.r.a.ClaimGroup(t.Context(), "", l.groupInput.Request.GroupID, l.groupInput, nil); err != nil {
		t.Fatal(err)
	}
	l.r.claim("paused-integration", l.r.input("paused-integration"), &l.integration)
	return l
}

func (l *heldLeases) taskClaim(t *testing.T) api.ClaimView {
	t.Helper()
	task, err := l.r.a.Show(t.Context(), l.r.member.TaskID)
	if err != nil || task.Claim == nil {
		t.Fatalf("task claim: %+v %v", task.Claim, err)
	}
	return *task.Claim
}

func (l *heldLeases) groupClaim(t *testing.T) api.ClaimView {
	t.Helper()
	view, err := l.r.a.Group(t.Context(), l.groupInput.Request.GroupID)
	if err != nil || view.Claim == nil {
		t.Fatalf("group lease: %+v %v", view.Claim, err)
	}
	return *view.Claim
}

func (l *heldLeases) integrationLease(t *testing.T) api.Integration {
	t.Helper()
	observation, err := l.r.a.IntegrationObservation(t.Context(), l.integration.Integration.ID)
	if err != nil || observation.Integration == nil {
		t.Fatalf("integration lease: %+v %v", observation.Integration, err)
	}
	return *observation.Integration
}

// assertPaused checks that every lease is still active with its expiry moved
// by held, that each holder renews, and that rivals are refused because the
// leases are active.
func (l *heldLeases) assertPaused(t *testing.T, held time.Duration) {
	t.Helper()
	ctx := t.Context()
	claim := l.taskClaim(t)
	if want := l.r.task.Claim.ExpiresAt.Add(held); claim.Status != api.ClaimActive || !claim.ExpiresAt.Equal(want) {
		t.Fatalf("task claim after import: %s until %v, want active until %v", claim.Status, claim.ExpiresAt, want)
	}
	group := l.groupClaim(t)
	if want := l.group.Claim.ExpiresAt.Add(held); group.Status != api.ClaimActive || !group.ExpiresAt.Equal(want) {
		t.Fatalf("group lease after import: %s until %v, want active until %v", group.Status, group.ExpiresAt, want)
	}
	if got, want := l.integrationLease(t).ExpiresAt, l.integration.Integration.ExpiresAt.Add(held); !got.Equal(want) {
		t.Fatalf("integration lease after import expires %v, want %v", got, want)
	}

	_, err := l.r.b.Takeover(ctx, "", l.r.member.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "rival", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: claim.Generation})
	expectCode(t, err, api.CodeLeaseActive)
	rival := l.groupInput
	rival.ExecutionID, rival.ExpectedGeneration, rival.Decision = "rival-coordinator", group.Generation, "user:takeover"
	_, err = l.r.b.ClaimGroup(ctx, "", rival.Request.GroupID, rival, nil)
	expectCode(t, err, api.CodeLeaseActive)

	if _, err := l.r.a.Renew(ctx, l.r.member.TaskID, l.r.owner); err != nil {
		t.Fatalf("task renewal after import: %v", err)
	}
	if _, err := l.r.a.RenewGroup(ctx, l.groupInput.Request.GroupID, client.Claim{Token: l.group.Token, Generation: l.group.Claim.Generation}); err != nil {
		t.Fatalf("group renewal after import: %v", err)
	}
	if _, err := l.r.a.RenewIntegration(ctx, l.integration.Integration.ID, client.Claim{Token: l.integration.Token, Generation: l.integration.Integration.Generation}); err != nil {
		t.Fatalf("integration renewal after import: %v", err)
	}
}

// An import that holds the project lock longer than the TTL does not let the
// leases that were active when it took the lock lapse.
func TestImportPausesActiveLeases(t *testing.T) {
	l := newHeldLeases(t)
	source, _ := importSource(t, "paused-incoming")
	if result, err := importHolding(t, l.r.f, source, importHeld); err != nil || result.AddedTasks != 1 {
		t.Fatalf("import: %+v %v", result, err)
	}
	l.assertPaused(t, importHeld)
}

// A refused import and a repeated snapshot held the lock as well.
func TestImportPausesLeasesWhenItPublishesNothing(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		l := newHeldLeases(t)
		source, ids := importSource(t, "conflicting")
		if _, err := l.r.f.srv.Import("demo", "", source); err != nil {
			t.Fatal(err)
		}
		spec := filepath.Join(source, "backlog", ids[0], "spec.md")
		text, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(spec, append(text, "\nsource-only edit\n"...), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = importHolding(t, l.r.f, source, importHeld)
		expectCode(t, err, api.CodeBoardRejected)
		l.assertPaused(t, importHeld)
	})
	t.Run("existing", func(t *testing.T) {
		l := newHeldLeases(t)
		source, _ := importSource(t, "repeated")
		if _, err := l.r.f.srv.Import("demo", "", source); err != nil {
			t.Fatal(err)
		}
		result, err := importHolding(t, l.r.f, source, importHeld)
		if err != nil || !result.Existing {
			t.Fatalf("repeated import: %+v %v", result, err)
		}
		l.assertPaused(t, importHeld)
	})
}

// A lease that lapsed before the import took the lock stays lapsed, and a
// takeover succeeds.
func TestImportKeepsLapsedLeasesLapsed(t *testing.T) {
	f := newFixture(t)
	a, b := f.project("host-a"), f.project("host-b")
	task := todoTask(t, a, "lapsed-before-import")
	grant, err := claim(t, a, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(91 * time.Second)
	source, _ := importSource(t, "after-lapse")
	if _, err := importHolding(t, f, source, importHeld); err != nil {
		t.Fatal(err)
	}
	shown, err := a.Show(t.Context(), task.TaskID)
	if err != nil || shown.Claim == nil || shown.Claim.Status != api.ClaimLapsed || !shown.Claim.ExpiresAt.Equal(grant.Claim.ExpiresAt) {
		t.Fatalf("lapsed claim after import: %+v %v", shown.Claim, err)
	}
	if _, err := b.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "host-b", ExecutionID: "successor", Owner: "codex", Decision: "user:takeover", ExpectedGeneration: grant.Claim.Generation}); err != nil {
		t.Fatalf("takeover of a lease that lapsed before the import: %v", err)
	}
}
