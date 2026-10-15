package issue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

type resultStore struct {
	Schema    int                      `json:"schema_version"`
	SourceKey string                   `json:"source_key"`
	TaskID    string                   `json:"task_id"`
	IssueID   int64                    `json:"issue_id"`
	Records   map[string]*ResultRecord `json:"records"`
}

func resultHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func resultError(detail string) error { return NewError(ErrorInvalidQuery, "result", detail) }

// taskKey is the card identity a record's version hashes: the anchor alone,
// or every member in work order when the record covers several cards.
func (r *ResultRecord) taskKey(anchor string) string {
	if len(r.TaskIDs) == 0 {
		return anchor
	}
	return strings.Join(r.TaskIDs, ",")
}

func recordTaskKeyValid(record *ResultRecord, anchor string) bool {
	return len(record.TaskIDs) == 0 || (len(record.TaskIDs) > 1 && record.TaskIDs[0] == anchor)
}

// withResultStore serializes all local writers, including recovery, for an
// issue across task worktrees. Persist precedes every remote mutation.
func withResultStore(ctx context.Context, root string, repository Repository, number int, cardID string, fn func(*resultStore, func() error) error) (err error) {
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return err
	}
	if cardID == "" {
		return resultError("a bound done card is required")
	}
	dir, err := board.EnsurePrivateDataDir(root, "issue-results", "v1", resultHash(key))
	if err != nil {
		return err
	}
	file, err := fs.OpenLockFile(root, filepath.Join(dir, "lock"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	lock, err := fs.LockExclusiveContext(ctx, file)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	path := filepath.Join(dir, "state.json")
	data, found, err := fs.ReadRegularFileIfExists(root, path)
	if err != nil {
		return err
	}
	store := resultStore{Schema: 1, SourceKey: key, TaskID: cardID, Records: map[string]*ResultRecord{}}
	if found {
		if err := json.Unmarshal(data, &store); err != nil {
			return err
		}
		if store.TaskID != cardID {
			// A sibling ID is a usage mistake, not a damaged record: name the anchor.
			if sets, err := LoadWorkSets(root); err != nil {
				return err
			} else if err := siblingCardError(sets[key], cardID); err != nil {
				return err
			}
		}
		if store.Schema != 1 || store.SourceKey != key || store.TaskID != cardID || store.Records == nil {
			return resultError("inconsistent result recovery record")
		}
		for version, record := range store.Records {
			if record == nil || record.Version != version || !recordTaskKeyValid(record, cardID) || resultVersion(record.taskKey(cardID), record.Commits, record.Outcomes, record.FullyResolved) != version {
				return resultError("invalid result record")
			}
			switch record.Status {
			case "uncertain", "published", "equivalent", "rejected":
			default:
				return resultError("invalid result status")
			}
		}
	}
	save := func() error {
		encoded, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			return err
		}
		return fs.WriteTextAtomic(root, path, string(encoded)+"\n", true)
	}
	return fn(&store, save)
}

var resultSHA = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// ReadResultWork validates the anchor binding and the readiness of the
// issue's work set, then reads the completion evidence of every member.
func ReadResultWork(root string, repository Repository, number int, cardID string) (ResultWork, error) {
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return ResultWork{}, err
	}
	sets, err := LoadWorkSets(root)
	if err != nil {
		return ResultWork{}, err
	}
	set, ok := sets[key]
	if !ok || cardID == "" {
		return ResultWork{}, NewError(ErrorInvalidQuery, "card", Sanitize(cardID))
	}
	if set.Anchor.TaskID != cardID {
		if err := siblingCardError(set, cardID); err != nil {
			return ResultWork{}, err
		}
		return ResultWork{}, NewError(ErrorInvalidQuery, "card", Sanitize(cardID))
	}
	if pending := set.Pending(); len(pending) > 0 {
		return ResultWork{}, resultError("work set incomplete; pending cards: " + strings.Join(pending, ", "))
	}
	work := ResultWork{Excluded: set.Excluded}
	if work.Excluded == nil {
		work.Excluded = []WorkCard{}
	}
	for i, member := range set.Members() {
		card, err := readResultEvidence(root, key, repository, number, member.TaskID, i == 0)
		if err != nil {
			return ResultWork{}, err
		}
		work.Cards = append(work.Cards, card)
	}
	work.combine()
	return work, nil
}

// siblingCardError explains a --card that names any non-anchor card bound to
// the issue, a member sibling or an excluded one, with the anchor's ID; it
// returns nil for the anchor and for cards outside the work set.
func siblingCardError(set WorkSet, cardID string) error {
	if cardID == "" || set.Anchor.TaskID == "" || set.Anchor.TaskID == cardID || !set.Contains(cardID) {
		return nil
	}
	return resultError("card " + cardID + " is not the anchor card; use --card " + set.Anchor.TaskID)
}

// combine derives the work-wide identity. A single-card work keeps the exact
// identity of the anchor card, so records written before work sets existed
// stay valid.
func (w *ResultWork) combine() {
	if len(w.Cards) == 1 {
		card := w.Cards[0]
		w.TaskKey, w.Commits, w.EvidenceVersion, w.Digest = card.TaskID, card.Commits, card.EvidenceVersion, card.Digest
		return
	}
	ids := make([]string, 0, len(w.Cards))
	var commits, evidence, digests []string
	for _, card := range w.Cards {
		ids = append(ids, card.TaskID)
		commits = append(commits, card.Commits...)
		evidence = append(evidence, card.TaskID, card.EvidenceVersion)
		digests = append(digests, card.TaskID, card.Digest)
	}
	w.TaskKey = strings.Join(ids, ",")
	w.Commits = uniqueSorted(commits)
	w.EvidenceVersion = resultHash(evidence)
	w.Digest = resultHash(digests)
}

