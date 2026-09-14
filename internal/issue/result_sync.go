package issue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/board"
)

// InspectResult refreshes complete evidence and reconciles uncertain writes.
// An absent comment never clears an uncertain intent: GitHub does not provide
// an idempotency key or a proof that an interrupted request was not accepted.
func InspectResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string) (out ResultInspection, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		card, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		for _, record := range store.Records {
			if record.Status == "uncertain" {
				for _, comment := range remote.Comments {
					if comment.AuthorID == record.ActorID && comment.Body == record.Body && comment.ID > 0 {
						record.Status = "published"
						record.CommentID = comment.ID
						break
					}
				}
			}
			for i := range record.Decisions {
				d := &record.Decisions[i]
				if d.Status != "uncertain" {
					continue
				}
				if remote.State == "closed" {
					d.Status = "observed-closed"
				} else if remote.StateVersion != d.StateVersion {
					d.Status = "superseded"
				}
			}
		}
		if err := save(); err != nil {
			return err
		}
		out = ResultInspection{Schema: 1, SourceKey: store.SourceKey, Card: card, Remote: remote, Token: resultToken(card, remote), Records: store.Records}
		return nil
	})
	return
}

func proposalVersion(card ResultCard, proposal ResultProposal) (string, error) {
	acceptance, _ := board.SectionBody(card.Spec, "ACCEPTANCE_CRITERIA")
	count := 0
	for _, line := range strings.Split(acceptance, "\n") {
		if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") {
			count++
		}
	}
	if count == 0 || len(proposal.Outcomes) != count {
		return "", resultError("assess every acceptance criterion in order")
	}
	for _, outcome := range proposal.Outcomes {
		if outcome != "met" && outcome != "unmet" && outcome != "unknown" {
			return "", resultError("invalid criterion outcome")
		}
		if proposal.FullyResolved && outcome != "met" {
			return "", resultError("incomplete criteria cannot authorize closure")
		}
	}
	if proposal.FullyResolved {
		summary, _ := board.SectionBody(card.Spec, "SUMMARY")
		if strings.TrimSpace(proposal.ResolutionEvidence) == "" || !strings.Contains(summary+"\n"+card.Report, proposal.ResolutionEvidence) {
			return "", resultError("full resolution needs an exact completion-document quotation")
		}
	}
	checks := append([]ResultCheck{}, proposal.Checks...)
	for _, check := range checks {
		if strings.TrimSpace(check.Command) == "" || !strings.Contains(card.Spec+"\n"+card.Report, check.Command) || (check.Status != "pass" && check.Status != "fail" && check.Status != "not-run" && check.Status != "N/A") {
			return "", resultError("verification must cite a recorded command and pass/fail/not-run/N/A")
		}
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].Command < checks[j].Command })
	for i := 1; i < len(checks); i++ {
		if checks[i].Command == checks[i-1].Command {
			return "", resultError("duplicate verification command")
		}
	}
	return resultVersion(card.TaskID, card.Commits, proposal.Outcomes, proposal.FullyResolved), nil
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
// a stale observation cannot be used to publish or assert equivalence.
func ApplyResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, proposal ResultProposal) (out ResultRecord, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		card, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		if proposal.Token != resultToken(card, remote) {
			return resultError("inspection changed; inspect and assess again")
		}
		version, err := proposalVersion(card, proposal)
		if err != nil {
			return err
		}
		for _, record := range store.Records {
			if record.Status == "uncertain" {
				return resultError("publication uncertain; inspect remote before any further write")
			}
		}
		if record := store.Records[version]; record != nil && record.Status != "rejected" {
			// Reassessing current evidence may allow a close, without republishing
			// a language rewrite or new run log as another result comment.
			record.CardDigest = card.Digest
			record.EvidenceVersion = card.EvidenceVersion
			record.FullyResolved = proposal.FullyResolved
			record.ResolutionEvidence = proposal.ResolutionEvidence
			out = *record
			return save()
		}
		checks := append([]ResultCheck{}, proposal.Checks...)
		sort.Slice(checks, func(i, j int) bool { return checks[i].Command < checks[j].Command })
		record := &ResultRecord{EvidenceVersion: card.EvidenceVersion, Commits: card.Commits, Outcomes: proposal.Outcomes, Checks: checks, Version: version, CardDigest: card.Digest, ActorID: remote.ActorID, FullyResolved: proposal.FullyResolved, ResolutionEvidence: proposal.ResolutionEvidence}
		if previous := store.Records[version]; previous != nil {
			record.Failures = previous.Failures
		}
		if proposal.EquivalentCommentID > 0 {
			for _, comment := range remote.Comments {
				if comment.ID == proposal.EquivalentCommentID {
					record.Status = "equivalent"
					record.Body = comment.Body
					record.ActorID = comment.AuthorID
					record.CommentID = comment.ID
					store.Records[version] = record
					out = *record
					return save()
				}
			}
			return resultError("equivalent comment absent from current complete observation")
		}
		for _, previous := range store.Records {
			if (previous.Status == "published" || previous.Status == "equivalent") && previous.EvidenceVersion == card.EvidenceVersion && resultHash(previous.Commits) == resultHash(card.Commits) {
				return resultError("completion evidence unchanged; reference the existing result comment instead of publishing another assessment")
			}
		}
		if err := validateResultBody(proposal.Body); err != nil {
			return err
		}
		var nonce [24]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		record.Body = proposal.Body + "\n\n<!-- kander-result:" + hex.EncodeToString(nonce[:]) + " -->"
		record.Status = "uncertain"
		store.Records[version] = record
		if err := save(); err != nil {
			return err
		}
		comment, err := provider.PostResult(ctx, repository, number, record.Body)
		if err != nil {
			var rejected *ResultWriteRejection
			if errors.As(err, &rejected) {
				record.Status = "rejected"
				record.Failures = append(record.Failures, ResultFailure{Action: "comment", Detail: Sanitize(err.Error())})
				return errors.Join(err, save())
			}
			return err
		}
		if comment.ID <= 0 || comment.AuthorID != record.ActorID || comment.Body != record.Body {
			return resultError("publication response mismatch; outcome remains uncertain")
		}
		record.Status = "published"
		record.CommentID = comment.ID
		out = *record
		return save()
	})
	return
}

