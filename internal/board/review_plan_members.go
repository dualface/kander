package board

import (
	"slices"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/statemachine/reviewplan"
)

// normalizedAddMembers is the sorted, duplicate-free task IDs of the add_members
// operation. A request that names the operation names at least one member.
func normalizedAddMembers(x ReviewPlanExtension) ([]string, error) {
	if x.AddMembers == nil {
		return nil, nil
	}
	if len(x.AddMembers) == 0 {
		return nil, reviewError(t("board.review_add_members_empty"))
	}
	return normalizedReviewTasks(x.AddMembers)
}

// ReviewPlanExtensionTasks lists the tasks one extend-plan request needs: the
// members of plan p and the members the request adds. The server takes its
// authority and lock set from it, and a client builds the joint confirmation set
// from it, so both see the same members.
func ReviewPlanExtensionTasks(p ReviewPlan, x ReviewPlanExtension) ([]string, error) {
	added, err := normalizedAddMembers(x)
	if err != nil || len(added) == 0 {
		return p.TaskIDs, err
	}
	return normalizedReviewTasks(slices.Concat(p.TaskIDs, added))
}

// planGroup is the task group every member of p shares; a plan whose members
// share none is no group plan and takes no new member.
func planGroup(tx *Transaction, p ReviewPlan) (string, error) {
	group := ""
	for i, id := range p.TaskIDs {
		s, err := tx.Snapshot(id)
		if err != nil {
			return "", err
		}
		if g := TaskGroupFrom(s.Text); i == 0 {
			group = g
		} else if g != group {
			return "", nil
		}
	}
	return group, nil
}

// memberAdditionFacts is the event of one new member for the plan kernel and, for
// each fact that failed, the text that names the member and the value. The
// kernel decides; the text only explains. The snapshot of a write transaction
// refuses a card that is not a directory card.
func memberAdditionFacts(tx *Transaction, p ReviewPlan, group, id string, cas bool) (ev reviewplan.AddMembers, parts []string, s Snapshot, err error) {
	if s, err = tx.Snapshot(id); err != nil {
		return
	}
	ev.Cas = cas
	if ev.Active = s.Entry.State == "working" || s.Entry.State == "review"; !ev.Active {
		parts = append(parts, t("board.review_add_member_state", id, s.Entry.State))
	}
	var pointer struct {
		PlanID string `json:"plan_id"`
	}
	pointed, err := readReviewJSON(tx, taskPlanName(id), &pointer)
	if err != nil {
		return
	}
	excluded, err := memberExcluded(tx, id)
	if err != nil {
		return
	}
	listed := containsID(p.TaskIDs, id)
	switch {
	case listed:
		parts = append(parts, t("board.review_add_member_listed", id, p.PlanID))
	case pointed:
		parts = append(parts, t("board.review_add_member_planned", id, shown(pointer.PlanID, ValidReviewID)))
	}
	if excluded {
		parts = append(parts, t("board.review_add_member_excluded", id))
	}
	ev.Free = !listed && !pointed && !excluded
	cardGroup := TaskGroupFrom(s.Text)
	switch ev.Group = group != "" && cardGroup == group; {
	case ev.Group:
	case group == "":
		parts = append(parts, t("board.review_add_member_no_group", id, p.PlanID))
	default:
		parts = append(parts, t("board.review_add_member_group", id, quoted(cardGroup), p.PlanID, group))
	}
	language := MetadataFrom(s.Text, FieldLanguage)
	if language == "" {
		language = p.ReportLanguage
	}
	if _, err = config.ValidateAgentLanguage(language); err != nil {
		return
	}
	if ev.Language = language == p.ReportLanguage; !ev.Language {
		parts = append(parts, t("board.review_add_member_language", id, language, p.PlanID, p.ReportLanguage))
	}
	// A resolution of an unplanned batch covers the members of the plan when it was
	// recorded, never a member who joins afterwards, so no resolution excuses a run.
	unresolved := p
	unresolved.ResolvedBatches = nil
	batch, _, err := unplannedReviewRun(tx, unresolved, []string{id}, true, false)
	if err != nil {
		return
	}
	if ev.NoUnplannedRuns = batch == ""; !ev.NoUnplannedRuns {
		parts = append(parts, t("board.review_add_member_unplanned_run", id, batch, p.PlanID))
	}
	return
}

// addPlanMembers adds the started members ids of the plan's group to the plan p
// that is not sealed. Each new member is one kernel event on the plan; the first
// refusal names the member and what does not hold, and nothing is written. An
// accepted member becomes a plan member with its current execution cycle and a
// plan pointer. The caller publishes the plan, the cycles, the copies, and the
// history with the revision bump, as for every extension. The obligations of a
// new member need no record: the member is in none of the batches of the plan, so
// its kernel starts with every batch not owed.
func addPlanMembers(tx *Transaction, pk *planKernel, p *ReviewPlan, x ReviewPlanExtension, ids []string) error {
	group, err := planGroup(tx, *p)
	if err != nil {
		return err
	}
	cas := p.Revision == x.ExpectedRevision
	cycles := make(map[string]string, len(ids))
	for _, id := range ids {
		ev, parts, s, e := memberAdditionFacts(tx, *p, group, id, cas)
		if e != nil {
			return e
		}
		if !pk.step(ev) {
			return reviewRefusal("plan member addition refused", t("board.review_add_members_next"), parts...)
		}
		cycles[id] = planCycle(s)
	}
	members, err := normalizedReviewTasks(slices.Concat(p.TaskIDs, ids))
	if err != nil {
		return err
	}
	p.TaskIDs = members
	for _, id := range ids {
		p.Cycles[id] = cycles[id]
		if err = tx.PutGroup(reviewControlGroup, taskPlanName(id), reviewJSON(map[string]string{"plan_id": p.PlanID})); err != nil {
			return err
		}
	}
	return nil
}
