package board

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/internal/fs"
)

// compactedNames are the per-file originals compaction removes from
// originals/ and from every member card's reviews/<run>/ copy. Every other
// file, report.md and the JSON records included, is kept.
var compactedNames = []string{"evidence.txt", "error.log", "prompt.txt", "output.raw", "stdout.log", "review-context.md", "task-context.md", "review-contract.md"}

// Compaction receipt locations. inputs/ and staging/ go as whole trees and
// are recorded with the name compactedTree.
const (
	compactedInputs    = "inputs"
	compactedStaging   = "staging"
	compactedOriginals = "originals"
	compactedCardPre   = "card:"
	compactedTree      = "*"
)

// compactionReceipt records which files compaction removed from one settled
// run. It never copies a hash: run.json and the card manifests stay the
// reference, and a listed file that still exists is verified against them.
type compactionReceipt struct {
	Schema        int             `json:"schema"`
	RunID         string          `json:"run_id"`
	TaskIDs       []string        `json:"task_ids"`
	Files         []compactedFile `json:"files"`
	CompactedAt   string          `json:"compacted_at"`
	KanderVersion string          `json:"kander_version"`
}

type compactedFile struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
}

func compactionReceiptName(runID string) string { return "compactions/" + runID + ".json" }

func compactionReceiptPath(root, runID string) string {
	return control(root, "groups", pruneControlGroup, compactionReceiptName(runID))
}

// compactionBackupPath holds the copy archive pruning writes before it removes
// a receipt. Until the round knows which runs it deletes, the copy stands for
// the receipt, so an interrupted round never leaves a kept run without one.
func compactionBackupPath(root, runID string) string {
	return control(root, "groups", pruneControlGroup, "compaction-backups", runID+".json")
}

// readReceiptFile reads an optional receipt through a no-follow, non-blocking
// handle whose type is checked before any read, so a FIFO or another special
// file in its place fails instead of hanging the board.
func readReceiptFile(root, path string) ([]byte, bool, error) {
	f, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil || f == nil {
		return nil, false, err
	}
	data, err := io.ReadAll(f)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, true, err
	}
	return data, true, nil
}

// covers reports whether the receipt lists name at location. A tree entry
// covers every file below its location.
func (r compactionReceipt) covers(location, name string) bool {
	return slices.ContainsFunc(r.Files, func(f compactedFile) bool {
		return f.Location == location && (f.Name == name || f.Name == compactedTree)
	})
}

func invalidCompactionReceipt(runID string) error {
	return reviewError(runID + ": invalid compaction receipt")
}

// validCompactedFile accepts only the locations and names compaction writes.
func validCompactedFile(f compactedFile) bool {
	if f.Bytes < 0 {
		return false
	}
	switch f.Location {
	case compactedInputs, compactedStaging:
		return f.Name == compactedTree
	case compactedOriginals:
		return slices.Contains(compactedNames, f.Name)
	}
	task, ok := strings.CutPrefix(f.Location, compactedCardPre)
	return ok && taskIDRe.MatchString(task) && slices.Contains(compactedNames, f.Name)
}

// readCompactionReceipt reads the receipt of runID. A missing receipt is not
// an error; a receipt that names another run, lists an unknown location or
// name, or does not parse is invalid.
func readCompactionReceipt(root, runID string) (r compactionReceipt, exists bool, err error) {
	if !ValidReviewID(runID) {
		return r, false, reviewError("run_id")
	}
	data, ok, err := readReceiptFile(root, compactionReceiptPath(root, runID))
	if err == nil && !ok {
		// An archive cleanup that removed the receipt and stopped before it
		// deleted the run leaves the copy it wrote first.
		data, ok, err = readReceiptFile(root, compactionBackupPath(root, runID))
	}
	if err != nil || !ok {
		return r, false, err
	}
	if json.Unmarshal(data, &r) != nil || r.Schema != 1 || r.RunID != runID || !slices.IsSorted(r.TaskIDs) || len(r.TaskIDs) == 0 {
		return r, true, invalidCompactionReceipt(runID)
	}
	for _, f := range r.Files {
		if !validCompactedFile(f) {
			return r, true, invalidCompactionReceipt(runID)
		}
	}
	return r, true, nil
}

