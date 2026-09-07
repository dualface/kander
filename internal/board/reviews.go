package board

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

const reviewControlGroup = "00000000-review-archive-group"

var reviewIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ReviewInput is the immutable identity of one invocation. Hashes cover bytes,
// not normalized Markdown. ReportLanguage is frozen at intent creation.
type ReviewInput struct {
	TaskGroup      string            `json:"task_group"`
	Advance        *ReviewAdvance    `json:"advance,omitempty"`
	RunID          string            `json:"run_id"`
	BatchID        string            `json:"batch_id"`
	PreviousRunID  string            `json:"previous_run_id,omitempty"`
	TaskIDs        []string          `json:"task_ids"`
	Role           string            `json:"role"`
	Reviewer       string            `json:"reviewer"`
	Model          string            `json:"model"`
	Effort         string            `json:"effort"`
	CWD            string            `json:"cwd"`
	Base           string            `json:"base"`
	Commit         string            `json:"commit"`
	ReviewedCommit string            `json:"reviewed_commit,omitempty"`
	ReportLanguage string            `json:"report_language"`
	InputHashes    map[string]string `json:"input_hashes"`
}

// ReviewAdvance records the explicit CAS and attribution for every new commit.
// The review layer verifies the Git range; board only validates structure.
type ReviewAdvance struct {
	PreviousTarget string            `json:"previous_target"`
	Target         string            `json:"target"`
	Reason         string            `json:"reason"`
	Deliveries     map[string]string `json:"deliveries"`
}

type ReviewBatch struct {
	TaskContextHash string            `json:"task_context_hash"`
	Schema          int               `json:"schema"`
	BatchID         string            `json:"batch_id"`
	TaskIDs         []string          `json:"task_ids"`
	Base            string            `json:"base"`
	TargetCommit    string            `json:"target_commit"`
	ReportLanguage  string            `json:"report_language"`
	Requirements    map[string]string `json:"requirements"`
	Advances        []ReviewAdvance   `json:"advances"`
	Revision        uint64            `json:"revision"`
}

// ReviewRun separates execution facts from semantic interpretation. An ok run
// never asserts PASS. Publication is complete only after all receipts verify.
type ReviewRun struct {
	Schema int `json:"schema"`
	ReviewInput
	KanderVersion   string            `json:"kander_version"`
	Phase           string            `json:"phase"`
	LaunchStatus    string            `json:"launch_status"`
	ExecutionStatus string            `json:"execution_status"`
	SemanticStatus  string            `json:"semantic_status"`
	FailureReason   string            `json:"failure_reason,omitempty"`
	ExitCode        int               `json:"exit_code"`
	CreatedAt       string            `json:"created_at"`
	FinishedAt      string            `json:"finished_at,omitempty"`
	DurationMS      int64             `json:"duration_ms"`
	Hashes          map[string]string `json:"hashes"`
	Published       map[string]bool   `json:"published"`
}

type ReviewIndex struct {
	RunID           string `json:"run_id"`
	BatchID         string `json:"batch_id"`
	Role            string `json:"role"`
	ExecutionStatus string `json:"execution_status"`
	Base            string `json:"base"`
	Commit          string `json:"commit"`
	PreviousRunID   string `json:"previous_run_id,omitempty"`
	Report          string `json:"report"`
}

type ReviewManifest struct {
	Schema      int               `json:"schema"`
	Input       ReviewInput       `json:"input"`
	SidecarHash string            `json:"sidecar_hash"`
	Hashes      map[string]string `json:"hashes"`
}

func reviewError(detail string) error { return kanbanError("board.review_evidence_invalid", detail) }
func ReviewDigest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func NewReviewID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func ValidReviewID(id string) bool { return reviewIDPattern.MatchString(id) }
func reviewJSON(value any) string {
	b, _ := json.MarshalIndent(value, "", "  ")
	return string(b) + "\n"
}
func reviewRunName(id string) string   { return "runs/" + id + "/run.json" }
func reviewBatchName(id string) string { return "batches/" + id + ".json" }
func reviewScope(ids []string, readOnly bool) LockScope {
	return LockScope{Groups: []string{reviewControlGroup}, Tasks: ids, ReadOnly: readOnly}
}
func readReviewJSON(tx *Transaction, name string, value any) (bool, error) {
	b, exists, err := tx.ReadGroup(reviewControlGroup, name)
	if err != nil || !exists {
		return exists, err
	}
	if err := json.Unmarshal(b, value); err != nil {
		return true, reviewError(name + ": " + err.Error())
	}
	return true, nil
}

