package remote

import (
	"errors"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
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
	err := reclaimRefusal(live, "same", now)
	if !api.IsCode(err, api.CodeLeaseInvalid) || refusalText(err) != config.Text("remote.execution_already_holder") {
		t.Fatalf("current holder: %v", err)
	}
	if config.Text("remote.execution_already_holder") == config.Text("remote.execution_cannot_reclaim") {
		t.Fatal("the holder message repeats the expired-claim message")
	}
	for name, claim := range map[string]*api.ClaimView{
		"unexpired but lost":    {ExecutionID: "same", Status: api.ClaimLost, ExpiresAt: now.Add(time.Minute)},
		"active but expired":    {ExecutionID: "same", Status: api.ClaimActive, ExpiresAt: now.Add(-time.Second)},
		"released":              {ExecutionID: "same", Status: api.ClaimReleased, ExpiresAt: now.Add(time.Minute)},
		"active exactly at end": {ExecutionID: "same", Status: api.ClaimActive, ExpiresAt: now},
	} {
		err := reclaimRefusal(claim, "same", now)
		if !api.IsCode(err, api.CodeLeaseInvalid) || refusalText(err) != config.Text("remote.execution_cannot_reclaim") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := reclaimRefusal(live, "another", now); err != nil {
		t.Fatalf("another execution may take over: %v", err)
	}
	if err := reclaimRefusal(nil, "same", now); err != nil {
		t.Fatalf("no claim: %v", err)
	}
}
