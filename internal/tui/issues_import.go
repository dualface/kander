package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/dualface/kander/internal/issue"
)

// Import and launch settle together even if the user closes the overlay. The
// origin pointer prevents a reopened overlay from receiving the old receipt.
type issuesImportResult struct {
	seq        uint64
	origin     *issuesState
	repository issue.Repository
	number     int
	result     issue.ImportResult
	index      issue.Index
	outcome    issue.TriageOutcome
	err        error
	launchErr  error
	indexErr   error
	launched   bool
}

func (a *App) issuesImportSelected() {
	if _, bound := a.issuesSelectedBound(); bound {
		return
	}
	if a.Issues == nil || a.issuesImporting {
		return
	}
	if a.offerBoardInit(boardInitImport) {
		return
	}
	if a.ImportIssue == nil {
		a.issuesSetNotice(a.Context.IssuesImportFailed)
		return
	}
	a.queueIssueRefinement(issue.ImportResult{})
}

func (a *App) issuesRefineSelected(card issue.LocalCard) {
	if card.State != "backlog" {
		return
	}
	a.queueIssueRefinement(issue.ImportResult{TaskID: card.TaskID, State: card.State, Path: card.Path, Existing: true})
}

// queueIssueRefinement captures every target and callback on the UI thread.
// A fresh import starts only its newly created card; an existing binding needs
// the explicit refinement action, so racing imports do not auto-start twice.
func (a *App) queueIssueRefinement(existing issue.ImportResult) {
	st := a.Issues
	if st == nil || a.issuesImporting {
		return
	}
	number, repository := a.issuesSelectedNumber(), a.issuesRepository()
	if number <= 0 || repository == nil {
		a.issuesSetNotice(a.Context.IssuesNoTarget)
		return
	}
	importer, loader, preview, refine := a.ImportIssue, a.ImportIndex, a.PrepareTriage, a.RefineIssue
	resolved := *repository
	a.issuesImportSeq++
	seq := a.issuesImportSeq
	a.issuesImporting, st.importing = true, true
	a.issuesSetNotice(t("tui.issues_import_refining"))
	a.pendingWork = func() any {
		ctx, cancel := context.WithTimeout(context.Background(), issuesTriageTimeout)
		defer cancel()
		out := issuesImportResult{seq: seq, origin: st, repository: resolved, number: number, result: existing}
		retry := existing.TaskID != ""
		if !retry {
			out.result, out.err = importer(ctx, resolved, number, issue.ImportOptions{Comments: true})
			if out.err != nil {
				return out
			}
		}
		// Read the binding before launch so a failure still offers the retry action.
		if loader != nil {
			out.index, out.indexErr = loader()
		}
		if !retry && out.result.Existing {
			return out
		}
		if refine == nil || preview == nil {
			out.launchErr = errors.New(t("tui.issues_takeover_unavailable"))
			return out
		}
		settings, err := preview()
		if err != nil {
			out.launchErr = err
			return out
		}
		if !backgroundStartLauncher(settings.Launcher) {
			out.launchErr = errors.New(t("tui.start_use_cli", settings.Launcher))
			return out
		}
		if err := ctx.Err(); err != nil {
			out.launchErr = err
			return out
		}
		out.outcome, out.launchErr = refine(ctx, resolved, number, issue.TriageOptions{
			CardID: out.result.TaskID, Agent: settings.Agent, Launcher: settings.Launcher,
		})
		out.launched = out.launchErr == nil
		return out
	}
}

func (a *App) applyIssuesImport(result issuesImportResult) {
	a.invalidateBoardReads()
	if id := strings.TrimSpace(result.result.TaskID); id == "" {
		a.invalidateSummaries()
	} else {
		a.invalidateSummaries(id)
	}
	a.requestBoardRefresh(true)
	if result.seq != a.issuesImportSeq {
		return
	}
	a.issuesImporting = false
	// Older list and index scans must not erase the committed binding below.
	a.issuesIndexSeq++
	st := a.Issues
	sameOrigin := st != nil && (result.origin == nil || result.origin == st)
	if st != nil {
		st.importing = false
	}
	repository := a.issuesRepository()
	sameRepository := st != nil && repository != nil && *repository == result.repository
	if sameRepository {
		// A reopened overlay keeps its own list/index scan, but still learns the
		// committed binding immediately so s cannot select the unbound action.
		if sameOrigin && result.index != nil {
			st.index = result.index
			st.indexRefreshedAt = a.Now()
		}
		if result.err == nil && result.result.TaskID != "" {
			if key, err := result.repository.IssueSourceKey(result.number); err == nil {
				if st.index == nil {
					st.index = issue.Index{}
				}
				if _, exists := st.index[key]; !exists {
					st.index[key] = issue.LocalCard{TaskID: result.result.TaskID, State: result.result.State, Path: result.result.Path, CommentsLoaded: result.result.CommentsLoaded}
				}
			}
		}
	}
	if !sameOrigin || !sameRepository || result.number != a.issuesSelectedNumber() {
		// Navigation changes where the receipt belongs, not whether it is shown.
		a.showFocusNotice(a.issueRefinementReceipt(result))
		return
	}
	a.issuesSetNotice(a.issueRefinementReceipt(result))
}

func (a *App) issueRefinementReceipt(result issuesImportResult) string {
	if result.err != nil {
		return a.Context.IssuesImportFailed + ": " + issue.Message(result.err)
	}
	var message string
	switch {
	case result.launchErr != nil:
		key := "tui.issues_refine_failed"
		if a.Issues == nil {
			key = "tui.issues_refine_failed_board"
		}
		message = t(key, result.result.TaskID, issue.TriageError(result.launchErr))
	case result.launched:
		message = t("tui.issues_refine_started", result.result.TaskID, result.outcome.Agent, result.outcome.Address)
	default:
		message = t("tui.issues_import_existing", result.result.TaskID)
	}
	if result.indexErr != nil {
		message += "\n" + t("tui.issues_load_failed") + ": " + issue.TriageError(result.indexErr)
	}
	if len(result.outcome.Warnings) > 0 {
		message += "\n" + strings.Join(result.outcome.Warnings, "\n")
	}
	return message
}