// LockReviewRun serializes retries across processes without holding board,
// group, or task locks while a reviewer runs. The OS releases it on process exit.
func LockReviewRun(root, runID string) (func() error, error) {
	if !ValidReviewID(runID) {
		return nil, reviewError("run_id")
	}
	if err := ensureLayout(root); err != nil {
		return nil, err
	}
	if err := ensureControl(root); err != nil {
		return nil, err
	}
	f, err := fs.OpenLockFile(root, control(root, "locks", "review-run-"+runID+".lock"))
	if err != nil {
		return nil, err
	}
	lock, err := fs.LockExclusive(f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return func() error { return errors.Join(lock.Unlock(), f.Close()) }, nil
}

func normalizedReviewTasks(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		n, err := NormalizeTaskID(id)
		if err != nil {
			return nil, err
		}
		if !containsID(out, n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, reviewError("task_ids")
	}
	return out, nil
}
func reviewCards(tx *Transaction, ids []string, fallback, frozen string) (string, error) {
	language := frozen
	group := ""
	for i, id := range ids {
		s, err := tx.Snapshot(id)
		if err != nil {
			return "", err
		}
		if !s.Entry.IsDirectory() {
			return "", kanbanError("board.migration_required", id)
		}
		if s.Entry.State != "working" && s.Entry.State != "review" {
			return "", reviewError(id + ": working/review")
		}
		g := TaskGroupFrom(s.Text)
		if i == 0 {
			group = g
		} else if g != group || group == "" {
			return "", reviewError("task group membership")
		}
		l := MetadataFrom(s.Text, FieldLanguage)
		if l == "" {
			if frozen != "" {
				l = frozen
			} else {
				l = fallback
			}
		}
		if _, err := config.ValidateAgentLanguage(l); err != nil {
			return "", err
		}
		if language == "" {
			language = l
		} else if language != l {
			return "", reviewError(id + ": report_language")
		}
	}
	return language, nil
}

// PrepareReviewRun commits all input bytes and intent before process launch.
// Reusing an identity returns its durable state and never authorizes a rerun.
func PrepareReviewRun(root string, input ReviewInput, requirements map[string]string, advance *ReviewAdvance, originals map[string][]byte, version string) (run ReviewRun, fresh bool, err error) {
	if !ValidReviewID(input.RunID) || !ValidReviewID(input.BatchID) || input.PreviousRunID != "" && !ValidReviewID(input.PreviousRunID) {
		return run, false, reviewError("run/batch/previous ID")
	}
	input.TaskIDs, err = normalizedReviewTasks(input.TaskIDs)
	if err != nil {
		return run, false, err
	}
	input.Advance = advance
	input.InputHashes = map[string]string{}
	for name, data := range originals {
		if name != "task-context.md" && name != "review-context.md" {
			return run, false, reviewError("input name")
		}
		input.InputHashes[name] = ReviewDigest(data)
	}
	if len(originals) != 2 {
		return run, false, reviewError("missing inputs")
	}
	err = WithTransaction(root, reviewScope(input.TaskIDs, false), func(tx *Transaction) error {
		exists, e := readReviewJSON(tx, reviewRunName(input.RunID), &run)
		if e != nil {
			return e
		}
		fallback := input.ReportLanguage
		input.ReportLanguage = ""
		if exists {
			input.ReportLanguage = run.ReportLanguage
		} else {
			var existingBatch ReviewBatch
			known, e := readReviewJSON(tx, reviewBatchName(input.BatchID), &existingBatch)
			if e != nil {
				return e
			}
			if known {
				input.ReportLanguage = existingBatch.ReportLanguage
			}
		}
		language, e := reviewCards(tx, input.TaskIDs, fallback, input.ReportLanguage)
		if e != nil {
			return e
		}
		input.ReportLanguage = language
		first, e := tx.Snapshot(input.TaskIDs[0])
		if e != nil {
			return e
		}
		input.TaskGroup = TaskGroupFrom(first.Text)
		if exists {
			if run.Schema != 1 || !reflect.DeepEqual(input, run.ReviewInput) {
				return reviewError(input.RunID + ": input conflict")
			}
			var batch ReviewBatch
			ok, e := readReviewJSON(tx, reviewBatchName(input.BatchID), &batch)
			if e != nil {
				return e
			}
			if !ok {
				return reviewError("missing batch")
			}
			if len(requirements) > 0 && !reflect.DeepEqual(requirements, batch.Requirements) {
				return reviewError("requirements conflict")
			}
			return nil
		}
		var batch ReviewBatch
		exists, e = readReviewJSON(tx, reviewBatchName(input.BatchID), &batch)
		if e != nil {
			return e
		}
		if !exists {
			if advance != nil || len(requirements) == 0 {
				return reviewError("new batch requires requirements; no advance")
			}
			for role, requirement := range requirements {
				if !reviewRole(role) || requirement != "required" && (!strings.HasPrefix(requirement, "N/A: ") || strings.TrimSpace(strings.TrimPrefix(requirement, "N/A: ")) == "") {
					return reviewError("requirements")
				}
			}
			for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
				if strings.TrimSpace(requirements[role]) == "" {
					return reviewError("missing requirement: " + role)
				}
			}
			batch = ReviewBatch{TaskContextHash: input.InputHashes["task-context.md"], Schema: 1, BatchID: input.BatchID, TaskIDs: input.TaskIDs, Base: input.Base, TargetCommit: input.Commit, ReportLanguage: language, Requirements: requirements, Revision: 1}
		} else {
			if batch.Schema != 1 || batch.TaskContextHash != input.InputHashes["task-context.md"] || batch.Base != input.Base || !reflect.DeepEqual(batch.TaskIDs, input.TaskIDs) || batch.ReportLanguage != language || len(requirements) > 0 && !reflect.DeepEqual(requirements, batch.Requirements) {
				return reviewError("batch binding conflict")
			}
			if batch.TargetCommit != input.Commit {
				if advance == nil || advance.PreviousTarget != batch.TargetCommit || advance.Target != input.Commit || strings.TrimSpace(advance.Reason) == "" || len(advance.Deliveries) == 0 {
					return reviewError("batch target CAS conflict")
				}
				for commit, id := range advance.Deliveries {
					if commit == "" || !containsID(input.TaskIDs, id) {
						return reviewError("foreign delivery")
					}
				}
				if e = settledReviewBatch(tx, input.BatchID); e != nil {
					return e
				}
				batch.Advances = append(batch.Advances, *advance)
				batch.TargetCommit = input.Commit
				batch.Revision++
			} else if advance != nil {
				return reviewError("redundant batch advance")
			}
		}
		if batch.Requirements[input.Role] != "required" {
			return reviewError("role is not required")
		}
		if input.ReviewedCommit != "" && input.PreviousRunID == "" || input.ReviewedCommit == "" && input.PreviousRunID != "" {
			return reviewError("previous_run_id/reviewed_commit")
		}
		if input.PreviousRunID != "" {
			var previous ReviewRun
			ok, e := readReviewJSON(tx, reviewRunName(input.PreviousRunID), &previous)
			if e != nil {
				return e
			}
			if !ok || previous.Phase != "finalized" || previous.BatchID != input.BatchID || previous.Role != input.Role || previous.Base != input.Base || previous.Commit != input.ReviewedCommit || !allPublished(previous) {
				return reviewError("previous run mismatch or incomplete publication")
			}
			if e = verifyPublishedReview(tx, previous); e != nil {
				return e
			}
		}
		run = ReviewRun{Schema: 1, ReviewInput: input, KanderVersion: version, Phase: "prepared", LaunchStatus: "not_started", ExecutionStatus: "incomplete", SemanticStatus: "unassessed", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Hashes: map[string]string{}, Published: map[string]bool{}}
		if e = tx.PutGroup(reviewControlGroup, reviewBatchName(input.BatchID), reviewJSON(batch)); e != nil {
			return e
		}
		for name, data := range originals {
			if e = tx.putGroupBytes(reviewControlGroup, "runs/"+input.RunID+"/inputs/"+name, data); e != nil {
				return e
			}
		}
		for _, name := range []string{"output.raw", "stdout.log", "error.log"} {
			if e = tx.PutGroup(reviewControlGroup, "runs/"+input.RunID+"/staging/"+name, ""); e != nil {
				return e
			}
		}
		fresh = true
		return tx.PutGroup(reviewControlGroup, reviewRunName(input.RunID), reviewJSON(run))
	})
	return
}
func reviewRole(role string) bool {
	return role == "PM" || role == "QA" || role == "CSA" || role == "Hacker"
}
func allPublished(run ReviewRun) bool {
	for _, id := range run.TaskIDs {
		if !run.Published[id] {
			return false
		}
	}
	return len(run.TaskIDs) > 0
}

