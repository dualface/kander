package issue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/board"
	rk "github.com/dualface/kander/internal/statemachine/issueresult"
	dk "github.com/dualface/kander/internal/statemachine/issueresultdecision"
)

// InspectResult refreshes complete evidence and reconciles uncertain writes.
// An absent comment never clears an uncertain intent: GitHub does not provide
// an idempotency key or a proof that an interrupted request was not accepted.
// The issue result kernels decide each change of status.
func InspectResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string) (out ResultInspection, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		work, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		ids := newIdentities()
		for version, record := range store.Records {
			if record.Status == "uncertain" {
				for _, comment := range remote.Comments {
					if comment.AuthorID == record.ActorID && comment.Body == record.Body && comment.ID > 0 {
						next, ok := resultStep(projectRecord(*store, version, ids, work), rk.Reconcile{Found: true, Ncid: ids.comment(comment.ID)})
						if !ok {
							return errResultInconsistent()
						}
						record.Status = statusOf(next.St)
						record.CommentID = comment.ID
						break
					}
				}
			}
			for i := range record.Decisions {
				d := &record.Decisions[i]
				// The kernel settles an uncertain consent. Any other decision, and an
				// uncertain one on an open remote with the same state version, stays.
				if next, ok := decisionStep(dk.State{Ds: dstatOf(d.Status)}, dk.Reconcile{Closed: remote.State == "closed", Same: remote.StateVersion == d.StateVersion}); ok {
					d.Status = decisionStatusOf(next.Ds)
				}
			}
		}
		if err := save(); err != nil {
			return err
		}
		out = ResultInspection{Schema: 1, SourceKey: store.SourceKey, Card: work.Anchor(), Cards: work.Cards, Excluded: work.Excluded, Remote: remote, Token: resultToken(work.Digest, remote), Records: store.Records}
		return nil
	})
	return
}

func proposalVersion(work ResultWork, proposal ResultProposal) (string, error) {
	count := 0
	var documents, completion []string
	for _, card := range work.Cards {
		acceptance, _ := board.SectionBody(card.Spec, "ACCEPTANCE_CRITERIA")
		for _, line := range strings.Split(acceptance, "\n") {
			if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") {
				count++
			}
		}
		summary, _ := board.SectionBody(card.Spec, "SUMMARY")
		documents = append(documents, card.Spec, card.Report)
		completion = append(completion, summary, card.Report)
	}
	if count == 0 || len(proposal.Outcomes) != count {
		return "", resultError("assess every acceptance criterion of every card in order")
	}
	for _, outcome := range proposal.Outcomes {
		if outcome != "met" && outcome != "unmet" && outcome != "unknown" {
			return "", resultError("invalid criterion outcome")
		}
		if proposal.FullyResolved && outcome != "met" {
			return "", resultError("incomplete criteria cannot authorize closure")
		}
	}
	// Evidence may come from any member: a sibling can carry the verification
	// or the completion statement for the whole issue.
	if proposal.FullyResolved {
		if strings.TrimSpace(proposal.ResolutionEvidence) == "" || !containsInAny(completion, proposal.ResolutionEvidence) {
			return "", resultError("full resolution needs an exact completion-document quotation")
		}
	}
	checks := append([]ResultCheck{}, proposal.Checks...)
	for _, check := range checks {
		if strings.TrimSpace(check.Command) == "" || !containsInAny(documents, check.Command) || (check.Status != "pass" && check.Status != "fail" && check.Status != "not-run" && check.Status != "N/A") {
			return "", resultError("verification must cite a recorded command and pass/fail/not-run/N/A")
		}
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].Command < checks[j].Command })
	for i := 1; i < len(checks); i++ {
		if checks[i].Command == checks[i-1].Command {
			return "", resultError("duplicate verification command")
		}
	}
	return resultVersion(work.TaskKey, work.Commits, proposal.Outcomes, proposal.FullyResolved), nil
}

// containsInAny matches within one document, never across a document boundary.
func containsInAny(documents []string, text string) bool {
	for _, document := range documents {
		if strings.Contains(document, text) {
			return true
		}
	}
	return false
}

func resultVersion(task string, commits, outcomes []string, resolved bool) string {
	return resultHash(struct {
		Task              string
		Commits, Outcomes []string
		Resolved          bool
	}{task, commits, outcomes, resolved})
}

var resultPrivateText = regexp.MustCompile(`(?i)(?:(?:^|[^a-z0-9])[a-z]:[\\/](?:[^/\\]|$)|(?:^|[\s(\[<:=\x60"\x27])/(?:[^/\s])|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|(?:herdr|tmux):|\\\\|<!--\s*kander-result:)`)

