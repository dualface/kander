package launch

import (
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/process"
)

func resultAgentPrompt(request issue.TriageLaunch, paths config.InstallPaths) (string, string, error) {
	work, err := issue.ReadResultWork(request.Root, request.Repository, request.Number, request.CardID)
	if err != nil {
		return "", "", err
	}
	lang, err := resolvePromptLanguage(work.Anchor().Spec, paths)
	if err != nil {
		return "", "", err
	}
	preamble, err := rulesAndLanguage(paths, lang)
	if err != nil {
		return "", "", err
	}
	body, err := render(paths, "result.md", map[string]any{
		"Target":     triageTarget(request.Repository, request.Number),
		"CardID":     request.CardID,
		"Preamble":   preamble,
		"JSONPath":   process.AgentPath(request.JSONPath),
		"IssueRules": rulesPath(paths, "KANDER-ISSUE-RULES.md"),
		"Command":    commandName(paths),
		"Number":     request.Number,
		"Host":       request.Repository.Host,
		"Owner":      request.Repository.Owner,
		"Repo":       request.Repository.Name,
	})
	return body, lang, err
}