// ReviewStaging is the producer-owned output boundary, independent of card paths.
func ReviewStaging(root, runID string) (string, error) {
	if !ValidReviewID(runID) {
		return "", reviewError("run_id")
	}
	path := control(root, "groups", reviewControlGroup, "runs", runID, "staging")
	_, err := fs.DirectoryIdentity(root, path)
	return path, err
}

// UpdateReviewRun records execution phase before and after launch. Callers hold
// LockReviewRun; this short transaction never retains a card lock during review.
func UpdateReviewRun(root string, run ReviewRun) error {
	return WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var old ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(run.RunID), &old)
		if err != nil {
			return err
		}
		if !ok || old.Phase == "finalized" || !reflect.DeepEqual(old.ReviewInput, run.ReviewInput) {
			return reviewError("run update conflict")
		}
		return tx.PutGroup(reviewControlGroup, reviewRunName(run.RunID), reviewJSON(run))
	})
}

// StoreReviewArtifact snapshots generated prompts before launch, without giving
// the reviewer a board transaction or write access through a Kander command.
func StoreReviewArtifact(root, runID, name string, data []byte) error {
	if !ValidReviewID(runID) || (name != "prompt.txt" && name != "evidence.txt") {
		return reviewError("artifact")
	}
	return WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var run ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(runID), &run)
		if err != nil {
			return err
		}
		if !ok || run.Phase != "prepared" {
			return reviewError("artifact phase")
		}
		path := "runs/" + runID + "/inputs/" + name
		old, exists, err := tx.ReadGroup(reviewControlGroup, path)
		if err != nil {
			return err
		}
		if exists {
			if string(old) == string(data) {
				return nil
			}
			return reviewError("artifact conflict")
		}
		return tx.putGroupBytes(reviewControlGroup, path, data)
	})
}

