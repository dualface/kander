package remote_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

const (
	firstCoordinator  = "coordinator-first"
	secondCoordinator = "coordinator-second"
)

// guardFixture is one group with two unstarted members on a server with a
// fake clock, and two client homes: host a runs the first coordinator
// execution, host b the second.
type guardFixture struct {
	t       *testing.T
	a, b    *remote.Session
	log     *writeLog
	clock   *atomic.Int64
	group   string
	members []string
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixNano())
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), TTL: 90 * time.Second, RenewInterval: 15 * time.Second, Now: func() time.Time { return time.Unix(0, clock.Load()) }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	log := &writeLog{}
	httpServer := httptest.NewServer(log.wrap(srv.Handler()))
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	first, _ := client.New(httpServer.URL, "host-a")
	second, _ := client.New(httpServer.URL, "host-b")
	info, err := first.Register(t.Context(), "coordinator-guard", "")
	if err != nil {
		t.Fatal(err)
	}
	open := func(c *client.Client, name string) *remote.Session {
		s := &remote.Session{Client: c, Project: c.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: filepath.Join(t.TempDir(), name)}
		if err := os.MkdirAll(s.Home, 0o700); err != nil {
			t.Fatal(err)
		}
		return s
	}
	f := &guardFixture{t: t, a: open(first, "a"), b: open(second, "b"), log: log, clock: clock, group: "20261118-guard-group"}
	for _, slug := range []string{"guard-one", "guard-two"} {
		task, err := f.a.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Guard a group", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+f.group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		if task, err = f.a.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if task, err = f.a.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		f.members = append(f.members, task.TaskID)
	}
	return f
}

func (f *guardFixture) request(basis string, checkpoint board.CoordinatorCheckpoint) board.CoordinatorClaim {
	return board.CoordinatorClaim{GroupID: f.group, ExpectedRevision: checkpoint.Revision, ExpectedEpoch: checkpoint.Authority.Epoch, Owner: "coordinator", Token: "label", Basis: basis, Members: f.members}
}

// acquire binds the process to a coordinator execution, as a coordinator
// agent's environment does, and claims the group.
func (f *guardFixture) acquire(s *remote.Session, execution string, request board.CoordinatorClaim) (api.GroupGrant, error) {
	f.t.Helper()
	f.t.Setenv(remote.EnvExecutionID, execution)
	f.t.Setenv(remote.EnvCoordinatorExecutionID, execution)
	return s.AcquireCoordinator(f.t.Context(), request, request.Basis)
}

// claimRequests counts the claim requests of the group since the last call.
func (f *guardFixture) claimRequests() int {
	n := 0
	for _, record := range f.log.take() {
		if strings.HasSuffix(record.Path, "/groups/"+f.group+"/claim") {
			n++
		}
	}
	return n
}

func (f *guardFixture) first() api.GroupGrant {
	f.t.Helper()
	grant, err := f.acquire(f.a, firstCoordinator, f.request("first claim", board.CoordinatorCheckpoint{}))
	if err != nil {
		f.t.Fatal(err)
	}
	f.log.take()
	return grant
}

// refused runs a claim that must end before any request is sent and returns the
// text of the lease_invalid refusal.
func (f *guardFixture) refused(s *remote.Session, execution string, request board.CoordinatorClaim) string {
	f.t.Helper()
	_, err := f.acquire(s, execution, request)
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != api.CodeLeaseInvalid {
		f.t.Fatalf("claim of %s was not refused with lease_invalid: %v", execution, err)
	}
	if n := f.claimRequests(); n != 0 {
		f.t.Fatalf("the refused claim sent %d requests", n)
	}
	return refusal.Message
}

func (f *guardFixture) release(s *remote.Session) {
	f.t.Helper()
	proof, err := s.CoordinatorAuthority(f.group, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err = s.Project.ReleaseGroup(f.t.Context(), client.NewKey(), f.group, api.GroupRelease{GroupID: f.group, Reason: "group done", Decision: "decision-1"}, proof); err != nil {
		f.t.Fatal(err)
	}
}

// takeOver lets the second execution take the group once the lease of the
// first one lapsed.
func (f *guardFixture) takeOver(grant api.GroupGrant) api.GroupGrant {
	f.t.Helper()
	f.clock.Add(int64(3 * time.Minute))
	taken, err := f.acquire(f.b, secondCoordinator, f.request("decision-handover", grant.Checkpoint))
	if err != nil {
		f.t.Fatal(err)
	}
	f.log.take()
	return taken
}

func releasedText(f *guardFixture, execution string) string {
	return config.Text("remote.coordinator_execution_released", execution, f.group)
}

func TestAReleasedExecutionCannotClaimAgain(t *testing.T) {
	f := newGuardFixture(t)
	grant := f.first()
	f.release(f.a)
	f.log.take()
	want := releasedText(f, firstCoordinator)
	// The worker has not removed the credential yet: the service record says so.
	if got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint)); got != want {
		t.Fatalf("service record released: %q", got)
	}
	// After the worker removed the credential only the tombstone is left.
	credential, found, err := f.a.CoordinatorCredential(f.group)
	if err != nil || !found {
		t.Fatalf("%v %v", found, err)
	}
	if err = remote.RemoveCredential(f.a.Home, credential); err != nil {
		t.Fatal(err)
	}
	if got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint)); got != want {
		t.Fatalf("tombstone: %q", got)
	}
	// Another execution took the group over: the tombstone still decides.
	f.takeOver(grant)
	if got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint)); got != want {
		t.Fatalf("tombstone and a record of another execution: %q", got)
	}
	if strings.Contains(want, "lost coordinator execution") {
		t.Fatalf("released text speaks of a lost execution: %q", want)
	}
}

