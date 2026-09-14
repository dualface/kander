package issue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type resultFake struct {
	remote                     ResultRemote
	posts, closes              int
	readErr, postErr, closeErr error
	accept                     bool
	crash                      bool
}

func (f *resultFake) ReadResult(context.Context, Repository, int) (ResultRemote, error) {
	return f.remote, f.readErr
}
func (f *resultFake) PostResult(_ context.Context, _ Repository, _ int, body string) (ResultComment, error) {
	f.posts++
	c := ResultComment{ID: int64(100 + f.posts), AuthorID: f.remote.ActorID, Body: body}
	if f.postErr == nil || f.accept {
		f.remote.Comments = append(f.remote.Comments, c)
	}
	if f.crash {
		panic("simulated exit after send")
	}
	return c, f.postErr
}
func (f *resultFake) CloseResult(context.Context, Repository, int) error {
	f.closes++
	if f.closeErr == nil || f.accept {
		f.remote.State = "closed"
		f.remote.StateVersion = "42:closed"
	}
	return f.closeErr
}

func resultFixture(t *testing.T) (string, Repository, string, *resultFake) {
	t.Helper()
	root := importTestBoard(t)
	repository := importTestRepository()
	stub := &stubResolver{snapshot: importTestSnapshot(repository, 42)}
	imported, err := Import(context.Background(), stub, root, repository, 42, ImportOptions{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	// Construct an isolated historical done fixture without inventing runtime
	// review conclusions; no real board/configuration is touched by these tests.
	done := filepath.Join(root, "done", imported.TaskID)
	if err := os.Rename(imported.Path, done); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(done, "spec.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "## SUMMARY", "## SUMMARY\n\nThe entire issue is resolved. Verified: go test ./...\nDelivery: "+strings.Repeat("a", 40)+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	key, _ := repository.IssueSourceKey(42)
	fake := &resultFake{remote: ResultRemote{SourceKey: key, IssueID: 42, ActorID: 7, State: "open", StateVersion: "42:initial", Revision: "one", Comments: []ResultComment{}}}
	return root, repository, imported.TaskID, fake
}

func inspectFixture(t *testing.T, root string, repo Repository, id string, fake *resultFake) ResultInspection {
	t.Helper()
	out, err := InspectResult(context.Background(), fake, root, repo, 42, id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func fixtureProposal(i ResultInspection) ResultProposal {
	return ResultProposal{Token: i.Token, Outcomes: []string{"met", "met", "met", "met"}, Checks: []ResultCheck{{Command: "go test ./...", Status: "pass"}}, Body: "The issue is resolved. Delivered the fix; go test ./... passed. No remaining work.", FullyResolved: true, ResolutionEvidence: "The entire issue is resolved."}
}

func TestResultRepeatLanguageLogsAndUpdatedDelivery(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	i := inspectFixture(t, root, repo, id, fake)
	first, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "done", id, "spec.md")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), "## IMPLEMENTATION", "## IMPLEMENTATION\n\n2026-09-14: unrelated execution note.", 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	i = inspectFixture(t, root, repo, id, fake)
	p := fixtureProposal(i)
	p.Body = "问题已解决。测试通过，无遗留项。"
	second, err := ApplyResult(ctx, fake, root, repo, 42, id, p)
	if err != nil {
		t.Fatal(err)
	}
	if fake.posts != 1 || first.Version != second.Version {
		t.Fatalf("duplicated rewrite: posts=%d", fake.posts)
	}
	data = []byte(strings.Replace(string(data), strings.Repeat("a", 40), strings.Repeat("b", 40), 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	i = inspectFixture(t, root, repo, id, fake)
	third, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	if fake.posts != 2 || third.Version == first.Version {
		t.Fatal("updated delivery not published")
	}
}

func TestResultEquivalentHumanCommentAndCriterionSafety(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	fake.remote.Comments = []ResultComment{{ID: 99, AuthorID: 80, Body: "Human verification: fixed, tests pass."}}
	i := inspectFixture(t, root, repo, id, fake)
	p := fixtureProposal(i)
	p.EquivalentCommentID = 99
	p.Body = ""
	out, err := ApplyResult(ctx, fake, root, repo, 42, id, p)
	if err != nil || out.Status != "equivalent" || fake.posts != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	for _, mutate := range []func(*ResultProposal){
		func(p *ResultProposal) { p.Outcomes[0] = "unknown" },
		func(p *ResultProposal) { p.ResolutionEvidence = "not in the card" },
		func(p *ResultProposal) { p.Outcomes = nil },
		func(p *ResultProposal) { p.Checks[0].Command = "never executed" },
	} {
		p := fixtureProposal(i)
		mutate(&p)
		if _, err := ApplyResult(ctx, fake, root, repo, 42, id, p); err == nil {
			t.Fatal("invalid resolution accepted")
		}
	}
}

func TestResultConcurrentAttempts(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	i := inspectFixture(t, root, repo, id, fake)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ApplyResult(context.Background(), fake, root, repo, 42, id, fixtureProposal(i))
		}()
	}
	wg.Wait()
	if fake.posts != 1 {
		t.Fatalf("posts=%d", fake.posts)
	}
	if len(inspectFixture(t, root, repo, id, fake).Records) != 1 {
		t.Fatal("multiple intents")
	}
}

func TestResultUncertainSendRecovery(t *testing.T) {
	for _, mode := range []string{"response-lost", "crash-after-send", "not-accepted", "forged-marker"} {
		t.Run(mode, func(t *testing.T) {
			root, repo, id, fake := resultFixture(t)
			i := inspectFixture(t, root, repo, id, fake)
			fake.postErr = errors.New("response lost")
			fake.accept = mode == "response-lost" || mode == "crash-after-send"
			fake.crash = mode == "crash-after-send"
			func() {
				defer func() {
					if r := recover(); r != nil && !fake.crash {
						panic(r)
					}
				}()
				_, _ = ApplyResult(context.Background(), fake, root, repo, 42, id, fixtureProposal(i))
			}()
			if mode == "forged-marker" {
				_ = withResultStore(context.Background(), root, repo, 42, id, func(s *resultStore, _ func() error) error {
					for _, r := range s.Records {
						fake.remote.Comments = append(fake.remote.Comments, ResultComment{ID: 500, AuthorID: 999, Body: r.Body})
					}
					return nil
				})
			}
			i = inspectFixture(t, root, repo, id, fake)
			for _, r := range i.Records {
				want := "uncertain"
				if fake.accept {
					want = "published"
				}
				if r.Status != want {
					t.Fatalf("status=%s want=%s", r.Status, want)
				}
			}
			fake.postErr = nil
			fake.crash = false
			_, err := ApplyResult(context.Background(), fake, root, repo, 42, id, fixtureProposal(i))
			if !fake.accept && err == nil {
				t.Fatal("uncertainty cleared")
			}
			if fake.posts != 1 {
				t.Fatalf("retried send: %d", fake.posts)
			}
		})
	}
}

func TestResultCloseConsentRefusalAndReopening(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	i := inspectFixture(t, root, repo, id, fake)
	record, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
	if err != nil {
		t.Fatal(err)
	}
	i = inspectFixture(t, root, repo, id, fake)
	d := ResultDecision{Token: i.Token, Version: record.Version, Decision: "no", UserReference: "User: do not close this issue."}
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err != nil {
		t.Fatal(err)
	}
	d.Decision = "yes"
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err == nil || fake.closes != 0 {
		t.Fatal("refusal must explicitly reject an unconfirmed override")
	}
	d.Reconsider = true
	d.UserReference = "User explicitly reconsidered and confirmed closing this issue."
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err != nil || fake.closes != 1 {
		t.Fatalf("close err=%v count=%d", err, fake.closes)
	}
	i = inspectFixture(t, root, repo, id, fake)
	d.Token = i.Token
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err != nil || fake.closes != 1 {
		t.Fatal("already closed repeated")
	}
	fake.remote.State = "open"
	fake.remote.StateVersion = "42:reopened"
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err == nil {
		t.Fatal("old consent closed reopened issue")
	}
	i = inspectFixture(t, root, repo, id, fake)
	d.Token = i.Token
	d.Reconsider = false
	d.Decision = "no"
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err != nil || fake.closes != 1 {
		t.Fatal("new refusal failed")
	}
}

func TestResultCloseLostResponseAndReadFailures(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(strconvBool(accepted), func(t *testing.T) {
			root, repo, id, fake := resultFixture(t)
			ctx := context.Background()
			i := inspectFixture(t, root, repo, id, fake)
			record, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i))
			if err != nil {
				t.Fatal(err)
			}
			i = inspectFixture(t, root, repo, id, fake)
			d := ResultDecision{Token: i.Token, Version: record.Version, Decision: "yes", UserReference: "User: close this issue now."}
			fake.closeErr = errors.New("timeout")
			fake.accept = accepted
			if _, err := DecideResult(ctx, fake, root, repo, 42, id, d); err == nil {
				t.Fatal("lost response reported success")
			}
			i = inspectFixture(t, root, repo, id, fake)
			d.Token = i.Token
			_, err = DecideResult(ctx, fake, root, repo, 42, id, d)
			if !accepted && err == nil {
				t.Fatal("uncertain close retried")
			}
			if fake.closes != 1 {
				t.Fatal("duplicate close")
			}
			fake.readErr = errors.New("incomplete comments")
			if _, err := InspectResult(ctx, fake, root, repo, 42, id); err == nil {
				t.Fatal("partial read accepted")
			}
			if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i)); err == nil {
				t.Fatal("write with incomplete read")
			}
		})
	}
}
func strconvBool(v bool) string {
	if v {
		return "accepted"
	}
	return "uncertain"
}

