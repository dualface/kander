package remote_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// requestsFor are the review requests of the card that name the publication.
func (f *singleMember) requestsFor(id string) []api.CollabRequest {
	f.t.Helper()
	var named []api.CollabRequest
	for _, request := range f.requests() {
		if request.Kind == api.RequestReview && strings.Contains(request.Message, id) {
			named = append(named, request)
		}
	}
	return named
}

// A client of an earlier version asked its holder with the key and the text of that version. The
// request is still the question for the publication; the client of this version must find it and
// not send the same question again under another text.
const earlierRequestText = "Joint review material %s is ready. Inspect its operation and versions, then confirm this task from your own local project directory: kander review confirm <absolute-CWD> %s %s. Confirmation is limited to this task revision and execution generation."

func earlierRequestKey(id, task string) string {
	sum := sha256.Sum256([]byte(id + "\x00" + task))
	return "approval-" + hex.EncodeToString(sum[:])
}

func TestRetryAfterAnUpgradeAsksNoSecondTime(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	payload := map[string]string{"run_id": "single-run"}
	id, err := remote.PublicationID(t.Context(), f.coordinate, "review-assign", payload, f.ids)
	if err != nil {
		t.Fatal(err)
	}
	earlier := api.NewCollabRequest{Kind: api.RequestReview, TargetGeneration: f.grant.Claim.Generation, Message: fmt.Sprintf(earlierRequestText, id, id, f.ids[0])}
	if _, err := f.coordinate.Project.CreateRequest(t.Context(), earlierRequestKey(id, f.ids[0]), f.ids[0], earlier); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := f.coordinate.ReviewAuthority(t.Context(), "review-assign", payload, f.ids)
		if api.IsCode(err, api.CodeIdempotencyMismatch) {
			t.Fatalf("attempt %d: the retry met the request of the earlier version: %v", attempt, err)
		}
		if got := pendingPublicationID(t, err); got != id {
			t.Fatalf("attempt %d waits for %s, want %s", attempt, got, id)
		}
		if requests := f.requestsFor(id); len(requests) != 1 || requests[0].Message != earlier.Message {
			t.Fatalf("attempt %d: requests=%+v, want the one of the earlier version", attempt, requests)
		}
	}
}

// Without an earlier request the client asks once, whatever number of times and in whatever
// interface language the command is run again.
func TestRetryAsksTheHolderOnce(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	payload := map[string]string{"run_id": "single-run"}
	_, err := f.coordinate.ReviewAuthority(t.Context(), "review-assign", payload, f.ids)
	id := pendingPublicationID(t, err)
	if requests := f.requestsFor(id); len(requests) != 1 {
		t.Fatalf("first run left %d requests, want one", len(requests))
	}
	for _, language := range []string{"en", "en", "ja", "zh-CN"} {
		t.Setenv(config.EnvLang, language)
		_, err := f.coordinate.ReviewAuthority(t.Context(), "review-assign", payload, f.ids)
		if api.IsCode(err, api.CodeIdempotencyMismatch) {
			t.Fatalf("retry in %s: %v", language, err)
		}
		if got := pendingPublicationID(t, err); got != id {
			t.Fatalf("retry in %s waits for %s, want %s", language, got, id)
		}
		if requests := f.requestsFor(id); len(requests) != 1 {
			t.Fatalf("retry in %s left %d requests, want one", language, len(requests))
		}
	}
}

// A request the holder already answered still counts as the question for that publication.
func TestRetryAfterTheHolderAnsweredWithoutConfirmingAsksNoSecondTime(t *testing.T) {
	f := newSingleMember(t, "20261118-single-group", "20261118-single-group")
	payload := map[string]string{"run_id": "single-run"}
	_, err := f.coordinate.ReviewAuthority(t.Context(), "review-assign", payload, f.ids)
	id := pendingPublicationID(t, err)
	request := f.requestsFor(id)[0]
	if _, err := f.holder.Project.AnswerRequest(t.Context(), "answer-review", f.ids[0], request.ID, api.CollabAnswer{Status: api.RequestDone, Note: "read"}, client.Claim{Token: f.grant.Token, Generation: f.grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	_, err = f.coordinate.ReviewAuthority(t.Context(), "review-assign", payload, f.ids)
	pendingPublicationID(t, err)
	if requests := f.requestsFor(id); len(requests) != 1 {
		t.Fatalf("the retry left %d requests, want the answered one", len(requests))
	}
}