// DecideResult records explicit target-bound consent or refusal. An unchanged
// refusal is returned without prompting/writing again; reconsider is only for
// a user who actively requests another decision. Old consent is never reused.
func DecideResult(ctx context.Context, provider ResultProvider, root string, repository Repository, number int, cardID string, decision ResultDecision) (out ResultRecord, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, save func() error) error {
		card, remote, err := readResult(ctx, provider, root, repository, number, cardID, store)
		if err != nil {
			return err
		}
		if decision.Token != resultToken(card, remote) {
			return resultError("close target changed; inspect and request a new decision")
		}
		record := store.Records[decision.Version]
		if record == nil || (record.Status != "published" && record.Status != "equivalent") || record.CardDigest != card.Digest || !record.FullyResolved {
			return resultError("no current fully resolved assessment")
		}
		if remote.State == "closed" {
			out = *record
			return nil
		}
		for _, d := range record.Decisions {
			if d.StateVersion != remote.StateVersion {
				continue
			}
			if d.Status == "uncertain" {
				return resultError("close outcome uncertain; inspect remote, never retry blindly")
			}
			if d.Decision == "no" && !decision.Reconsider {
				if decision.Decision != "no" {
					return resultError("refusal recorded; explicit user reconsideration is required")
				}
				out = *record
				return nil
			}
			if d.Decision == "yes" && !(d.Status == "rejected" && decision.Reconsider) {
				return resultError("old close consent cannot be reused")
			}
		}
		if (decision.Decision != "yes" && decision.Decision != "no") || strings.TrimSpace(decision.UserReference) == "" {
			return resultError("explicit user decision and reference required")
		}
		d := ResultCloseRecord{StateVersion: remote.StateVersion, Decision: decision.Decision, UserReference: decision.UserReference, Status: "declined"}
		if decision.Decision == "yes" {
			d.Status = "uncertain"
		}
		record.Decisions = append(record.Decisions, d)
		if err := save(); err != nil {
			return err
		}
		if decision.Decision == "no" {
			out = *record
			return nil
		}
		if err := provider.CloseResult(ctx, repository, number); err != nil {
			var rejected *ResultWriteRejection
			if errors.As(err, &rejected) {
				record.Decisions[len(record.Decisions)-1].Status = "rejected"
				record.Failures = append(record.Failures, ResultFailure{Action: "close", Detail: Sanitize(err.Error())})
				return errors.Join(err, save())
			}
			return err
		}
		record.Decisions[len(record.Decisions)-1].Status = "closed"
		out = *record
		return save()
	})
	return
}