func TestResultRejectsChangedBindingStateAndPrivateText(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	i := inspectFixture(t, root, repo, id, fake)
	for _, body := range []string{"Inspect /etc/passwd", "[report](/home/user/report.md)", `See \\server\share\report.md`, `Use C:\Users\name\file`, "session 12345678-1234-1234-1234-123456789abc", "\x1b[31msecret"} {
		p := fixtureProposal(i)
		p.Body = body
		if _, err := ApplyResult(ctx, fake, root, repo, 42, id, p); err == nil {
			t.Fatalf("private text accepted %q", body)
		}
	}
	fake.remote.SourceKey = "github://github.com/other/repo/issues/42"
	if _, err := InspectResult(ctx, fake, root, repo, 42, id); err == nil {
		t.Fatal("foreign issue accepted")
	}
	fake.remote.SourceKey = i.SourceKey
	if err := os.Rename(filepath.Join(root, "done", id), filepath.Join(root, "archived", id)); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(i)); err == nil {
		t.Fatal("archived card published")
	}
	if fake.posts != 0 {
		t.Fatal("invalid target wrote")
	}
}

func TestResultPublicationAllowsLongMarkdownAndHTTPS(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	inspection := inspectFixture(t, root, repo, id, fake)
	proposal := fixtureProposal(inspection)
	proposal.Body = "## Delivery\n\n[Commit](https://github.com/dualface/kander/commit/" + strings.Repeat("a", 40) + ")\n\n## Verification\n\n- `go test ./...`: passed.\n\n## Remaining work\n\n" + strings.Repeat("The documented requirement is satisfied. ", 15)
	if len(proposal.Body) <= 300 {
		t.Fatal("fixture must exceed diagnostic bound")
	}
	if _, err := ApplyResult(context.Background(), fake, root, repo, 42, id, proposal); err != nil {
		t.Fatal(err)
	}
	if fake.posts != 1 || !strings.HasPrefix(fake.remote.Comments[0].Body, proposal.Body) {
		t.Fatal("publication text altered")
	}
}