func validateResultBody(body string) error {
	if strings.TrimSpace(body) == "" || len(body) > 16000 || !utf8.ValidString(body) || SanitizeRemoteText(body) != body || redact(body) != body || resultPrivateText.MatchString(body) {
		return resultError("result text is empty, oversized, or contains private/control data")
	}
	return nil
}

// ApplyResult permits one pending or accepted publication per result version.
// A definite provider rejection may be retried after fresh inspection.
// Semantic equivalence is assessed by the agent against the inspected comments;
// a stale observation cannot be used to publish or assert equivalence. The issue
// result kernels decide what is written and what is refused; the checks run in
// the order they always had, each asked of the kernel with every other fact
// holding.
func ApplyResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, proposal ResultProposal) (out ResultRecord, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		work, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		ids := newIdentities()
		if proposal.Token != resultToken(work.Digest, remote) {
			return resultError("inspection changed; inspect and assess again")
		}
		version, err := proposalVersion(work, proposal)
		if err != nil {
			return err
		}
		state := projectRecord(*store, version, ids, work)
		met := allMet(proposal.Outcomes)
		uncertain := resultError("publication uncertain; inspect remote before any further write")
		if record := store.Records[version]; record != nil && record.Status != "rejected" {
			// Reassessing current evidence may allow a close, without republishing
			// a language rewrite or new run log as another result comment.
			if _, ok := resultStep(state, rk.Reassess{}); !ok {
				return uncertain
			}
			record.CardDigest = work.Digest
			record.EvidenceVersion = work.EvidenceVersion
			record.FullyResolved = proposal.FullyResolved
			record.ResolutionEvidence = proposal.ResolutionEvidence
			out = *record
			return save()
		}
		// No record may be uncertain before a new one is written.
		if _, ok := resultStep(state, rk.Intent{Nfull: proposal.FullyResolved, Nmet: met, BodyOk: true, Nbod: 1, Nchk: 1, Nsound: true}); !ok {
			return uncertain
		}
		checks := append([]ResultCheck{}, proposal.Checks...)
		sort.Slice(checks, func(i, j int) bool { return checks[i].Command < checks[j].Command })
		record := &ResultRecord{TaskIDs: work.taskIDs(), EvidenceVersion: work.EvidenceVersion, Commits: work.Commits, Outcomes: proposal.Outcomes, Checks: checks, Version: version, CardDigest: work.Digest, ActorID: remote.ActorID, FullyResolved: proposal.FullyResolved, ResolutionEvidence: proposal.ResolutionEvidence}
		if previous := store.Records[version]; previous != nil {
			record.Failures = previous.Failures
		}
		if proposal.EquivalentCommentID > 0 {
			var found *ResultComment
			for i, comment := range remote.Comments {
				if comment.ID == proposal.EquivalentCommentID {
					found = &remote.Comments[i]
					break
				}
			}
			if found != nil {
				record.Body, record.ActorID, record.CommentID = found.Body, found.AuthorID, found.ID
			}
			next, ok := resultStep(state, rk.Equivalent{Nfull: proposal.FullyResolved, Nmet: met, Found: found != nil,
				Nbod: ids.body(record.Body, record.ActorID), Ncid: ids.comment(record.CommentID), Nchk: ids.checks(record.Checks),
				Nsound: soundRecord(record, store.TaskID)})
			if !ok {
				if found == nil {
					return resultError("equivalent comment absent from current complete observation")
				}
				return errResultInconsistent()
			}
			record.Status = statusOf(next.St)
			store.Records[version] = record
			out = *record
			return save()
		}
		duplicate := false
		for _, previous := range store.Records {
			if (previous.Status == "published" || previous.Status == "equivalent") && previous.EvidenceVersion == work.EvidenceVersion && resultHash(previous.Commits) == resultHash(work.Commits) {
				duplicate = true
				break
			}
		}
		bodyErr := validateResultBody(proposal.Body)
		if !duplicate && bodyErr == nil {
			var nonce [24]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return err
			}
			record.Body = proposal.Body + "\n\n<!-- kander-result:" + hex.EncodeToString(nonce[:]) + " -->"
		}
		next, ok := resultStep(state, rk.Intent{Nfull: proposal.FullyResolved, Nmet: met, Dup: duplicate, BodyOk: bodyErr == nil,
			Nbod: ids.body(record.Body, record.ActorID), Nchk: ids.checks(record.Checks), Nsound: soundRecord(record, store.TaskID)})
		if !ok {
			if duplicate {
				return resultError("completion evidence unchanged; reference the existing result comment instead of publishing another assessment")
			}
			if bodyErr != nil {
				return bodyErr
			}
			return errResultInconsistent()
		}
		record.Status = statusOf(next.St)
		store.Records[version] = record
		if err := save(); err != nil {
			return err
		}
		comment, err := provider.PostResult(ctx, repository, number, record.Body)
		if err != nil {
			var rejected *ResultWriteRejection
			if errors.As(err, &rejected) {
				failures := append(slices.Clone(record.Failures), ResultFailure{Action: "comment", Detail: Sanitize(err.Error())})
				next, ok := resultStep(projectRecord(*store, version, ids, work), rk.Reject{Nfh: ids.failures(failures)})
				if !ok {
					return errors.Join(err, errResultInconsistent())
				}
				record.Status = statusOf(next.St)
				record.Failures = failures
				return errors.Join(err, save())
			}
			return err
		}
		next, ok = resultStep(projectRecord(*store, version, ids, work), rk.Publish{Ok: comment.ID > 0 && comment.AuthorID == record.ActorID && comment.Body == record.Body, Ncid: ids.comment(comment.ID)})
		if !ok {
			return resultError("publication response mismatch; outcome remains uncertain")
		}
		record.Status = statusOf(next.St)
		record.CommentID = comment.ID
		out = *record
		return save()
	})
	return
}

