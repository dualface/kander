//go:build unix

package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
)

// prepared stores a joint publication of the world's card, as an orchestrator's client does.
func (w *remoteWorld) prepared(t *testing.T, operation string, payload any) api.Publication {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	card, err := w.session.Project.Show(t.Context(), w.task)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(operation + string(raw)))
	prepare := api.PublicationPrepare{ID: "review-" + hex.EncodeToString(sum[:]), Operation: operation, Payload: raw, Members: []api.PublicationMember{{TaskID: w.task, Revision: card.Revision, Generation: card.Claim.Generation}}}
	publication, err := w.session.Project.PreparePublication(t.Context(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	return publication
}

// A missing publication fails like any evidence subcommand fails when it runs: status 2 and,
// with --json, the not_found error object. A wrong argument count is a usage error, as it is
// for the other evidence subcommands.
func TestPublicationCommandFailures(t *testing.T) {
	w := newRemoteWorld(t)
	missing := "review-" + strings.Repeat("0", 64)
	progressCode, _, _ := captureRun(t, []string{"progress", w.h.repo, "20261120-no-such-task"})
	code, stdout, stderr := captureRun(t, []string{"publication", w.h.repo, missing})
	if code != 2 || code != progressCode || stdout != "" || !strings.Contains(stderr, "joint publication does not exist") {
		t.Fatalf("code=%d (progress %d) stdout=%q stderr=%q", code, progressCode, stdout, stderr)
	}
	code, stdout, stderr = captureRun(t, []string{"publication", w.h.repo, missing, "--json"})
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if detail := jsonObject(t, stderr)["error"].(map[string]any); detail["code"] != "not_found" {
		t.Fatalf("error=%v", detail)
	}
	for _, args := range [][]string{{"publication", w.h.repo}, {"publication", w.h.repo, missing, "extra"}} {
		code, stdout, stderr := captureRun(t, args)
		want, _, _ := captureRun(t, append([]string{"progress", w.h.repo}, args[2:]...))
		if len(args) == 2 {
			want, _, _ = captureRun(t, []string{"progress", w.h.repo})
		}
		if code == 0 || code != want || stdout != "" || stderr == "" {
			t.Fatalf("%v: code=%d (other subcommand %d) stdout=%q stderr=%q", args, code, want, stdout, stderr)
		}
	}
}

// Reading a publication creates no request and changes neither the publication nor a card.
func TestPublicationCommandOnlyReads(t *testing.T) {
	w := newRemoteWorld(t)
	publication := w.prepared(t, "review-assign", map[string]string{"run_id": "run-1"})
	state := func() (string, uint64, int) {
		stored, err := w.session.Project.Publication(t.Context(), publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		card, err := w.session.Project.Show(t.Context(), w.task)
		if err != nil {
			t.Fatal(err)
		}
		requests, err := w.session.Project.Requests(t.Context(), w.task)
		if err != nil {
			t.Fatal(err)
		}
		return string(bytes), card.Revision, len(requests)
	}
	beforePublication, beforeRevision, beforeRequests := state()
	code, stdout, stderr := captureRun(t, []string{"publication", w.h.repo, publication.ID})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil || printed["publication_id"] != publication.ID || printed["operation"] != "review-assign" {
		t.Fatalf("stdout=%q %v", stdout, err)
	}
	code, stdout, stderr = captureRun(t, []string{"publication", w.h.repo, publication.ID, "--json"})
	if object := jsonObject(t, stdout); code != 0 || stderr != "" || object["digest"] != publication.Digest {
		t.Fatalf("--json: code=%d object=%v stderr=%q", code, object, stderr)
	}
	afterPublication, afterRevision, afterRequests := state()
	if afterPublication != beforePublication || afterRevision != beforeRevision || afterRequests != beforeRequests {
		t.Fatalf("the read changed state: publication %v, revision %d to %d, requests %d to %d", afterPublication == beforePublication, beforeRevision, afterRevision, beforeRequests, afterRequests)
	}
}

// The holder reads the material, confirms it, and sees the confirmation in both commands.
func TestHolderReadsThenConfirmsThroughTheCommands(t *testing.T) {
	w := newRemoteWorld(t)
	publication := w.prepared(t, "review-assign", map[string]string{"run_id": "run-1"})
	read := func() map[string]any {
		t.Helper()
		return jsonObject(t, commandOK(t, "publication", w.h.repo, publication.ID, "--json"))
	}
	if member := read()["members"].([]any)[0].(map[string]any); member["confirmed"] != false {
		t.Fatalf("member before the confirmation=%v", member)
	}
	confirmed := jsonObject(t, commandOK(t, "confirm", w.h.repo, publication.ID, w.task, "--json"))
	pending, isList := confirmed["pending"].([]any)
	if confirmed["operation"] != "review-assign" || confirmed["status"] != "confirmed" || confirmed["task_id"] != w.task || !isList || len(pending) != 0 {
		t.Fatalf("confirm result=%v", confirmed)
	}
	if member := read()["members"].([]any)[0].(map[string]any); member["confirmed"] != true || member["stale"] != false {
		t.Fatalf("member after the confirmation=%v", member)
	}
}

// The printed text keeps the markers readable and holds no raw escape.
func TestPublicationCommandPrintsReadableMarkers(t *testing.T) {
	w := newRemoteWorld(t)
	publication := w.prepared(t, "review-assign", map[string]string{"note": "red\x1b[31m text \x1b]0;title\x07 end"})
	out := commandOK(t, "publication", w.h.repo, publication.ID)
	for _, want := range []string{`"note": "red<U+001B> text <U+001B> end"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output omits %q: %s", want, out)
		}
	}
	for _, forbidden := range []string{"\x1b", `\u001b`, `\u003c`} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("output holds %q: %s", forbidden, out)
		}
	}
}