// runCompaction returns the receipt of a run whose members are taskIDs. A
// receipt bound to other members is invalid, so it can never relax the check
// of a run it was not written for.
func runCompaction(root, runID string, taskIDs []string) (compactionReceipt, bool, error) {
	r, exists, err := readCompactionReceipt(root, runID)
	if err != nil || !exists {
		return r, exists, err
	}
	ids := slices.Clone(taskIDs)
	slices.Sort(ids)
	if !slices.Equal(ids, r.TaskIDs) {
		return r, true, invalidCompactionReceipt(runID)
	}
	return r, true, nil
}

// compactedError names a file a compaction removed, so a reader never reports
// it as damaged evidence.
func compactedError(runID, name string) error {
	return kanbanError("board.review_compacted", runID, name)
}

// compactedRunError refuses to prepare or replay a compacted run again.
func compactedRunError(runID string) error {
	return kanbanError("board.review_run_compacted", runID)
}

// requireNotCompacted refuses a run that has a compaction receipt.
func requireNotCompacted(root string, run ReviewRun) error {
	_, compacted, err := runCompaction(root, run.RunID, run.TaskIDs)
	if err != nil {
		return err
	}
	if compacted {
		return compactedRunError(run.RunID)
	}
	return nil
}

// ReviewRunCompacted reports whether the run has a valid compaction receipt.
// An unknown run is not compacted.
func ReviewRunCompacted(root, id string) (compacted bool, err error) {
	if !ValidReviewID(id) {
		return false, reviewError("run_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var run ReviewRun
		exists, e := readReviewJSON(tx, reviewRunName(id), &run)
		if e != nil || !exists {
			return e
		}
		_, receipt, e := runCompaction(root, id, run.TaskIDs)
		compacted = receipt && e == nil
		return e
	})
	return
}

// CompactedReviewRuns maps each of ids to the sorted runs with a valid
// compaction receipt that bind it. Check prints them as information; an
// invalid receipt is reported by CheckReviewEvidence instead. A run whose
// receipt archive pruning removed before an interruption is found through
// its backup, which readers take for the receipt.
func CompactedReviewRuns(root string, ids []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(ids) == 0 {
		return result, nil
	}
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		runIDs := map[string]bool{}
		for _, dir := range []string{"compactions", "compaction-backups"} {
			entries, e := fs.ListDirectory(root, control(root, "groups", pruneControlGroup, dir))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return e
			}
			for _, entry := range entries {
				runID, ok := strings.CutSuffix(entry.Name, ".json")
				if entry.Kind == fs.KindFile && ok && ValidReviewID(runID) {
					runIDs[runID] = true
				}
			}
		}
		for _, runID := range sortedKeys(runIDs) {
			var run ReviewRun
			exists, e := readReviewJSON(tx, reviewRunName(runID), &run)
			if e != nil || !exists {
				continue
			}
			// The receipt wins over its backup, as for every other reader.
			if _, valid, e := runCompaction(root, runID, run.TaskIDs); e != nil || !valid {
				continue
			}
			for _, id := range run.TaskIDs {
				if slices.Contains(ids, id) {
					result[id] = append(result[id], runID)
				}
			}
		}
		return nil
	})
	for id := range result {
		slices.Sort(result[id])
	}
	return result, err
}

// compactionLookup returns, for one run, whether a file it lists is compacted.
// It is the only relaxation the integrity checks accept: a listed file that is
// missing passes, a listed file that exists is still hashed, and a file the
// receipt does not list must exist.
func compactionLookup(root string, run ReviewRun) (func(location, name string) bool, error) {
	r, exists, err := runCompaction(root, run.RunID, run.TaskIDs)
	if err != nil {
		return nil, err
	}
	return func(location, name string) bool { return exists && r.covers(location, name) }, nil
}