func TestALostExecutionCannotClaimAgain(t *testing.T) {
	t.Run("service record with a reason", func(t *testing.T) {
		f := newGuardFixture(t)
		grant := f.first()
		proof, err := f.a.CoordinatorAuthority(f.group, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.a.Project.ObserveGroup(t.Context(), client.NewKey(), f.group, api.AgentObservation{Status: "stopped", Reason: "agent exited\nwith code 3"}, proof); err != nil {
			t.Fatal(err)
		}
		f.log.take()
		got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
		if want := config.Text("remote.coordinator_execution_lost_reason", firstCoordinator, f.group, grant.Claim.Generation, "agent exited with code 3"); got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
	t.Run("service record without a reason", func(t *testing.T) {
		f := newGuardFixture(t)
		grant := f.first()
		proof, _ := f.a.CoordinatorAuthority(f.group, nil)
		if _, err := f.a.Project.ObserveGroup(t.Context(), client.NewKey(), f.group, api.AgentObservation{Status: "stopped"}, proof); err != nil {
			t.Fatal(err)
		}
		f.log.take()
		got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
		if want := config.Text("remote.coordinator_execution_lost", firstCoordinator, f.group, grant.Claim.Generation); got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
	t.Run("local credential marked lost", func(t *testing.T) {
		f := newGuardFixture(t)
		grant := f.first()
		credential, _, err := f.a.CoordinatorCredential(f.group)
		if err != nil {
			t.Fatal(err)
		}
		credential.Lost, credential.LostReason = true, "the renewal was refused"
		if err = remote.WriteCredential(f.a.Home, credential); err != nil {
			t.Fatal(err)
		}
		got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
		if want := config.Text("remote.coordinator_execution_lost_reason", firstCoordinator, f.group, grant.Claim.Generation, "the renewal was refused"); got != want {
			t.Fatalf("%q, want %q", got, want)
		}
	})
	t.Run("long reason is cut", func(t *testing.T) {
		f := newGuardFixture(t)
		grant := f.first()
		proof, _ := f.a.CoordinatorAuthority(f.group, nil)
		if _, err := f.a.Project.ObserveGroup(t.Context(), client.NewKey(), f.group, api.AgentObservation{Status: "stopped", Reason: strings.Repeat("x", 1000)}, proof); err != nil {
			t.Fatal(err)
		}
		got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
		if strings.Contains(got, strings.Repeat("x", 201)) || !strings.Contains(got, strings.Repeat("x", 200)+"...") {
			t.Fatalf("the reason was not cut to 200 characters: %q", got)
		}
	})
}

func TestATakenOverExecutionCannotClaimAgain(t *testing.T) {
	f := newGuardFixture(t)
	grant := f.first()
	taken := f.takeOver(grant)
	if taken.Claim.Generation != grant.Claim.Generation+1 {
		t.Fatalf("takeover generation %d", taken.Claim.Generation)
	}
	got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
	if want := config.Text("remote.coordinator_execution_taken_over", firstCoordinator, f.group, grant.Claim.Generation, taken.Claim.Generation); got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if !strings.Contains(got, strconv.FormatUint(taken.Claim.Generation, 10)) {
		t.Fatalf("the refusal does not name the new generation: %q", got)
	}
}

func TestTheThreeEndingsReadDifferently(t *testing.T) {
	texts := map[string]string{
		"released":   config.Text("remote.coordinator_execution_released", "e", "g"),
		"lost":       config.Text("remote.coordinator_execution_lost", "e", "g", 1),
		"lostReason": config.Text("remote.coordinator_execution_lost_reason", "e", "g", 1, "r"),
		"takenOver":  config.Text("remote.coordinator_execution_taken_over", "e", "g", 1, 2),
	}
	seen := map[string]string{}
	for name, text := range texts {
		if other, dup := seen[text]; dup {
			t.Fatalf("%s and %s read the same: %q", name, other, text)
		}
		seen[text] = name
	}
}

// A coordinator that is not bound to an execution takes a fresh execution ID
// whenever it cannot reuse its lease, so the refusals do not apply to it.
func TestAnUnboundCoordinatorStillClaimsWithAFreshExecution(t *testing.T) {
	f := newGuardFixture(t)
	grant := f.first()
	f.release(f.a)
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	taken, err := f.b.AcquireCoordinator(t.Context(), f.request("decision-unbound", grant.Checkpoint), "decision-unbound")
	if err != nil {
		t.Fatal(err)
	}
	if taken.Claim.Generation != grant.Claim.Generation+1 || taken.Claim.ExecutionID == firstCoordinator {
		t.Fatalf("%+v", taken.Claim)
	}
}

func TestAHolderClaimsAgainOnlyByReplay(t *testing.T) {
	f := newGuardFixture(t)
	request := f.request("first claim", board.CoordinatorCheckpoint{})
	grant, err := f.acquire(f.a, firstCoordinator, request)
	if err != nil {
		t.Fatal(err)
	}
	// The same request replays the stored checkpoint.
	replay, err := f.acquire(f.a, firstCoordinator, request)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Checkpoint.Revision != grant.Checkpoint.Revision || replay.Checkpoint.Authority.Epoch != grant.Checkpoint.Authority.Epoch {
		t.Fatalf("replay changed the checkpoint: %+v %+v", replay.Checkpoint, grant.Checkpoint)
	}
	// A changed request is refused, and the refusal says the claim is not needed.
	changed := request
	changed.Basis = "a different basis"
	_, err = f.acquire(f.a, firstCoordinator, changed)
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != api.CodeBoardRejected || refusal.BoardCode != "board.coordinator_conflict" {
		t.Fatalf("a changed claim: %v", err)
	}
	for _, part := range []string{firstCoordinator, f.group, "generation " + strconv.FormatUint(grant.Claim.Generation, 10), "needs no claim", "kander coordinator show " + f.group, "release"} {
		if !strings.Contains(refusal.Message, part) {
			t.Fatalf("the refusal lacks %q: %q", part, refusal.Message)
		}
	}
	if !strings.Contains(refusal.Message, "claim CAS or reused token") {
		t.Fatalf("the refusal dropped the reason of the service: %q", refusal.Message)
	}
}

// Every new text exists in all three languages with its arguments.
func TestGuardTextsInEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		for _, tc := range []struct {
			id    string
			args  []any
			parts []string
		}{
			{"remote.coordinator_release_needs_end_execution", nil, []string{"dismiss"}},
			{"remote.coordinator_release_final", []any{"exec-1", "group-1", 4}, []string{"exec-1", "group-1", "4", "dismiss"}},
			{"remote.coordinator_execution_released", []any{"exec-1", "group-1"}, []string{"exec-1", "group-1", "kander coordinator show group-1"}},
			{"remote.coordinator_execution_lost", []any{"exec-1", "group-1", 4}, []string{"exec-1", "group-1", "4", "kander coordinator show group-1"}},
			{"remote.coordinator_execution_lost_reason", []any{"exec-1", "group-1", 4, "why"}, []string{"exec-1", "group-1", "4", "why", "kander coordinator show group-1"}},
			{"remote.coordinator_execution_taken_over", []any{"exec-1", "group-1", 4, 5}, []string{"exec-1", "group-1", "4", "5", "kander coordinator show group-1"}},
			{"remote.coordinator_claim_already_held", []any{"exec-1", "group-1", 4, "why"}, []string{"exec-1", "group-1", "4", "why", "kander coordinator show group-1", "release"}},
		} {
			text := i18n.Text(lang, tc.id, tc.args...)
			if text == tc.id {
				t.Fatalf("%s/%s has no text", lang, tc.id)
			}
			for _, part := range tc.parts {
				if !strings.Contains(text, part) {
					t.Errorf("%s/%s lacks %q: %s", lang, tc.id, part, text)
				}
			}
		}
	}
}

// The text says which generation took the group over, never that it still runs
// it: the successor may have released the group since, and the replaced
// execution is refused all the same.
func TestATakenOverExecutionIsRefusedAfterTheSuccessorReleased(t *testing.T) {
	f := newGuardFixture(t)
	grant := f.first()
	taken := f.takeOver(grant)
	f.release(f.b)
	f.log.take()
	got := f.refused(f.a, firstCoordinator, f.request("again", grant.Checkpoint))
	if want := config.Text("remote.coordinator_execution_taken_over", firstCoordinator, f.group, grant.Claim.Generation, taken.Claim.Generation); got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	for _, claim := range []string{"is running", "still holds", "now holds"} {
		if strings.Contains(got, claim) {
			t.Fatalf("the refusal speaks of a running successor: %q", got)
		}
	}
}