func uniqueSorted(values []string) []string {
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	unique := []string{}
	for _, value := range sorted {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

// readResultEvidence reads one complete member. Only the anchor carries the
// source binding; a sibling is bound through its SOURCE_ISSUE line, which the
// work set scan already checked.
func readResultEvidence(root, key string, repository Repository, number int, taskID string, anchor bool) (ResultCard, error) {
	snapshot, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		return ResultCard{}, err
	}
	if !workCardOf(snapshot.Entry, snapshot.Text).Complete() {
		return ResultCard{}, resultError("card " + taskID + " is no longer complete")
	}
	if anchor {
		source, found, err := board.ReadCardFile(snapshot.Entry, SourceFileName)
		if err != nil {
			return ResultCard{}, err
		}
		if !found {
			return ResultCard{}, resultError("missing source binding")
		}
		binding, err := UnmarshalImportSnapshot(source)
		if err != nil {
			return ResultCard{}, err
		}
		if err := validateImportIdentity(binding, repository, number); err != nil {
			return ResultCard{}, err
		}
	}
	report, hasReport, err := board.ReadCardFile(snapshot.Entry, "report.md")
	if err != nil {
		return ResultCard{}, err
	}
	if snapshot.Entry.Kind == "large" && (!hasReport || strings.TrimSpace(string(report)) == "") {
		return ResultCard{}, resultError("large card " + taskID + " has no completion report")
	}
	summary, _ := board.SectionBody(snapshot.Text, "SUMMARY")
	acceptance, _ := board.SectionBody(snapshot.Text, "ACCEPTANCE_CRITERIA")
	// Completion documentation determines delivery identity. Implementation is
	// still provided for verification, but unrelated execution logs add no key.
	return ResultCard{TaskID: taskID, State: snapshot.Entry.State, Spec: snapshot.Text, Report: string(report),
		Commits:         uniqueSorted(resultSHA.FindAllString(summary+"\n"+string(report), -1)),
		EvidenceVersion: resultHash([]string{summary, acceptance, string(report)}),
		Digest:          resultHash([]string{key, taskID, snapshot.Text, string(report)})}, nil
}

// resultPending reports whether a ready work set lacks a published or
// equivalent result for its current completion evidence. It reads the local
// records without the writer lock: they are replaced atomically and this is
// only a display hint. Unreadable evidence or records count as pending so the
// issue still draws attention.
func resultPending(root string, set WorkSet) bool {
	key := set.SourceKey
	evidence := make([]ResultCard, 0, len(set.Siblings)+1)
	for _, member := range set.Members() {
		snapshot, err := board.ReadSnapshot(root, member.TaskID)
		if err != nil {
			return true
		}
		report, _, err := board.ReadCardFile(snapshot.Entry, "report.md")
		if err != nil {
			return true
		}
		summary, _ := board.SectionBody(snapshot.Text, "SUMMARY")
		acceptance, _ := board.SectionBody(snapshot.Text, "ACCEPTANCE_CRITERIA")
		evidence = append(evidence, ResultCard{TaskID: member.TaskID, EvidenceVersion: resultHash([]string{summary, acceptance, string(report)})})
	}
	work := ResultWork{Cards: evidence}
	work.combine()
	dir, err := board.PrivateDataPath(root, "issue-results", "v1", resultHash(key))
	if err != nil {
		return true
	}
	data, found, err := fs.ReadRegularFileIfExists(root, filepath.Join(dir, "state.json"))
	if err != nil {
		return true
	}
	if !found {
		return true
	}
	var store resultStore
	if err := json.Unmarshal(data, &store); err != nil {
		return true
	}
	for _, record := range store.Records {
		if record != nil && (record.Status == "published" || record.Status == "equivalent") && record.EvidenceVersion == work.EvidenceVersion {
			return false
		}
	}
	return true
}

func resultToken(digest string, remote ResultRemote) string {
	return resultHash([]string{digest, resultHash(remote)})
}

func readResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, store *resultStore) (ResultWork, ResultRemote, error) {
	work, err := ReadResultWork(root, repository, number, cardID)
	if err != nil {
		return work, ResultRemote{}, err
	}
	if provider == nil {
		return work, ResultRemote{}, resultError("result provider unavailable")
	}
	remote, err := provider.ReadResult(ctx, repository, number)
	if err != nil {
		return work, remote, err
	}
	if remote.SourceKey != store.SourceKey || remote.IssueID <= 0 || remote.ActorID <= 0 || remote.Revision == "" || remote.StateVersion == "" || (remote.State != "open" && remote.State != "closed") || (store.IssueID != 0 && store.IssueID != remote.IssueID) {
		return work, remote, resultError("remote identity or state mismatch")
	}
	store.IssueID = remote.IssueID
	// Recheck after network latency; stale state or binding never reaches writes.
	current, err := ReadResultWork(root, repository, number, cardID)
	if err != nil {
		return work, remote, err
	}
	if current.Digest != work.Digest {
		return work, remote, resultError("card changed during inspection")
	}
	return work, remote, nil
}
