package issue

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/board"
)

// StartRefinement starts contract completion using the bound card's immutable
// import attachments. It neither fetches another revision nor claims the card.
func StartRefinement(ctx context.Context, root string, repository Repository, number int, options TriageOptions) (TriageOutcome, error) {
	cardID := strings.TrimSpace(options.CardID)
	if cardID == "" {
		return TriageOutcome{}, NewError(ErrorInvalidQuery, "card", "missing card")
	}
	if err := validateTriageCard(root, repository, number, cardID); err != nil {
		return TriageOutcome{}, err
	}
	card, err := board.TaskPayloadContext(ctx, root, cardID)
	if err != nil {
		return TriageOutcome{}, err
	}
	if card.State != "backlog" {
		return TriageOutcome{}, NewError(ErrorInvalidQuery, "card", Sanitize(cardID))
	}
	if err := ctx.Err(); err != nil {
		return TriageOutcome{}, err
	}
	if triageStarter == nil {
		return TriageOutcome{}, NewError(ErrorCLIUnavailable, "refine", "no refinement launcher is registered")
	}
	return triageStarter(TriageLaunch{
		RefineCard: true, Root: root, Repository: repository, Number: number,
		CardID: cardID, JSONPath: filepath.Join(root, card.State, card.TaskID, SourceFileName),
		MarkdownPath: filepath.Join(root, card.State, card.TaskID, SourceMarkdownName),
		Agent:        strings.TrimSpace(options.Agent), Launcher: strings.TrimSpace(options.Launcher),
	})
}
