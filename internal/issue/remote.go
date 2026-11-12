package issue

import (
	"context"
	"encoding/json"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

// RemoteIndex reads source bindings without requiring a local kanban directory.
func RemoteIndex(ctx context.Context, session *remote.Session) (Index, error) {
	raw, err := session.Project.IssueIndex(ctx)
	if err != nil {
		return nil, err
	}
	var index Index
	err = json.Unmarshal(raw, &index)
	return index, err
}

// ImportRemote fetches the provider snapshot on this host, then publishes it
// through the server's atomic source-key importer. Repeated imports preserve
// the original source bytes and never create another anchor.
func ImportRemote(ctx context.Context, session *remote.Session, provider IssueProvider, repository Repository, number int, options ImportOptions) (ImportResult, error) {
	if err := repository.Validate(); err != nil {
		return ImportResult{}, err
	}
	sourceKey, err := repository.IssueSourceKey(number)
	if err != nil {
		return ImportResult{}, err
	}
	index, err := RemoteIndex(ctx, session)
	if err != nil {
		return ImportResult{}, err
	}
	if card, exists := index[sourceKey]; exists {
		url, err := repository.IssueURL(number)
		return ImportResult{TaskID: card.TaskID, State: card.State, Path: card.Path, Existing: true, SourceKey: sourceKey, SourceURL: url, CommentsLoaded: card.CommentsLoaded}, err
	}
	if provider == nil {
		return ImportResult{}, NewError(ErrorCLIUnavailable, "import", "no issue provider is registered")
	}
	snapshot, err := provider.GetIssue(ctx, repository, number, options.Comments)
	if err != nil {
		return ImportResult{}, err
	}
	record, err := BuildImportSnapshot(snapshot)
	if err != nil {
		return ImportResult{}, err
	}
	if err := validateImportIdentity(record, repository, number); err != nil {
		return ImportResult{}, err
	}
	encoded, err := MarshalImportSnapshot(record)
	if err != nil {
		return ImportResult{}, err
	}
	raw, err := session.Project.ImportIssue(ctx, client.NewKey(), api.IssueImport{Snapshot: encoded, Type: options.Type, Language: options.Language, Large: options.Large})
	if err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	err = json.Unmarshal(raw, &result)
	return result, err
}

// StartRemoteTriage keeps source evidence on this host and resolves bindings
// on the server. A refinement downloads the original source attachments only.
func StartRemoteTriage(ctx context.Context, session *remote.Session, provider IssueProvider, projectDir string, repository Repository, number int, options TriageOptions, refine bool) (TriageOutcome, error) {
	if triageStarter == nil {
		return TriageOutcome{}, NewError(ErrorCLIUnavailable, "triage", "no takeover launcher is registered")
	}
	root, err := session.LocalDataRoot()
	if err != nil {
		return TriageOutcome{}, err
	}
	var card *board.TaskSummary
	if options.CardID != "" || refine {
		key, err := repository.IssueSourceKey(number)
		if err != nil {
			return TriageOutcome{}, err
		}
		index, err := RemoteIndex(ctx, session)
		if err != nil {
			return TriageOutcome{}, err
		}
		binding, exists := index[key]
		if !exists || binding.TaskID != options.CardID {
			return TriageOutcome{}, NewError(ErrorInvalidQuery, "card", Sanitize(options.CardID))
		}
		payload, err := session.TaskView(ctx, options.CardID)
		if err != nil {
			return TriageOutcome{}, err
		}
		card = &payload
		if refine && card.State != "backlog" {
			return TriageOutcome{}, NewError(ErrorInvalidQuery, "card", Sanitize(options.CardID))
		}
	}
	var evidence TriageEvidence
	if refine {
		directory, err := board.EnsureCacheDir(root, "refine", options.CardID)
		if err != nil {
			return TriageOutcome{}, err
		}
		for _, item := range []struct{ source, target string }{{SourceFileName, TriageJSONName}, {SourceMarkdownName, TriageMarkdownName}} {
			document, err := session.Project.Document(ctx, options.CardID, item.source)
			if err != nil {
				return TriageOutcome{}, err
			}
			target := filepath.Join(directory, item.target)
			if err := fs.WriteTextAtomic(root, target, document.Text, true); err != nil {
				return TriageOutcome{}, err
			}
			if item.target == TriageJSONName {
				evidence.JSONPath = target
			} else {
				evidence.MarkdownPath = target
			}
		}
	} else {
		evidence, err = PrepareTriage(ctx, provider, root, repository, number)
		if err != nil {
			return TriageOutcome{}, err
		}
	}
	return triageStarter(TriageLaunch{RefineCard: refine, Root: root, ProjectDir: projectDir, CardSnapshot: card, Repository: repository, Number: number, CardID: options.CardID, JSONPath: evidence.JSONPath, MarkdownPath: evidence.MarkdownPath, Agent: options.Agent, Launcher: options.Launcher})
}
