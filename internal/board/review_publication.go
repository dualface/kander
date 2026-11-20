package board

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/dualface/kander/internal/statemachine/reviewrun"
)

// PublishReviewRun preserves the standalone board's per-card recovery contract.
func PublishReviewRun(root string, runID string) (ReviewRun, map[string]error, error) {
	return publishReviewRun(root, runID, nil)
}
func publishReviewRun(root string, runID string, published func(string)) (ReviewRun, map[string]error, error) {
	run, err := ReadReviewRun(root, runID)
	if err != nil {
		return run, nil, err
	}
	if !reviewRunFinal(run, true) {
		return run, nil, reviewError("run not finalized")
	}
	failures := map[string]error{}
	for _, id := range run.TaskIDs {
		var current ReviewRun
		err = WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
			ok, e := readReviewJSON(tx, reviewRunName(runID), &current)
			if e != nil {
				return e
			}
			if !ok || !reflect.DeepEqual(current.ReviewInput, run.ReviewInput) {
				return reviewError("intent conflict")
			}
			if e = stageReviewRunCard(tx, &current, run, id); e != nil {
				return e
			}
			return tx.PutGroup(reviewControlGroup, reviewRunName(runID), reviewJSON(current))
		})
		if err != nil {
			failures[id] = err
		} else {
			run.Published[id] = true
			if slices.Contains(current.SkippedMembers, id) && !slices.Contains(run.SkippedMembers, id) {
				run.SkippedMembers = append(run.SkippedMembers, id)
			}
			if published != nil {
				published(id)
			}
		}
	}
	return run, failures, nil
}

// PublishReviewRunAtomic commits all member copies, indexes and the service
// receipt in one redo transaction. No member can observe a partial publication.
func PublishReviewRunAtomic(root, runID string, extensions ...TransactionExtension) (ReviewRun, error) {
	run, err := ReadReviewRun(root, runID)
	if err != nil {
		return run, err
	}
	err = WithTransaction(root, reviewScope(run.TaskIDs, false), func(tx *Transaction) error {
		var current ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(runID), &current)
		if err != nil {
			return err
		}
		if !ok || !reviewRunFinal(current, true) || !reflect.DeepEqual(current.ReviewInput, run.ReviewInput) {
			return reviewError("intent conflict")
		}
		for _, id := range run.TaskIDs {
			if err := stageReviewRunCard(tx, &current, run, id); err != nil {
				return err
			}
		}
		run = current
		tx.SetOperationResult(run)
		return tx.PutGroup(reviewControlGroup, reviewRunName(runID), reviewJSON(run))
	}, extensions...)
	return run, err
}

// stageReviewRunCard publishes run to member id inside tx and records it in
// current. The caller has checked that the run is finalized with the frozen
// input. The kernel decides what the member's state allows: a skipped member
// passes, a member that left the batch is skipped, a published member is
// verified, and any other member takes a copy once its checks pass. The checks of
// the card, the batch, and the originals stay hand-written and run in the order
// they always have.
func stageReviewRunCard(tx *Transaction, current *ReviewRun, run ReviewRun, id string) error {
	runID := run.RunID

	s, e := tx.Snapshot(id)
	if e != nil {
		return e
	}
	if TaskGroupFrom(s.Text) != run.TaskGroup {
		return reviewError("card group binding")
	}
	state := projectReviewRun(*current, true, id)
	if state.Skipped {
		if _, ok := stepReviewRun(state, reviewrun.PassSkipped{SameInput: true, GroupOk: true}); !ok {
			if state.Abandoned {
				return abandonedRunError(runID)
			}
			return reviewError("intent conflict")
		}
		return nil
	}
	if !state.Published {
		excluded, e := memberExcluded(tx, id)
		if e != nil {
			return e
		}
		if excluded {
			// The member left during the run: it takes no copy, and the
			// run still settles for the members that remain.
			next, ok := stepReviewRun(state, reviewrun.Skip{SameInput: true, GroupOk: true, Excluded: true})
			if !ok {
				return reviewError("intent conflict")
			}
			current.Published[id] = next.Published
			current.SkippedMembers = append(current.SkippedMembers, id)
			return nil
		}
		if _, e = reviewCards(tx, []string{id}, run.ReportLanguage, run.ReportLanguage); e != nil {
			return e
		}
	}
	var batch ReviewBatch
	ok, e := readReviewJSON(tx, reviewBatchName(run.BatchID), &batch)
	if e != nil {
		return e
	}
	if !ok || batch.Base != run.Base || batch.ReportLanguage != run.ReportLanguage {
		return reviewError("publication batch binding")
	}
	if e = requireRunMembers(tx, batch, run.TaskIDs); e != nil {
		return e
	}
	ancestors, e := reviewAncestors(tx, *current)
	if e != nil {
		return e
	}
	if e = checkRunStructure(tx, *current, ancestors); e != nil {
		return e
	}
	originals, e := reviewOriginals(tx, runID, true)
	if e != nil {
		return e
	}
	if e = verifyReviewHashes(originals, run.Hashes); e != nil {
		return e
	}
	sidecar, ok, e := tx.ReadGroup(reviewControlGroup, "runs/"+runID+"/sidecar.json")
	if e != nil {
		return e
	}
	if !ok {
		return reviewError("missing sidecar")
	}
	manifest := ReviewManifest{Schema: 1, Input: run.ReviewInput, SidecarHash: ReviewDigest(sidecar), Hashes: run.Hashes}
	prefix := "reviews/" + runID + "/"
	if state.Published {
		verified := verifyCardReview(tx, id, manifest, sidecar)
		if _, ok := stepReviewRun(state, reviewrun.Verify{SameInput: true, GroupOk: true, Integrity: true, Verified: verified == nil}); !ok {
			return firstError(verified, reviewError("intent conflict"))
		}
		return nil
	}
	next, ok := stepReviewRun(state, reviewrun.Stage{SameInput: true, GroupOk: true, MemberOk: true, Integrity: true})
	if !ok {
		return reviewError("intent conflict")
	}
	for name, data := range originals {
		if e = putImmutableReview(tx, id, prefix+name, data); e != nil {
			return e
		}
	}
	if e = putImmutableReview(tx, id, prefix+"sidecar.json", sidecar); e != nil {
		return e
	}
	if e = putImmutableReview(tx, id, prefix+"manifest.json", []byte(reviewJSON(manifest))); e != nil {
		return e
	}
	index := reviewIndex(run)
	indexes, e := ParseReviewIndexes(s.Text)
	if e != nil {
		return e
	}
	for _, existing := range indexes {
		if existing.RunID == runID {
			return reviewError("index exists without receipt")
		}
	}
	line, _ := json.Marshal(index)
	text, e := appendReviewIndex(s.Text, string(line))
	if e != nil {
		return e
	}
	if _, e = ParseReviewIndexes(text); e != nil {
		return e
	}
	if run.Resolved != "" {
		// The record shares the index transaction, so it adds no card revision of its own.
		if text, e = WithResolvedRecord(text, ReviewResolvedField(run.Role), run.Resolved); e != nil {
			return e
		}
	}
	if e = tx.Put(id, "spec.md", text); e != nil {
		return e
	}
	current.Published[id] = next.Published
	return nil
}