func TestResultUnchangedEvidenceBlocksDifferentAssessments(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	inspection := inspectFixture(t, root, repo, id, fake)
	first, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(inspection))
	if err != nil {
		t.Fatal(err)
	}
	inspection = inspectFixture(t, root, repo, id, fake)
	proposal := fixtureProposal(inspection)
	proposal.Checks = nil
	second, err := ApplyResult(ctx, fake, root, repo, 42, id, proposal)
	if err != nil || second.Version != first.Version || fake.posts != 1 {
		t.Fatalf("check selection duplicated: %v", err)
	}
	proposal.Outcomes[0] = "unknown"
	proposal.FullyResolved = false
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, proposal); err == nil {
		t.Fatal("changed assessment bypassed unchanged evidence")
	}
	// A reassessment can reference existing coverage without publishing again.
	proposal.EquivalentCommentID = first.CommentID
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, proposal); err != nil {
		t.Fatal(err)
	}
	if fake.posts != 1 {
		t.Fatal("equivalent reassessment posted")
	}
}

func TestResultDefiniteWriteRejectionsCanRecover(t *testing.T) {
	root, repo, id, fake := resultFixture(t)
	ctx := context.Background()
	inspection := inspectFixture(t, root, repo, id, fake)
	fake.postErr = &ResultWriteRejection{Err: errors.New("HTTP 403 rejected")}
	if _, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(inspection)); err == nil {
		t.Fatal("rejection reported success")
	}
	inspection = inspectFixture(t, root, repo, id, fake)
	for _, record := range inspection.Records {
		if record.Status != "rejected" || len(record.Failures) != 1 {
			t.Fatalf("rejection missing: %+v", record)
		}
	}
	fake.postErr = nil
	record, err := ApplyResult(ctx, fake, root, repo, 42, id, fixtureProposal(inspection))
	if err != nil {
		t.Fatal(err)
	}
	if fake.posts != 2 || len(fake.remote.Comments) != 1 || len(record.Failures) != 1 {
		t.Fatal("failed attempt lost or duplicate comment")
	}
	inspection = inspectFixture(t, root, repo, id, fake)
	decision := ResultDecision{Token: inspection.Token, Version: record.Version, Decision: "yes", UserReference: "User confirms this target."}
	fake.closeErr = &ResultWriteRejection{Err: errors.New("HTTP 403 rejected")}
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, decision); err == nil {
		t.Fatal("close rejection reported success")
	}
	inspection = inspectFixture(t, root, repo, id, fake)
	decision.Token = inspection.Token
	fake.closeErr = nil
	if _, err := DecideResult(ctx, fake, root, repo, 42, id, decision); err == nil {
		t.Fatal("old failed consent reused without reconsideration")
	}
	decision.Reconsider = true
	decision.UserReference = "User explicitly confirms again after permission repair."
	record, err = DecideResult(ctx, fake, root, repo, 42, id, decision)
	if err != nil {
		t.Fatal(err)
	}
	if fake.closes != 2 || len(record.Failures) != 2 || record.Decisions[len(record.Decisions)-1].Status != "closed" {
		t.Fatal("close retry or failure history missing")
	}
}