// FinalizeReviewRun freezes originals only after the execution layer has settled
// process collection, target checks and runtime cleanup. Recovery uses interrupted.
func FinalizeReviewRun(root string, run ReviewRun, report []byte) (ReviewRun, error) {
	err := WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var old ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(run.RunID), &old)
		if err != nil {
			return err
		}
		if !ok || old.Phase == "finalized" || !reflect.DeepEqual(old.ReviewInput, run.ReviewInput) {
			return reviewError("finalize conflict")
		}
		if run.ExecutionStatus == "ok" && (run.ExitCode != 0 || run.LaunchStatus != "started" || len(report) == 0) {
			return reviewError("invalid ok finalization")
		}
		if run.ExecutionStatus != "ok" && (run.ExitCode == 0 || run.FailureReason == "") {
			return reviewError("missing failure facts")
		}
		files, err := reviewOriginals(tx, run.RunID, false)
		if err != nil {
			return err
		}
		if report != nil {
			files["report.md"] = report
		}
		for _, name := range []string{"task-context.md", "review-context.md", "output.raw", "stdout.log", "error.log"} {
			if _, ok := files[name]; !ok {
				return reviewError("missing original: " + name)
			}
		}
		run.Hashes = map[string]string{}
		for name, data := range files {
			run.Hashes[name] = ReviewDigest(data)
			if err = tx.putGroupBytes(reviewControlGroup, "runs/"+run.RunID+"/originals/"+name, data); err != nil {
				return err
			}
		}
		run.Phase = "finalized"
		run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		start, err := time.Parse(time.RFC3339Nano, run.CreatedAt)
		if err != nil {
			return err
		}
		run.DurationMS = time.Since(start).Milliseconds()
		sidecar := run
		sidecar.Published = nil
		if err = tx.PutGroup(reviewControlGroup, "runs/"+run.RunID+"/sidecar.json", reviewJSON(sidecar)); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, reviewRunName(run.RunID), reviewJSON(run))
	})
	return run, err
}
func reviewOriginals(tx *Transaction, id string, final bool) (map[string][]byte, error) {
	result := map[string][]byte{}
	names := map[string]string{"task-context.md": "inputs", "review-context.md": "inputs", "prompt.txt": "inputs", "evidence.txt": "inputs", "output.raw": "staging", "stdout.log": "staging", "error.log": "staging"}
	if final {
		names["report.md"] = "originals"
	}
	for name, dir := range names {
		if final {
			dir = "originals"
		}
		data, ok, err := tx.ReadGroup(reviewControlGroup, "runs/"+id+"/"+dir+"/"+name)
		if err != nil {
			return nil, err
		}
		if ok {
			result[name] = data
		}
	}
	return result, nil
}