// DecideResult records explicit target-bound consent or refusal. An unchanged
// refusal is returned without prompting/writing again; reconsider is only for
// a user who actively requests another decision. Old consent is never reused.
// The issue result kernels decide each step: the record gate, a closed remote,
// the scan of the earlier decisions of the same state version, the decision
// fields, and the outcome of the close request.
func DecideResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, decision ResultDecision) (out ResultRecord, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		work, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		ids := newIdentities()
		if decision.Token != resultToken(work.Digest, remote) {
			return resultError("close target changed; inspect and request a new decision")
		}
		record := store.Records[decision.Version]
		state := projectRecord(*store, decision.Version, ids, work)
		digestOk := record != nil && record.CardDigest == work.Digest
		// The gate: a record that is published or equivalent, of the current work, and fully resolved.
		if _, ok := resultStep(state, rk.Keep{DigestOk: digestOk, Closed: true}); !ok {
			return resultError("no current fully resolved assessment")
		}
		if remote.State == "closed" {
			out = *record
			return nil
		}
		scan := scanDecisions(record, remote.StateVersion, decision)
		if _, ok := resultStep(state, rk.Keep{DigestOk: digestOk, Scan: scan}); ok {
			out = *record
			return nil
		}
		switch scan {
		case rk.ScanUncertain:
			return resultError("close outcome uncertain; inspect remote, never retry blindly")
		case rk.ScanRefusal:
			return resultError("refusal recorded; explicit user reconsideration is required")
		case rk.ScanConsent:
			return resultError("old close consent cannot be reused")
		}
		fieldsOk := (decision.Decision == "yes" || decision.Decision == "no") && strings.TrimSpace(decision.UserReference) != ""
		if _, ok := resultStep(state, rk.Decide{DigestOk: digestOk, Scan: scan, FieldsOk: fieldsOk}); !ok {
			return resultError("explicit user decision and reference required")
		}
		var appended dk.State
		if decision.Decision == "yes" {
			appended, _ = decisionStep(dk.Initial(decisionKernelParams), dk.AppendYes{Ndid: ids.decision(ResultCloseRecord{StateVersion: remote.StateVersion, Decision: decision.Decision, UserReference: decision.UserReference})})
		} else {
			appended, _ = decisionStep(dk.Initial(decisionKernelParams), dk.AppendNo{Ndid: ids.decision(ResultCloseRecord{StateVersion: remote.StateVersion, Decision: decision.Decision, UserReference: decision.UserReference})})
		}
		record.Decisions = append(record.Decisions, ResultCloseRecord{StateVersion: remote.StateVersion, Decision: decision.Decision, UserReference: decision.UserReference, Status: decisionStatusOf(appended.Ds)})
		if err := save(); err != nil {
			return err
		}
		if decision.Decision == "no" {
			out = *record
			return nil
		}
		last := &record.Decisions[len(record.Decisions)-1]
		if err := provider.CloseResult(ctx, repository, number); err != nil {
			var rejected *ResultWriteRejection
			if errors.As(err, &rejected) {
				closed, ok := decisionStep(dk.State{Ds: dstatOf(last.Status)}, dk.CloseRejected{})
				failures := append(slices.Clone(record.Failures), ResultFailure{Action: "close", Detail: Sanitize(err.Error())})
				_, okFail := resultStep(projectRecord(*store, decision.Version, ids, work), rk.FailClose{Nfh: ids.failures(failures)})
				if !ok || !okFail {
					return errors.Join(err, errResultInconsistent())
				}
				last.Status = decisionStatusOf(closed.Ds)
				record.Failures = failures
				return errors.Join(err, save())
			}
			return err
		}
		closed, ok := decisionStep(dk.State{Ds: dstatOf(last.Status)}, dk.CloseDone{})
		if !ok {
			return errResultInconsistent()
		}
		last.Status = decisionStatusOf(closed.Ds)
		out = *record
		return save()
	})
	return
}
