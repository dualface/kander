package launch

import (
	"fmt"
	"path/filepath"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
)

func resultAgentPrompt(request issue.TriageLaunch, paths config.InstallPaths) (string, error) {
	card, err := issue.ReadResultCard(request.Root, request.Repository, request.Number, request.CardID)
	if err != nil {
		return "", err
	}
	lang, err := resolvePromptLanguage(card.Spec, paths)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`Reconcile the completed result of %s, bound card %s.
%s%s
Read %s as current evidence. Before acting, read "Completed Result Reconciliation" in %s.
This session never authorizes task execution or card mutation. Startup authorizes adding one
missing result comment through the controlled command. It NEVER authorizes closing the issue;
closing needs separate explicit user consent for this current issue and result.
Use only %s issue result %d --repo %s/%s/%s --card %s with --action inspect,
--action apply, or --action decide as defined by the protocol.
`, triageTarget(request.Repository, request.Number), request.CardID, RuleLoadingInstruction(paths), promptLanguageDirective(lang), request.JSONPath, filepath.Join(paths.RulesDir, "KANDER-ISSUE-RULES.md"), commandName(paths), request.Number, request.Repository.Host, request.Repository.Owner, request.Repository.Name, request.CardID), nil
}
