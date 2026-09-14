package issue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
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
	if _, err := ReadResultCard(root, repository, number, options.CardID); err != nil {
		return TriageOutcome{}, err
	}
	return triageStarter(TriageLaunch{Root: root, Repository: repository, Number: number, CardID: options.CardID, JSONPath: path, MarkdownPath: path, Agent: options.Agent, Launcher: options.Launcher, ResultSync: true})
}
