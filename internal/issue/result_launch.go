package issue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/remote"
)

// StartResult starts a distinct read/remote-result session, never a card claim.
// Each launch owns immutable evidence paths, so concurrent sessions cannot
// overwrite the snapshot another agent is about to read.
func StartResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, options TriageOptions) (TriageOutcome, error) {
	if triageStarter == nil {
		return TriageOutcome{}, resultError("result session launcher unavailable")
	}
	inspection, err := InspectResult(ctx, provider, root, repository, number, options.CardID)
	if err != nil {
		return TriageOutcome{}, err
	}
	var journal *remote.ActionJournal
	if backend, ok := remoteResultOf(ctx); ok {
		var fresh bool
		journal, fresh, err = backend.session.BeginAction("issue-result-start", struct{ Source, Task, Token, Agent, Launcher, Operation string }{inspection.SourceKey, options.CardID, inspection.Token, options.Agent, options.Launcher, options.OperationID})
		if err != nil {
			return TriageOutcome{}, err
		}
		defer journal.Close()
		if !fresh && journal.Value.Phase != "prepared" && journal.Value.Phase != "session-ready" {
			var outcome TriageOutcome
			if journal.Value.Phase == "complete" && json.Unmarshal(journal.Value.Result, &outcome) == nil {
				return outcome, nil
			}
			return outcome, resultError("result launch outcome requires inspection: " + journal.Path)
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return TriageOutcome{}, err
	}
	dir, err := board.EnsureCacheDir(root, "result-sessions", hex.EncodeToString(nonce[:]))
	if err != nil {
		return TriageOutcome{}, err
	}
	data, err := json.MarshalIndent(inspection, "", "  ")
	if err != nil {
		return TriageOutcome{}, err
	}
	path := filepath.Join(dir, "result.json")
	if err := fs.WriteTextAtomic(root, path, string(data)+"\n", true); err != nil {
		return TriageOutcome{}, err
	}
	current, err := readResultWork(ctx, root, repository, number, options.CardID)
	if err != nil {
		return TriageOutcome{}, err
	}
	inspected := ResultWork{Cards: inspection.Cards}
	inspected.combine()
	if current.Digest != inspected.Digest {
		return TriageOutcome{}, resultError("result work changed before session launch")
	}
	projectDir := ""
	if backend, ok := remoteResultOf(ctx); ok {
		projectDir = backend.projectDir
	}
	if journal != nil {
		if journal.Value.Phase != "session-ready" {
			journal.Value.Phase = "starting"
		}
		if err := journal.Save(); err != nil {
			return TriageOutcome{}, err
		}
	}
	card := &board.TaskSummary{TaskID: options.CardID, State: inspection.Card.State, Document: inspection.Card.Spec}
	outcome, err := triageStarter(TriageLaunch{Action: journal, CardSnapshot: card, Root: root, ProjectDir: projectDir, Repository: repository, Number: number, CardID: options.CardID, JSONPath: path, MarkdownPath: path, Agent: options.Agent, Launcher: options.Launcher, ResultSync: true})
	if err != nil {
		return outcome, err
	}
	if journal != nil {
		journal.Value.Result, err = json.Marshal(outcome)
		if err == nil {
			journal.Value.Window, journal.Value.Phase = outcome.Address, "complete"
			err = journal.Save()
		}
	}
	return outcome, err
}
