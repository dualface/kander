package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
)

func refusalText(err error) string {
	var failure *api.Error
	if !errors.As(err, &failure) {
		return ""
	}
	return failure.Message
}

func TestReclaimRefusalNamesTheCurrentHolder(t *testing.T) {
	now := time.Now()
	live := &api.ClaimView{ExecutionID: "same", Status: api.ClaimActive, ExpiresAt: now.Add(time.Minute)}
	for name, holder := range map[string]*api.ClaimView{
		"active": live,
		"lapsed": {ExecutionID: "same", Status: api.ClaimLapsed, ExpiresAt: now.Add(-time.Second)},
	} {
		err := reclaimRefusal(holder, "same")
		if !api.IsCode(err, api.CodeLeaseInvalid) || refusalText(err) != config.Text("remote.execution_already_holder") {
			t.Fatalf("current holder, %s: %v", name, err)
		}
	}
	if config.Text("remote.execution_already_holder") == config.Text("remote.execution_cannot_reclaim") {
		t.Fatal("the holder message repeats the expired-claim message")
	}
	for name, claim := range map[string]*api.ClaimView{
		"unexpired but lost": {ExecutionID: "same", Status: api.ClaimLost, ExpiresAt: now.Add(time.Minute)},
		"released":           {ExecutionID: "same", Status: api.ClaimReleased, ExpiresAt: now.Add(time.Minute)},
	} {
		err := reclaimRefusal(claim, "same")
		if !api.IsCode(err, api.CodeLeaseInvalid) || refusalText(err) != config.Text("remote.execution_cannot_reclaim") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := reclaimRefusal(live, "another"); err != nil {
		t.Fatalf("another execution may take over: %v", err)
	}
	if err := reclaimRefusal(nil, "same"); err != nil {
		t.Fatalf("no claim: %v", err)
	}
}
