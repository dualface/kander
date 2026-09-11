package issue

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

// The two local evidence files one takeover session is pointed at. The JSON
// file is the machine-readable snapshot; the Markdown file renders the same
// record for reading. Both are rebuilt on every takeover attempt.
const (
	TriageJSONName     = "issue.json"
	TriageMarkdownName = "issue.md"
)

// TriageEvidence describes the refreshed local evidence of one issue. The
// launcher only needs the paths; the files themselves are the evidence.
type TriageEvidence struct {
	Directory    string
	JSONPath     string
	MarkdownPath string
}

// TriageOptions are the caller choices of one takeover.
type TriageOptions struct {
	// CardID binds the session to a card that already carries the issue; the
	// agent completes that card instead of importing a duplicate.
	CardID string
	// Agent and Launcher override the configured defaults; empty uses the
	// configuration.
	Agent    string
	Launcher string
}

// TriageLaunch is the input of one registered takeover starter. It carries the
// confirmed identity, the local evidence paths and the caller overrides; remote
// text never enters it, so the launcher can build its prompt from paths only.
type TriageLaunch struct {
	Root         string
	Repository   Repository
	Number       int
	CardID       string
	EvidenceDir  string
	JSONPath     string
	MarkdownPath string
	Agent        string
	Launcher     string
}

// TriageOutcome reports one started takeover session.
type TriageOutcome struct {
	Agent    string
	Launcher string
	Address  string
	Warnings []string
}

// TriageStarter starts the takeover session of one prepared issue. The launch
// layer registers it; the issue package never imports the launcher, so the
// command front end stays provider-neutral and testable.
type TriageStarter func(TriageLaunch) (TriageOutcome, error)

// triageStarter is assigned by the launch package at init; while unset, the
// `issue triage` command reports that no takeover launcher is registered.
var triageStarter TriageStarter

// SetTriageStarter wires the takeover starter. It is called by the launch
// package at init so every binary that can start agents also supports
// `kander issue triage`.
func SetTriageStarter(starter TriageStarter) {
	triageStarter = starter
}

// TriageDirectoryName returns the readable directory name of one issue's
// evidence: <owner>-<repo>-<number> reduced to the task-ID alphabet, so the
// name is safe on every platform and contains no remote text beyond the
// validated identity.
func TriageDirectoryName(repository Repository, number int) string {
	return slugSegment(repository.Owner) + "-" + slugSegment(repository.Name) + "-" + strconv.Itoa(number)
}

// PrepareTriage fetches the issue again and writes the takeover evidence below
// the private cache root:
//
//	<board>/.kander/caches/triage/<owner>-<repo>-<number>/issue.json
//	<board>/.kander/caches/triage/<owner>-<repo>-<number>/issue.md
//
// The snapshot is the same bounded, sanitized record the card attachments use;
// an issue that exceeds the import bounds is rejected instead of truncated. The
// evidence is rewritten on every attempt, so a takeover never works from a
// stale copy.
func PrepareTriage(ctx context.Context, provider IssueProvider, root string, repository Repository, number int) (TriageEvidence, error) {
	if provider == nil {
		return TriageEvidence{}, NewError(ErrorCLIUnavailable, "triage", "no issue provider is registered")
	}
	if err := repository.Validate(); err != nil {
		return TriageEvidence{}, err
	}
	if number <= 0 {
		return TriageEvidence{}, NewError(ErrorInvalidQuery, "number", strconv.Itoa(number))
	}
	snapshot, err := provider.GetIssue(ctx, repository, number, true)
	if err != nil {
		return TriageEvidence{}, err
	}
	record, err := BuildImportSnapshot(snapshot)
	if err != nil {
		return TriageEvidence{}, err
	}
	if err := validateImportIdentity(record, repository, number); err != nil {
		return TriageEvidence{}, err
	}
	encoded, err := MarshalImportSnapshot(record)
	if err != nil {
		return TriageEvidence{}, err
	}
	directory, err := board.EnsureCacheDir(root, "triage", TriageDirectoryName(repository, number))
	if err != nil {
		return TriageEvidence{}, err
	}
	markdownPath := filepath.Join(directory, TriageMarkdownName)
	if err := fs.WriteTextAtomic(root, markdownPath, RenderImportMarkdown(record), true); err != nil {
		return TriageEvidence{}, err
	}
	jsonPath := filepath.Join(directory, TriageJSONName)
	// The machine-readable record is written last, so its presence in a session
	// means both evidence files of this attempt exist.
	if err := fs.WriteTextAtomic(root, jsonPath, string(encoded), true); err != nil {
		return TriageEvidence{}, err
	}
	return TriageEvidence{
		Directory:    directory,
		JSONPath:     jsonPath,
		MarkdownPath: markdownPath,
	}, nil
}

// StartTriage prepares the evidence of one issue and starts the takeover
// session through the registered starter. It is the single path shared by the
// `kander issue triage` command and the TUI; the starter is the only part that
// starts a process, and it never reads or writes the board.
func StartTriage(ctx context.Context, provider IssueProvider, root string, repository Repository, number int, options TriageOptions) (TriageOutcome, error) {
	cardID := strings.TrimSpace(options.CardID)
	if err := validateTriageCard(root, repository, number, cardID); err != nil {
		return TriageOutcome{}, err
	}
	evidence, err := PrepareTriage(ctx, provider, root, repository, number)
	if err != nil {
		return TriageOutcome{}, err
	}
	starter := triageStarter
	if starter == nil {
		return TriageOutcome{}, NewError(ErrorCLIUnavailable, "triage", "no takeover launcher is registered")
	}
	return starter(TriageLaunch{
		Root:         root,
		Repository:   repository,
		Number:       number,
		CardID:       cardID,
		EvidenceDir:  evidence.Directory,
		JSONPath:     evidence.JSONPath,
		MarkdownPath: evidence.MarkdownPath,
		Agent:        strings.TrimSpace(options.Agent),
		Launcher:     strings.TrimSpace(options.Launcher),
	})
}

// TriageError renders one triage failure: a structured issue error becomes its
// localized sentence, and every other failure keeps its own message (launch
// failures are already localized and must not be reworded as GitHub errors).
func TriageError(err error) string {
	if err == nil {
		return ""
	}
	var structured *Error
	if errors.As(err, &structured) {
		return Message(err)
	}
	return Sanitize(err.Error())
}

// validateTriageCard accepts only a card that is actually bound to the issue.
// A wrong or unbound task id would send the takeover agent to a card that
// carries another issue, so it is rejected before any evidence is written.
func validateTriageCard(root string, repository Repository, number int, cardID string) error {
	if cardID == "" {
		return nil
	}
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return err
	}
	index, err := LoadIndex(root)
	if err != nil {
		return err
	}
	local, ok := index[key]
	if !ok || local.TaskID != cardID {
		return NewError(ErrorInvalidQuery, "card", Sanitize(cardID))
	}
	return nil
}