// PublishReviewRun commits each card independently, preserving successful
// receipts on partial failure. A retry verifies originals and only fills gaps.
func PublishReviewRun(root string, runID string) (ReviewRun, map[string]error, error) {
	return publishReviewRun(root, runID, nil)
}
func publishReviewRun(root string, runID string, published func(string)) (ReviewRun, map[string]error, error) {
	var run ReviewRun
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewRunName(runID), &run)
		if e != nil {
			return e
		}
		if !ok || run.Phase != "finalized" {
			return reviewError("run not finalized")
		}
		return nil
	})
	if err != nil {
		return run, nil, err
	}
	failures := map[string]error{}
	for _, id := range run.TaskIDs {
		err = WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
			var current ReviewRun
			ok, e := readReviewJSON(tx, reviewRunName(runID), &current)
			if e != nil {
				return e
			}
			if !ok || !reflect.DeepEqual(current.ReviewInput, run.ReviewInput) {
				return reviewError("intent conflict")
			}
			s, e := tx.Snapshot(id)
			if e != nil {
				return e
			}
			if TaskGroupFrom(s.Text) != run.TaskGroup {
				return reviewError("card group binding")
			}
			if !current.Published[id] {
				if _, e = reviewCards(tx, []string{id}, run.ReportLanguage, run.ReportLanguage); e != nil {
					return e
				}
			}
			var batch ReviewBatch
			ok, e = readReviewJSON(tx, reviewBatchName(run.BatchID), &batch)
			if e != nil {
				return e
			}
			if !ok || batch.Base != run.Base || !reflect.DeepEqual(batch.TaskIDs, run.TaskIDs) || batch.ReportLanguage != run.ReportLanguage {
				return reviewError("publication batch binding")
			}
			ancestors, e := reviewAncestors(tx, current)
			if e != nil {
				return e
			}
			if e = checkRunStructure(tx, current, ancestors); e != nil {
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
			if current.Published[id] {
				return verifyCardReview(tx, id, manifest, sidecar)
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
			text := appendReviewIndex(s.Text, string(line))
			if e = tx.Put(id, "spec.md", text); e != nil {
				return e
			}
			current.Published[id] = true
			return tx.PutGroup(reviewControlGroup, reviewRunName(runID), reviewJSON(current))
		})
		if err != nil {
			failures[id] = err
		} else {
			run.Published[id] = true
			if published != nil {
				published(id)
			}
		}
	}
	return run, failures, nil
}
func verifyReviewHashes(files map[string][]byte, hashes map[string]string) error {
	if len(files) != len(hashes) {
		return reviewError("original set mismatch")
	}
	for name, hash := range hashes {
		data, ok := files[name]
		if !ok || ReviewDigest(data) != hash {
			return reviewError(name + ": hash mismatch")
		}
	}
	return nil
}
func putImmutableReview(tx *Transaction, id, name string, data []byte) error {
	old, err := tx.Read(id, name)
	if err == nil {
		if old == string(data) {
			return nil
		}
		return reviewError(name + ": immutable conflict")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return tx.PutBytes(id, name, data)
}
func reviewIndex(run ReviewRun) ReviewIndex {
	report := "reviews/" + run.RunID + "/output.raw"
	if _, ok := run.Hashes["report.md"]; ok {
		report = "reviews/" + run.RunID + "/report.md"
	}
	return ReviewIndex{run.RunID, run.BatchID, run.Role, run.ExecutionStatus, run.Base, run.Commit, run.PreviousRunID, report}
}
func appendReviewIndex(text, line string) string {
	marker := "## REVIEWS"
	at := strings.Index(text, "\n"+marker+"\n")
	if at < 0 {
		return strings.TrimRight(text, "\n") + "\n\n" + marker + "\n\n- " + line + "\n"
	}
	start := at + len(marker) + 2
	end := strings.Index(text[start:], "\n## ")
	if end < 0 {
		return strings.TrimRight(text, "\n") + "\n- " + line + "\n"
	}
	at = start + end
	return strings.TrimRight(text[:at], "\n") + "\n- " + line + "\n\n" + text[at+1:]
}

// ParseReviewIndexes is shared by board, review, and future disposition gates.
// It reads only the machine-owned section, never reviewer prose.
func ParseReviewIndexes(text string) ([]ReviewIndex, error) {
	if len(regexp.MustCompile(`(?m)^## REVIEWS\s*$`).FindAllString(text, -1)) > 1 {
		return nil, reviewError("duplicate REVIEWS section")
	}
	body, ok := SectionBody(text, "REVIEWS")
	if !ok {
		return nil, nil
	}
	var result []ReviewIndex
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var index ReviewIndex
		if !strings.HasPrefix(line, "- ") || json.Unmarshal([]byte(strings.TrimPrefix(line, "- ")), &index) != nil || !ValidReviewID(index.RunID) || !ValidReviewID(index.BatchID) || !reviewRole(index.Role) || seen[index.RunID] {
			return nil, reviewError("invalid/duplicate review index")
		}
		seen[index.RunID] = true
		result = append(result, index)
	}
	return result, nil
}
func verifyCardReview(tx *Transaction, id string, manifest ReviewManifest, sidecar []byte) error {
	prefix := "reviews/" + manifest.Input.RunID + "/"
	b, err := tx.Read(id, prefix+"manifest.json")
	if err != nil {
		return err
	}
	var actual ReviewManifest
	if json.Unmarshal([]byte(b), &actual) != nil || !reflect.DeepEqual(actual, manifest) {
		return reviewError("manifest conflict")
	}
	b, err = tx.Read(id, prefix+"sidecar.json")
	if err != nil {
		return err
	}
	if b != string(sidecar) || ReviewDigest([]byte(b)) != manifest.SidecarHash {
		return reviewError("sidecar hash mismatch")
	}
	for name, hash := range manifest.Hashes {
		b, err = tx.Read(id, prefix+name)
		if err != nil {
			return err
		}
		if ReviewDigest([]byte(b)) != hash {
			return reviewError(name + ": hash mismatch")
		}
	}
	s, err := tx.Snapshot(id)
	if err != nil {
		return err
	}
	indexes, err := ParseReviewIndexes(s.Text)
	if err != nil {
		return err
	}
	var run ReviewRun
	if json.Unmarshal(sidecar, &run) != nil {
		return reviewError("sidecar schema")
	}
	language := MetadataFrom(s.Text, FieldLanguage)
	if language != "" && language != run.ReportLanguage || TaskGroupFrom(s.Text) != run.TaskGroup {
		return reviewError("card language/group binding")
	}
	matches := 0
	for _, index := range indexes {
		if index.RunID == run.RunID {
			matches++
			if index != reviewIndex(run) {
				return reviewError("index mismatch")
			}
		}
	}
	if matches != 1 {
		return reviewError("missing index")
	}
	return nil
}

