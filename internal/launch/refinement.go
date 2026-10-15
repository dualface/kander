package launch

import (
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/process"
)

// refinementAgentPrompt grants only contract editing. Read the live card so its
// frozen language takes precedence over the current configuration.
func refinementAgentPrompt(request issue.TriageLaunch, paths config.InstallPaths) (string, error) {
	card, err := board.TaskPayload(request.Root, request.CardID)
	if err != nil {
		return "", err
	}
	if card.State != "backlog" {
		return "", issue.NewError(issue.ErrorInvalidQuery, "card", issue.Sanitize(request.CardID))
	}
	language, err := resolvePromptLanguage(card.Document, paths)
	if err != nil {
		return "", err
	}
	preamble, err := rulesAndLanguage(paths, language)
	if err != nil {
		return "", err
	}
	return render(paths, "refine.md", map[string]any{
		"Preamble": preamble, "CardID": request.CardID,
		"IssueRules":   rulesPath(paths, "KANDER-ISSUE-RULES.md"),
		"JSONPath":     process.AgentPath(request.JSONPath),
		"MarkdownPath": process.AgentPath(request.MarkdownPath),
		"Command":      commandName(paths),
	})
}
