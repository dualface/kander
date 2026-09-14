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
		if store.Schema != 1 || store.SourceKey != key || store.TaskID != cardID || store.Records == nil {
			return resultError("inconsistent result recovery record")
		}
		for version, record := range store.Records {
			if record == nil || record.Version != version || resultVersion(cardID, record.Commits, record.Outcomes, record.FullyResolved) != version {
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

// ReadResultCard validates the current done binding and reads its evidence.
func ReadResultCard(root string, repository Repository, number int, cardID string) (ResultCard, error) {
	if err := validateTriageCard(root, repository, number, cardID); err != nil {
		return ResultCard{}, err
	}
	snapshot, err := board.ReadSnapshot(root, cardID)
	if err != nil {
		return ResultCard{}, err
	}
	if snapshot.Entry.State != "done" {
		return ResultCard{}, resultError("card is no longer done")
	}
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
	report, hasReport, err := board.ReadCardFile(snapshot.Entry, "report.md")
	if err != nil {
		return ResultCard{}, err
	}
	if snapshot.Entry.Kind == "large" && (!hasReport || strings.TrimSpace(string(report)) == "") {
		return ResultCard{}, resultError("large card has no completion report")
	}
	summary, _ := board.SectionBody(snapshot.Text, "SUMMARY")
	acceptance, _ := board.SectionBody(snapshot.Text, "ACCEPTANCE_CRITERIA")
	// Completion documentation determines delivery identity. Implementation is
	// still provided for verification, but unrelated execution logs add no key.
	commits := resultSHA.FindAllString(summary+"\n"+string(report), -1)
	sort.Strings(commits)
	unique := []string{}
	for _, commit := range commits {
		if len(unique) == 0 || unique[len(unique)-1] != commit {
			unique = append(unique, commit)
		}
	}
	return ResultCard{TaskID: cardID, Spec: snapshot.Text, Report: string(report), Commits: unique,
		EvidenceVersion: resultHash([]string{summary, acceptance, string(report)}),
		Digest:          resultHash([]string{binding.SourceKey, cardID, snapshot.Text, string(report)})}, nil
}

func resultToken(card ResultCard, remote ResultRemote) string {
	return resultHash([]string{card.Digest, resultHash(remote)})
}

func readResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, store *resultStore) (ResultCard, ResultRemote, error) {
	card, err := ReadResultCard(root, repository, number, cardID)
	if err != nil {
		return card, ResultRemote{}, err
	}
	if provider == nil {
		return card, ResultRemote{}, resultError("result provider unavailable")
	}
	remote, err := provider.ReadResult(ctx, repository, number)
	if err != nil {
		return card, remote, err
	}
	if remote.SourceKey != store.SourceKey || remote.IssueID <= 0 || remote.ActorID <= 0 || remote.Revision == "" || remote.StateVersion == "" || (remote.State != "open" && remote.State != "closed") || (store.IssueID != 0 && store.IssueID != remote.IssueID) {
		return card, remote, resultError("remote identity or state mismatch")
	}
	store.IssueID = remote.IssueID
	// Recheck after network latency; stale state or binding never reaches writes.
	current, err := ReadResultCard(root, repository, number, cardID)
	if err != nil {
		return card, remote, err
	}
	if current.Digest != card.Digest {
		return card, remote, resultError("card changed during inspection")
	}
	return card, remote, nil
}
