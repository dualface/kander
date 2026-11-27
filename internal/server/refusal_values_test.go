package server_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
)

// refusal requires the API error of a refused request: its code, its HTTP status,
// its board code, and every part of its message.
func refusal(t *testing.T, err error, code string, status int, boardCode string, parts ...string) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("want an API error, got %v", err)
	}
	if e.Code != code || e.Status != status || e.BoardCode != boardCode {
		t.Fatalf("want %s/%d/%q, got %s/%d/%q: %s", code, status, boardCode, e.Code, e.Status, e.BoardCode, e.Message)
	}
	for _, part := range parts {
		if !strings.Contains(e.Message, part) {
			t.Fatalf("%q missing from %q", part, e.Message)
		}
	}
}

// R21: the three revision conflicts of the request routes write their values after
// the text they always had; the API code and the HTTP status do not change.
func TestRequestConflictsNameTheirValues(t *testing.T) {
	english(t)
	t.Run("a proposal bound to another card version", func(t *testing.T) {
		h := newPreparedHolder(t, "refusal-values-version")
		input := h.input("refusal-version")
		proposal := h.propose(t, "proposal", input, 120*time.Second)
		// The card moves after the proposal was made; a preparation of other material
		// at the new revision is refused because the proposal binds the old one.
		current, err := h.holder.Show(t.Context(), h.task)
		if err != nil {
			t.Fatal(err)
		}
		moved, err := h.holder.PutDocument(t.Context(), "moves-card", h.task, api.Document{Path: "notes.md", Text: "the card moved", ExpectedRevision: current.Revision}, &h.proof)
		if err != nil {
			t.Fatal(err)
		}
		changed := *proposal.Dispatch
		changed.Message = "Different proposed work"
		_, err = h.holder.DispatchAction(t.Context(), "prepare", h.task, "prepare", api.DispatchCommand{Input: &changed, CardRevision: moved.Revision}, h.proof)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "",
			"dispatch proposal names another card version", "the proposal has card_revision "+num(proposal.CardRevision), "this request has card_revision "+num(moved.Revision), "kander request show "+h.task+" "+proposal.ID)
	})

	t.Run("a request that does not bind the inspected card", func(t *testing.T) {
		k := newKernelFixture(t, "refusal-values-bind")
		gen, card := k.grant.Claim.Generation, k.grant.Task.Revision
		intent := board.DispatchInput{ID: "bind-proposal", TaskID: k.task, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect the synchronized commit"}
		input := api.NewCollabRequest{Kind: api.RequestSync, Message: intent.Message, Dispatch: &intent, CardRevision: card + 4, TargetGeneration: gen, AcceptWithin: api.DurationOf(120 * time.Second)}
		_, err := k.sender.CreateRequest(t.Context(), "moved", k.task, input)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "",
			"dispatch request must bind the inspected card version", "the request has card_revision "+num(card+4), "the card is at revision "+num(card), "kander show --json "+k.task)

		other := intent
		other.TaskID = "20260101-other-task"
		input.Dispatch, input.CardRevision = &other, card
		_, err = k.sender.CreateRequest(t.Context(), "other-task", k.task, input)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "",
			"dispatch request must bind the inspected card version", "the proposal names task 20260101-other-task", "the request path names task "+k.task)
		if strings.Contains(err.Error(), "the request has card_revision") {
			t.Fatal("the card revision holds and is not named")
		}

		long := intent
		long.TaskID = "20260101-" + strings.Repeat("long-", 30) + "task"
		input.Dispatch = &long
		_, err = k.sender.CreateRequest(t.Context(), "long-task", k.task, input)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "", "the proposal names task "+long.TaskID+",")

		bad := intent
		bad.TaskID = "tok-pasted/by mistake"
		input.Dispatch = &bad
		_, err = k.sender.CreateRequest(t.Context(), "bad-task", k.task, input)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "", "the proposal names task <invalid>,")
		if strings.Contains(err.Error(), "tok-pasted") {
			t.Fatal("an invalid task ID is written back")
		}
	})

	t.Run("an answer to another revision of the request", func(t *testing.T) {
		k := newKernelFixture(t, "refusal-values-answer")
		request := k.create(t, "plain", api.NewCollabRequest{Kind: api.RequestFix, Message: "fix", TargetGeneration: k.grant.Claim.Generation})
		_, err := k.answer("answer", request.ID, api.CollabAnswer{Status: api.RequestAccepted, ExpectedRevision: 9})
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "",
			"request revision changed", "expected_revision 9", "the request is at revision "+num(request.Revision), "kander request show "+k.task+" "+request.ID)
	})
}

