//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

// add_members is a joint review write: the coordinator of the group asks for the
// confirmation of the holder of every started member, the new one included, and
// the plan takes the member only after the last confirmation.
func TestAddMembersWaitsForTheHolderOfTheNewMember(t *testing.T) {
	r := newCoordinatedRunOf(t, 2, 1)
	first, second := r.ids[0], r.ids[1]
	var plan board.ReviewPlan
	if err := readRemoteResource(t.Context(), r.coordinate, "plans", "coordinated-plan", &plan); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, AddMembers: []string{second, second}, Author: "coordinator", Basis: "the second card started"}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "extension.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	extend := func() error {
		_, err := remoteEvidenceAction(t.Context(), r.coordinate, "/host-b/clone", "extend-plan", input, nil)
		return err
	}
	current := func() board.ReviewPlan {
		var got board.ReviewPlan
		if err := readRemoteResource(t.Context(), r.coordinate, "plans", plan.PlanID, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	err = extend()
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("want a wait for the holders: %v", err)
	}
	id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	if id == "" {
		t.Fatalf("wait omits the publication: %v", err)
	}
	if _, err := r.holder.ConfirmReviewPublication(t.Context(), id, first); err != nil {
		t.Fatal(err)
	}
	if err := extend(); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("the holder of the new member has not confirmed: %v", err)
	}
	if got := current(); got.Revision != plan.Revision || !slices.Equal(got.TaskIDs, plan.TaskIDs) {
		t.Fatalf("the plan changed before the new holder confirmed: %+v", got)
	}
	if _, err := r.holder.ConfirmReviewPublication(t.Context(), id, second); err != nil {
		t.Fatal(err)
	}
	if err := extend(); err != nil {
		t.Fatalf("the confirmed addition was refused: %v", err)
	}
	if got := current(); got.Revision != plan.Revision+1 || !slices.Equal(got.TaskIDs, []string{first, second}) {
		t.Fatalf("plan: %+v", got)
	}
}
