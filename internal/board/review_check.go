package board

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// CheckReviewEvidence checks both indexes and intents, so a run that failed
// before the first card publication cannot disappear from check's view.
func CheckReviewEvidence(root string, ids []string) (problems []Problem, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = WithTransaction(root, reviewScope(ids, true), func(tx *Transaction) error {
		runs := map[string]ReviewRun{}
		entries, e := fs.ListDirectory(root, control(root, "groups", reviewControlGroup, "runs"))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		for _, entry := range entries {
			if !ValidReviewID(entry.Name) || entry.Kind != fs.KindDirectory {
				return reviewError("invalid run entry: " + entry.Name)
			}
			var run ReviewRun
			ok, e := readReviewJSON(tx, reviewRunName(entry.Name), &run)
			if e != nil {
				return e
			}
			if !ok || run.Schema != 1 || run.RunID != entry.Name {
				return reviewError("invalid intent: " + entry.Name)
			}
			runs[entry.Name] = run
		}
		add := func(id string, e error) { problems = append(problems, Problem{Path: id, Message: e.Error()}) }
		for _, id := range ids {
			s, e := tx.Snapshot(id)
			if e != nil {
				return e
			}
			indexes, e := ParseReviewIndexes(s.Text)
			if e != nil {
				add(id, e)
				continue
			}
			indexed := map[string]bool{}
			for _, index := range indexes {
				indexed[index.RunID] = true
				if run, ok := runs[index.RunID]; !ok || !containsID(run.TaskIDs, id) {
					add(id, reviewError(index.RunID+": missing intent or membership"))
				}
			}
			var archiveEntries []fs.DirEntry
			if s.Entry.IsDirectory() {
				archiveEntries, e = fs.ListDirectory(root, filepath.Join(s.Entry.Path, "reviews"))
			}
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				add(id, e)
			}
			for _, entry := range archiveEntries {
				if entry.Kind != fs.KindDirectory || !indexed[entry.Name] {
					add(id, reviewError(entry.Name+": unindexed archive"))
				}
			}
			for runID, run := range runs {
				if !containsID(run.TaskIDs, id) {
					continue
				}
				if run.Phase != "finalized" || !allPublished(run) {
					add(id, reviewError(runID+": incomplete publication"))
					continue
				}
				e = checkRunStructure(tx, run, runs)
				if e != nil {
					add(id, e)
					continue
				}
				if TaskGroupFrom(s.Text) != run.TaskGroup {
					add(id, reviewError(runID+": card group mismatch"))
					continue
				}
				language := MetadataFrom(s.Text, FieldLanguage)
				if language != "" && language != run.ReportLanguage {
					add(id, reviewError(runID+": card language mismatch"))
					continue
				}
				sidecar, ok, e := tx.ReadGroup(reviewControlGroup, "runs/"+runID+"/sidecar.json")
				if e != nil {
					add(id, e)
					continue
				}
				if !ok {
					add(id, reviewError(runID+": missing sidecar"))
					continue
				}
				manifest := ReviewManifest{Schema: 1, Input: run.ReviewInput, SidecarHash: ReviewDigest(sidecar), Hashes: run.Hashes}
				if e = verifyCardReview(tx, id, manifest, sidecar); e != nil {
					add(id, e)
				}
			}
		}
		return nil
	})
	return
}

