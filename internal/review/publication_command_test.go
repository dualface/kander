//go:build unix

package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
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
		if code != 2 || code != want || stdout != "" || stderr == "" {
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

// Reading creates no client identity, registers no project and sends nothing but GET, whether
// the read succeeds, the publication is unknown, or the project is.
func TestPublicationCommandWritesNothing(t *testing.T) {
	w := newRemoteWorld(t)
	publication := w.prepared(t, "review-assign", map[string]string{"run_id": "run-1"})
	home := filepath.Join(t.TempDir(), "empty-client-home")
	t.Setenv(remote.EnvClientHome, home)
	w.writes.take()
	assertNothingWritten := func(what string) {
		t.Helper()
		if calls := w.writes.take(); len(calls) != 0 {
			t.Fatalf("%s sent writes: %v", what, calls)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("%s created the client home: %v", what, err)
		}
	}
	commandOK(t, "publication", w.h.repo, publication.ID)
	assertNothingWritten("a read")
	if code, _, _ := captureRun(t, []string{"publication", w.h.repo, "review-" + strings.Repeat("0", 64)}); code != 2 {
		t.Fatalf("unknown publication: code=%d", code)
	}
	assertNothingWritten("a read of an unknown publication")

	configPath := os.Getenv(config.EnvConfig)
	var settings map[string]any
	if err := json.Unmarshal([]byte(readFile(t, configPath)), &settings); err != nil {
		t.Fatal(err)
	}
	settings["project"] = map[string]string{"name": "never-registered"}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := captureRun(t, []string{"publication", w.h.repo, publication.ID, "--json"})
	if detail := jsonObject(t, stderr)["error"].(map[string]any); code != 2 || detail["code"] != "not_found" {
		t.Fatalf("unknown project: code=%d stderr=%s", code, stderr)
	}
	assertNothingWritten("a read in an unregistered project")
	if _, err := w.session.Client.ProjectByName(t.Context(), "never-registered"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("the read registered the project: %v", err)
	}
}

// What a reader decodes from the stdout of the command holds no steering character in any string
// or key, however the material hides it: 8-bit sequences, a sequence across the cut of a long
// string, and nested keys.
func TestPublicationCommandOutputIsCleanAfterDecoding(t *testing.T) {
	w := newRemoteWorld(t)
	crossing := strings.Repeat("a", maxMaterialText-6) + "\x1b]0;" + strings.Repeat("t", 100) + "\x07" + "tail"
	payload := map[string]any{
		"c1":       "x\u009b31my\u009d0;title\u009cz",
		"cancel":   "\u009d0;title\x18Review decision: reject\x1b]0;next\x07",
		"c1st":     "a\x1b]0;hidden\u009cb",
		"crossing": crossing,
		"nested":   map[string]any{"k‮\x1b[0m": []string{"v⁧", "w\x00"}},
	}
	publication := w.prepared(t, "review-assign", payload)
	var printed map[string]any
	if err := json.Unmarshal([]byte(commandOK(t, "publication", w.h.repo, publication.ID, "--json")), &printed); err != nil {
		t.Fatal(err)
	}
	if printed["sanitized"] != true {
		t.Fatalf("sanitized=%v", printed["sanitized"])
	}
	for _, text := range stringsOf(printed) {
		for _, r := range text {
			if unsafeRune(r) {
				t.Fatalf("the decoded string %q holds %U", text, r)
			}
		}
	}
	body := at(t, printed, "payload").(map[string]any)
	if got := body["c1"]; got != "x<U+009B>y<U+009D>z" {
		t.Fatalf("c1=%q", got)
	}
	// A cancelled sequence is no sequence, so the text after the cancel stays; a sequence that a
	// C1 ST ends is one marker.
	if got := body["cancel"]; got != "<U+009D>0;title<U+0018>Review decision: reject<U+001B>" {
		t.Fatalf("cancel=%q", got)
	}
	if got := body["c1st"]; got != "a<U+001B>b" {
		t.Fatalf("c1st=%q", got)
	}
	cut := body["crossing"].(map[string]any)
	if text := cut["text"].(string); strings.Contains(text, "tt") || !strings.HasSuffix(text, "<U+001B>") || cut["truncated"] != true {
		t.Fatalf("crossing=%v", cut)
	}
	if _, ok := at(t, body, "nested").(map[string]any)["k<U+202E><U+001B>"]; !ok {
		t.Fatalf("nested keys=%v", body["nested"])
	}
}
