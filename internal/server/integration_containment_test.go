package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// containmentCase builds a delivery request whose reviewed commit differs from
// the member delivery, as it does after a target-side merge.
type containmentCase struct {
	name   string
	build  func(r *api.IntegrationRequest, sha func(string) string)
	reject string
}

func edge(a, d string) api.IntegrationEdge { return api.IntegrationEdge{Ancestor: a, Descendant: d} }

func TestIntegrationContainmentKindsNeedExactEdges(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 40) }
	// T is the observed target a, C the reviewed commit c, D the delivery d,
	// V the carrier e, R the last replayed commit f.
	cases := []containmentCase{
		{name: "fast-forward with the delivery below the reviewed commit", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment = api.IntegrationFastForward
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("c")), edge(s("d"), s("c"))}
		}},
		{name: "legacy request without containment", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("c")), edge(s("d"), s("c"))}
		}},
		{name: "contained with the delivery below the reviewed commit", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationContained, true
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a")), edge(s("d"), s("c"))}
		}},
		{name: "contained with the delivery in the target", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationContained, true
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "contained with both member edges", reject: api.CodeEvidenceIncomplete, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationContained, true
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a")), edge(s("d"), s("a")), edge(s("d"), s("c"))}
		}},
		{name: "contained with no member edge", reject: api.CodeEvidenceIncomplete, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationContained, true
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a"))}
		}},
		{name: "via with the delivery in the carrier", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.ViaCommit = api.IntegrationVia, s("e")
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("e")), edge(s("c"), s("e")), edge(s("d"), s("e"))}
		}},
		{name: "via equal to the target", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.ViaCommit, r.AlreadyContained = api.IntegrationVia, s("a"), true
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("a")), edge(s("c"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "via equal to the target without the contained flag", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.ViaCommit = api.IntegrationVia, s("a")
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("a")), edge(s("c"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "via with a push marked as contained", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.ViaCommit, r.AlreadyContained = api.IntegrationVia, s("e"), true
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("e")), edge(s("c"), s("e")), edge(s("d"), s("e"))}
		}},
		{name: "via without a carrier", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment = api.IntegrationVia
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("e")), edge(s("d"), s("e"))}
		}},
		{name: "via missing the target edge", reject: api.CodeEvidenceIncomplete, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.ViaCommit = api.IntegrationVia, s("e")
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("e")), edge(s("d"), s("e"))}
		}},
		{name: "replayed with the delivery in the target", build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationReplayed, true
			r.Replay = &api.IntegrationReplay{ReviewBase: s("1"), ReviewTarget: s("c"), ReplayBase: s("2"), ReplayTarget: s("f")}
			r.Edges = []api.IntegrationEdge{edge(s("f"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "replayed for another reviewed commit", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationReplayed, true
			r.Replay = &api.IntegrationReplay{ReviewBase: s("1"), ReviewTarget: s("9"), ReplayBase: s("2"), ReplayTarget: s("f")}
			r.Edges = []api.IntegrationEdge{edge(s("f"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "replayed without its range", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationReplayed, true
			r.Edges = []api.IntegrationEdge{edge(s("f"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "replayed edge from the reviewed commit instead", reject: api.CodeEvidenceIncomplete, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained = api.IntegrationReplayed, true
			r.Replay = &api.IntegrationReplay{ReviewBase: s("1"), ReviewTarget: s("c"), ReplayBase: s("2"), ReplayTarget: s("f")}
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "fast-forward carrying a range", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment = api.IntegrationFastForward
			r.Replay = &api.IntegrationReplay{ReviewBase: s("1"), ReviewTarget: s("c"), ReplayBase: s("2"), ReplayTarget: s("f")}
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("c")), edge(s("d"), s("c"))}
		}},
		{name: "contained with a carrier", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment, r.AlreadyContained, r.ViaCommit = api.IntegrationContained, true, s("e")
			r.Edges = []api.IntegrationEdge{edge(s("c"), s("a")), edge(s("d"), s("a"))}
		}},
		{name: "unknown containment", reject: api.CodeInvalidRequest, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment = "guess"
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("c")), edge(s("d"), s("c"))}
		}},
		{name: "fast-forward whose delivery is not below the reviewed commit", reject: api.CodeEvidenceIncomplete, build: func(r *api.IntegrationRequest, s func(string) string) {
			r.Containment = api.IntegrationFastForward
			r.Edges = []api.IntegrationEdge{edge(s("a"), s("c"))}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			p := f.project("host-a")
			member, grant := integrationTaskAt(t, f, p, "host-a", "contain-kind", sha("d"), sha("c"))
			input := integrationInput(t, p, member, grant.Claim.ExecutionID, "contain-kind-"+strings.ReplaceAll(c.name, " ", "-"))
			input.Commit = sha("c")
			c.build(&input, sha)
			_, err := p.ClaimIntegration(t.Context(), "", input, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
			if c.reject == "" {
				if err != nil {
					t.Fatalf("exact request refused: %v", err)
				}
				return
			}
			expectCode(t, err, c.reject)
		})
	}
}

func TestIntegrationContainmentIsDeliveryOnly(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	member, grant := integrationTask(t, f, p, "host-a", "contain-purpose")
	input := integrationInput(t, p, member, grant.Claim.ExecutionID, "contain-purpose")
	input.Containment = api.IntegrationFastForward
	input.GroupID, input.Purpose = "20261117-contain-group", "assemble"
	input.TargetRef = "refs/heads/group/" + input.GroupID
	_, err := p.ClaimIntegration(t.Context(), "", input, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation})
	expectCode(t, err, api.CodeInvalidRequest)
}

func TestIntegrationConfirmUsesTheReplayedTail(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 40) }
	f := newFixture(t)
	p := f.project("host-a")
	member, taskGrant := integrationTaskAt(t, f, p, "host-a", "contain-replay-confirm", sha("d"), sha("c"))
	input := integrationInput(t, p, member, taskGrant.Claim.ExecutionID, "contain-replay-confirm")
	input.Commit = sha("c")
	input.Containment, input.AlreadyContained = api.IntegrationReplayed, true
	input.Replay = &api.IntegrationReplay{ReviewBase: sha("1"), ReviewTarget: sha("c"), ReplayBase: sha("2"), ReplayTarget: sha("f")}
	input.Edges = []api.IntegrationEdge{edge(sha("f"), sha("a")), edge(sha("d"), sha("a"))}
	grant, err := p.ClaimIntegration(t.Context(), "", input, client.Claim{Token: taskGrant.Token, Generation: taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Integration.Generation}
	observation, err := p.IntegrationObservation(t.Context(), grant.Integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	confirm := func(ancestor string) error {
		_, err := p.IntegrationAction(t.Context(), "", grant.Integration.ID, api.IntegrationCommand{Action: "confirm", ExpectedRevision: grant.Integration.Revision, ObservedTarget: sha("a"), ObservedAt: time.Now().UTC(), ObservationTicket: observation.Ticket, Edges: []api.IntegrationEdge{edge(ancestor, sha("a"))}}, proof)
		return err
	}
	expectCode(t, confirm(sha("c")), api.CodeEvidenceIncomplete)
	if err := confirm(sha("f")); err != nil {
		t.Fatalf("replayed confirmation: %v", err)
	}
	done, err := p.Integration(t.Context(), grant.Integration.ID)
	if err != nil || done.Phase != "integrated" || done.Request.Commit != sha("c") || done.Request.Replay == nil {
		t.Fatalf("receipt: %+v %v", done, err)
	}
}