func checkRunStructure(tx *Transaction, run ReviewRun, runs map[string]ReviewRun) error {
	if run.Schema != 1 || !reviewRole(run.Role) || run.SemanticStatus != "unassessed" || run.ReportLanguage == "" {
		return reviewError(run.RunID + ": schema")
	}
	if run.ExecutionStatus != "ok" && run.ExecutionStatus != "failed" && run.ExecutionStatus != "interrupted" && run.ExecutionStatus != "not_started" {
		return reviewError(run.RunID + ": execution_status")
	}
	if run.ExecutionStatus == "ok" && (run.ExitCode != 0 || run.LaunchStatus != "started" || run.Hashes["report.md"] == "") {
		return reviewError(run.RunID + ": invalid ok")
	}
	if run.ExecutionStatus != "ok" && run.FailureReason == "" {
		return reviewError(run.RunID + ": missing failure reason")
	}
	var batch ReviewBatch
	ok, e := readReviewJSON(tx, reviewBatchName(run.BatchID), &batch)
	if e != nil {
		return e
	}
	if !ok || batch.Schema != 1 || batch.TaskContextHash != run.InputHashes["task-context.md"] || batch.BatchID != run.BatchID || batch.Base != run.Base || !reflect.DeepEqual(batch.TaskIDs, run.TaskIDs) || batch.ReportLanguage != run.ReportLanguage || batch.Requirements[run.Role] != "required" {
		return reviewError(run.RunID + ": batch mismatch")
	}
	for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
		requirement := batch.Requirements[role]
		if requirement != "required" && (!strings.HasPrefix(requirement, "N/A: ") || strings.TrimSpace(strings.TrimPrefix(requirement, "N/A: ")) == "") {
			return reviewError("batch requirements")
		}
	}
	if len(run.InputHashes) != 2 || run.InputHashes["task-context.md"] == "" || run.InputHashes["review-context.md"] == "" {
		return reviewError("input hash set")
	}
	for _, name := range []string{"task-context.md", "review-context.md", "output.raw", "stdout.log", "error.log"} {
		if run.Hashes[name] == "" {
			return reviewError("missing original: " + name)
		}
	}
	target := batch.TargetCommit
	found := run.Commit == target
	for i := len(batch.Advances) - 1; i >= 0; i-- {
		a := batch.Advances[i]
		if a.Target != target || strings.TrimSpace(a.Reason) == "" || len(a.Deliveries) == 0 {
			return reviewError("batch advance chain")
		}
		for _, id := range a.Deliveries {
			if !containsID(batch.TaskIDs, id) {
				return reviewError("foreign delivery")
			}
		}
		target = a.PreviousTarget
		if run.Commit == target {
			found = true
		}
	}
	if !found {
		return reviewError(run.RunID + ": target outside batch")
	}
	previous := run
	seen := map[string]bool{run.RunID: true}
	for previous.PreviousRunID != "" {
		p, ok := runs[previous.PreviousRunID]
		if !ok || seen[p.RunID] || p.Commit != previous.ReviewedCommit || p.BatchID != run.BatchID || p.Base != run.Base || p.Role != run.Role || !allPublished(p) {
			return reviewError(run.RunID + ": predecessor conflict")
		}
		seen[p.RunID] = true
		previous = p
	}
	if previous.ReviewedCommit != "" {
		return reviewError(run.RunID + ": missing predecessor")
	}
	sidecar, ok, e := tx.ReadGroup(reviewControlGroup, "runs/"+run.RunID+"/sidecar.json")
	if e != nil {
		return e
	}
	if !ok {
		return reviewError("missing sidecar")
	}
	var archived ReviewRun
	if json.Unmarshal(sidecar, &archived) != nil {
		return reviewError("sidecar schema")
	}
	expected := run
	expected.Published = nil
	if !reflect.DeepEqual(expected, archived) {
		return reviewError("sidecar/intent conflict")
	}
	files, e := reviewOriginals(tx, run.RunID, true)
	if e != nil {
		return e
	}
	if e = verifyReviewHashes(files, run.Hashes); e != nil {
		return e
	}
	for name, hash := range run.InputHashes {
		if run.Hashes[name] != hash {
			return reviewError("input hash conflict")
		}
	}
	return nil
}

func reviewAncestors(tx *Transaction, run ReviewRun) (map[string]ReviewRun, error) {
	result := map[string]ReviewRun{run.RunID: run}
	previous := run.PreviousRunID
	for previous != "" {
		if _, seen := result[previous]; seen {
			return nil, reviewError("predecessor cycle")
		}
		var ancestor ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(previous), &ancestor)
		if err != nil {
			return nil, err
		}
		if !ok || ancestor.RunID != previous {
			return nil, reviewError("missing predecessor: " + previous)
		}
		result[previous] = ancestor
		previous = ancestor.PreviousRunID
	}
	return result, nil
}

func settledReviewBatch(tx *Transaction, batchID string) error {
	entries, err := fs.ListDirectory(tx.root, control(tx.root, "groups", reviewControlGroup, "runs"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var run ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(entry.Name), &run)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("missing run")
		}
		if run.BatchID == batchID && (run.Phase != "finalized" || !allPublished(run)) {
			return reviewError("unsettled batch run: " + run.RunID)
		}
	}
	return nil
}

// The caller holds every member's shared or exclusive lock. Predecessors share
// the same fixed batch membership; validate their archives, not only receipts.
func verifyPublishedReview(tx *Transaction, run ReviewRun) error {
	ancestors, err := reviewAncestors(tx, run)
	if err != nil {
		return err
	}
	for _, ancestor := range ancestors {
		if !allPublished(ancestor) {
			return reviewError(ancestor.RunID + ": incomplete publication")
		}
		if err = checkRunStructure(tx, ancestor, ancestors); err != nil {
			return err
		}
		sidecar, ok, err := tx.ReadGroup(reviewControlGroup, "runs/"+ancestor.RunID+"/sidecar.json")
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("missing sidecar")
		}
		manifest := ReviewManifest{Schema: 1, Input: ancestor.ReviewInput, SidecarHash: ReviewDigest(sidecar), Hashes: ancestor.Hashes}
		for _, id := range ancestor.TaskIDs {
			if err = verifyCardReview(tx, id, manifest, sidecar); err != nil {
				return err
			}
		}
	}
	return nil
}
