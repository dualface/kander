package server_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// The service refuses the identity of an ended coordinator execution with one
// code and a diagnostic that names why: another client sent it, its lease was
// released, or its lease was lost. Only the text differs from before; the kernel
// call and the code do not.
func TestRefusedSameExecutionClaimNamesTheRecordState(t *testing.T) {
	refusal := func(t *testing.T, end func(f *fixture, a *client.Project, group string, grant api.GroupGrant), claimant func(f *fixture, a *client.Project) *client.Project) string {
		t.Helper()
		f := newFixture(t)
		a := f.project("host-a")
		request := groupRequest(t, a)
		group := request.Request.GroupID
		grant, err := a.ClaimGroup(t.Context(), "", group, request, nil)
		if err != nil {
			t.Fatal(err)
		}
		end(f, a, group, grant)
		again := request
		again.ExpectedGeneration = grant.Claim.Generation
		_, err = claimant(f, a).ClaimGroup(t.Context(), "", group, again, nil)
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Code != api.CodeLeaseInvalid {
			t.Fatalf("the claim of an ended execution: %v", err)
		}
		return failure.Message
	}
	same := func(_ *fixture, a *client.Project) *client.Project { return a }
	proof := func(grant api.GroupGrant) client.Claim {
		return client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	}
	released := refusal(t, func(_ *fixture, a *client.Project, group string, grant api.GroupGrant) {
		if _, err := a.ReleaseGroup(t.Context(), "", group, api.GroupRelease{GroupID: group, Reason: "done", Decision: "user:release"}, proof(grant)); err != nil {
			t.Fatal(err)
		}
	}, same)
	lost := refusal(t, func(_ *fixture, a *client.Project, group string, grant api.GroupGrant) {
		if _, err := a.ObserveGroup(t.Context(), "", group, api.AgentObservation{Status: "stopped"}, proof(grant)); err != nil {
			t.Fatal(err)
		}
	}, same)
	foreign := refusal(t, func(*fixture, *client.Project, string, api.GroupGrant) {}, func(f *fixture, _ *client.Project) *client.Project { return f.project("host-b") })
	for state, text := range map[string]string{"released": released, "lost": lost, "another client": foreign} {
		if !strings.Contains(text, "first-coordinator") {
			t.Errorf("%s: the diagnostic does not name the execution: %q", state, text)
		}
	}
	if !strings.Contains(released, "released its lease at generation 1") || !strings.Contains(lost, "lost its lease at generation 1") || !strings.Contains(foreign, "belongs to another client") {
		t.Fatalf("diagnostics: %q / %q / %q", released, lost, foreign)
	}
	if released == lost || lost == foreign || released == foreign {
		t.Fatalf("two states share a diagnostic: %q / %q / %q", released, lost, foreign)
	}
	if strings.Contains(released, "lost coordinator execution") {
		t.Fatalf("a released lease is called lost: %q", released)
	}
}