// ReadReviewRun returns committed execution facts; callers must still use
// ReviewPublicationComplete before treating the cross-card evidence as complete.
func ReadReviewRun(root, id string) (run ReviewRun, err error) {
	if !ValidReviewID(id) {
		return run, reviewError("run_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewRunName(id), &run)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing run: " + id)
		}
		return nil
	})
	return
}
func ReviewPublicationComplete(root, id string) error {
	run, err := ReadReviewRun(root, id)
	if err != nil {
		return err
	}
	if run.Phase != "finalized" || !allPublished(run) {
		return reviewError(id + ": incomplete publication")
	}
	return WithTransaction(root, reviewScope(run.TaskIDs, true), func(tx *Transaction) error {
		return verifyPublishedReview(tx, run)
	})
}

// LookupReviewRun distinguishes an unused identity from corrupt durable state.
func LookupReviewRun(root, id string) (run ReviewRun, exists bool, err error) {
	if !ValidReviewID(id) {
		return run, false, reviewError("run_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var e error
		exists, e = readReviewJSON(tx, reviewRunName(id), &run)
		return e
	})
	return
}

// ReadReviewOriginal exposes immutable evidence without exposing control paths.
func ReadReviewOriginal(root, id, name string) (data []byte, err error) {
	if !ValidReviewID(id) {
		return nil, reviewError("run_id")
	}
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var run ReviewRun
		ok, e := readReviewJSON(tx, reviewRunName(id), &run)
		if e != nil {
			return e
		}
		if !ok || run.Phase != "finalized" {
			return reviewError("run not finalized")
		}
		hash, ok := run.Hashes[name]
		if !ok {
			return os.ErrNotExist
		}
		data, ok, e = tx.ReadGroup(reviewControlGroup, "runs/"+id+"/originals/"+name)
		if e != nil {
			return e
		}
		if !ok || ReviewDigest(data) != hash {
			return reviewError("original hash mismatch")
		}
		return nil
	})
	return
}