// The board code families keep their API code and their HTTP status through the
// server: a rejected review, coordinator or dispatch request is a board rejection
// (422), and a stale card revision is a revision conflict (409).
func TestBoardRefusalFamiliesKeepTheirAPICodes(t *testing.T) {
	english(t)
	t.Run("dispatch_conflict", func(t *testing.T) {
		f := newFixture(t)
		a := f.project("host-a")
		task := todoTask(t, a, "refusal-values-dispatch")
		grant, err := a.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{BranchMode: "main-develop", ClientID: "host-a", ExecutionID: "dispatch-execution", Owner: "codex", Start: &api.StartInput{Session: "codex dispatch-session", Window: "tmux:$1:@1:%1"}})
		if err != nil {
			t.Fatal(err)
		}
		proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
		if task, err = a.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof); err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(task.Text, "- TASK_BRANCH:", "- TASK_BRANCH: refusal-values-branch", 1)
		if task, err = a.PutDocument(t.Context(), "task-branch", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, &proof); err != nil {
			t.Fatal(err)
		}
		input := board.DispatchInput{ID: "refusal-round", TaskID: task.TaskID, Kind: "sync", Message: "Synchronize the verified target commit", Base: strings.Repeat("a", 40)}
		prepared, err := a.DispatchAction(t.Context(), "prepare", task.TaskID, "prepare", api.DispatchCommand{Input: &input, CardRevision: task.Revision}, proof)
		if err != nil {
			t.Fatal(err)
		}
		current, err := a.Show(t.Context(), task.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.DispatchAction(t.Context(), "stale-attempt", task.TaskID, "attempt", api.DispatchCommand{DispatchID: input.ID, Revision: prepared.Revision + 5, CardRevision: current.Revision}, proof)
		refusal(t, err, api.CodeBoardRejected, http.StatusUnprocessableEntity, "board.dispatch_conflict",
			input.ID, "the request names dispatch revision "+num(prepared.Revision+5), "the dispatch is at revision "+num(prepared.Revision), "kander dispatch show")

		// A stale card revision of the same card is a revision conflict.
		_, err = a.PutDocument(t.Context(), "stale-document", task.TaskID, api.Document{Path: "notes.md", Text: "x", ExpectedRevision: current.Revision + 9, Authorization: prepared.Authorization}, &proof)
		refusal(t, err, api.CodeRevisionConflict, http.StatusConflict, "board.transaction_conflict",
			task.TaskID, "expected revision "+num(current.Revision+9), "current revision "+num(current.Revision), "re-read")
	})

	t.Run("coordinator_conflict", func(t *testing.T) {
		f := newFixture(t)
		a, b := f.project("host-a"), f.project("host-b")
		request := groupRequest(t, a)
		first, err := a.ClaimGroup(t.Context(), "claim-first", request.Request.GroupID, request, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.clock.advance(91 * time.Second)
		take := request
		take.ExecutionID = "successor-coordinator"
		take.ExpectedGeneration = first.Claim.Generation
		take.Decision = "explicit coordinator handoff after lease loss"
		take.Request.ExpectedRevision = first.Checkpoint.Revision + 6
		take.Request.ExpectedEpoch = first.Checkpoint.Authority.Epoch
		_, err = b.ClaimGroup(t.Context(), "claim-second", request.Request.GroupID, take, nil)
		refusal(t, err, api.CodeBoardRejected, http.StatusUnprocessableEntity, "board.coordinator_conflict",
			"claim CAS or reused token", "expected_revision is "+num(first.Checkpoint.Revision+6), "is at revision "+num(first.Checkpoint.Revision))
		if strings.Contains(err.Error(), request.Request.Token) || strings.Contains(err.Error(), first.Token) {
			t.Fatal("a token is written in the refusal")
		}
	})

	t.Run("review_evidence_invalid", func(t *testing.T) {
		f := newFixture(t)
		a := f.project("host-a")
		_, err := a.ReviewAction(t.Context(), "extend", "extend-plan", board.ReviewPlanExtension{PlanID: "missing-plan", ExpectedRevision: 1, Seal: true, Author: "coordinator", Basis: "seal"}, nil)
		refusal(t, err, api.CodeBoardRejected, http.StatusUnprocessableEntity, "board.review_evidence_invalid", "missing plan")
	})
}

func num(v uint64) string { return strconv.FormatUint(v, 10) }

// english renders the board refusals in English; the server renders them in its own language.
func english(t *testing.T) {
	t.Helper()
	previous := config.CLILanguage()
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Cleanup(func() {
		if previous == "" {
			config.ApplyLanguageArgument([]string{"kander"})
			return
		}
		config.ApplyLanguageArgument([]string{"kander", "--lang", previous})
	})
}
