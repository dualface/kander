package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// policyEvents reads the events after cursor without waiting.
func policyEvents(t *testing.T, p *client.Project, cursor string) ([]api.Event, string) {
	t.Helper()
	page, err := p.Events(t.Context(), cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Reset && cursor != "" {
		t.Fatalf("the event cursor %s was reset", cursor)
	}
	var out []api.Event
	for _, e := range page.Events {
		if e.Kind == api.EventProjectPolicyChanged {
			out = append(out, e)
		}
	}
	return out, page.Cursor
}

func TestPolicyWriteAppendsOneChangeEventWithTheNewRevision(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	_, cursor := policyEvents(t, p, "")
	if page, err := p.Events(t.Context(), cursor, 0); err != nil || page.PolicyEpoch != "" || page.PolicyRevision != 0 {
		t.Fatalf("an unset policy shows %q/%d on the event page: %v", page.PolicyEpoch, page.PolicyRevision, err)
	}
	first := putPolicy(t, p, requiredPMQA())
	second := putPolicy(t, p, enforcedPolicy{mode: "main-only", smallPMQA: "auto", largePM: "auto", review: true})
	events, _ := policyEvents(t, p, cursor)
	if len(events) != 2 || events[0].Revision != first || events[1].Revision != second {
		t.Fatalf("policy events %+v, want revisions %d and %d", events, first, second)
	}
	for _, e := range events {
		if e.TaskID != "" || e.GroupID != "" || e.State != "" {
			t.Fatalf("a policy event names a resource: %+v", e)
		}
	}
	// Every page carries the policy version it was answered with, also an empty one.
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.Events(t.Context(), cursor, 0)
	if err != nil || page.PolicyEpoch != current.Epoch || page.PolicyRevision != second {
		t.Fatalf("event page policy %q/%d %v, want %q/%d", page.PolicyEpoch, page.PolicyRevision, err, current.Epoch, second)
	}
}

func TestPolicyWriteThatFailsOrRepeatsAppendsNoEvent(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	revision := putPolicy(t, p, requiredPMQA())
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, cursor := policyEvents(t, p, "")
	// A stale expected revision and values the validation refuses write nothing.
	if _, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: revision - 1, Values: current.Values}); !api.IsCode(err, api.CodePolicyRevisionConflict) {
		t.Fatalf("stale write: %v", err)
	}
	bad := json.RawMessage(strings.Replace(string(current.Values), `"main-develop"`, `"trunk"`, 1))
	if _, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: revision, Values: bad}); !api.IsCode(err, api.CodeInvalidRequest) {
		t.Fatalf("invalid write: %v", err)
	}
	if events, _ := policyEvents(t, p, cursor); len(events) != 0 {
		t.Fatalf("refused writes appended %+v", events)
	}
	// A retry with the key of a completed write replays its answer and appends nothing.
	write := api.PolicyWrite{ExpectedRevision: revision, Values: current.Values}
	written, err := p.PutPolicy(t.Context(), "policy-retry-key", write)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.PutPolicy(t.Context(), "policy-retry-key", write)
	if err != nil || again.Revision != written.Revision {
		t.Fatalf("retry answered %+v %v, want revision %d", again, err, written.Revision)
	}
	events, _ := policyEvents(t, p, cursor)
	if len(events) != 1 || events[0].Revision != written.Revision {
		t.Fatalf("one write and its retry appended %+v", events)
	}
}

func TestLeaseRenewalsCarryTheCurrentPolicyVersion(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	member, taskGrant := integrationTask(t, f, p, "host-a", "renew-policy")
	taskProof := client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation}
	integration, err := p.ClaimIntegration(t.Context(), "", integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "renew-policy-target"), taskProof)
	if err != nil {
		t.Fatal(err)
	}
	integrationProof := client.Claim{Token: integration.Token, Generation: integration.Integration.Generation}
	request := groupRequest(t, p)
	group, err := p.ClaimGroup(t.Context(), "", request.Request.GroupID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	groupProof := client.Claim{Token: group.Token, Generation: group.Claim.Generation}
	renewals := map[string]func() (api.Renewal, error){
		"task":  func() (api.Renewal, error) { return p.Renew(t.Context(), member.TaskID, taskProof) },
		"group": func() (api.Renewal, error) { return p.RenewGroup(t.Context(), request.Request.GroupID, groupProof) },
		"integration": func() (api.Renewal, error) {
			return p.RenewIntegration(t.Context(), integration.Integration.ID, integrationProof)
		},
	}
	check := func(epoch string, revision uint64) {
		t.Helper()
		f.clock.advance(16 * time.Second)
		for kind, renew := range renewals {
			renewal, err := renew()
			if err != nil {
				t.Fatalf("%s renewal: %v", kind, err)
			}
			if renewal.PolicyEpoch != epoch || renewal.PolicyRevision != revision {
				t.Fatalf("%s renewal carries policy %q/%d, want %q/%d", kind, renewal.PolicyEpoch, renewal.PolicyRevision, epoch, revision)
			}
		}
	}
	check("", 0)
	putPolicy(t, p, requiredPMQA())
	putPolicy(t, p, requiredPMQA())
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	check(current.Epoch, 2)
}
