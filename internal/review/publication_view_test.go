//go:build unix

package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/remote"
)

var publicationIDPattern = regexp.MustCompile(`review-[0-9a-f]{64}`)

// prepared has the coordinator prepare a joint publication through the client
// entry the evidence commands use, and returns the publication the server stored.
func (r *coordinatedRun) prepared(operation string, payload any) api.Publication {
	r.t.Helper()
	_, err := r.coordinate.ReviewAuthority(r.t.Context(), operation, payload, r.ids[:1])
	return r.stored(err)
}

// stored reads the publication that the wait for the holder names.
func (r *coordinatedRun) stored(err error) api.Publication {
	r.t.Helper()
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		r.t.Fatalf("want a wait for the holder: %v", err)
	}
	id := publicationIDPattern.FindString(err.Error())
	if id == "" {
		r.t.Fatalf("wait omits the publication: %v", err)
	}
	publication, err := r.holder.Project.Publication(r.t.Context(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return publication
}

// view reads the publication as a process with no credential does, and decodes
// what the command prints into generic JSON, as a reader of its stdout would.
func (r *coordinatedRun) view(id string) map[string]any {
	r.t.Helper()
	view, err := readPublicationView(r.t.Context(), r.stranger, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return decoded(r.t, view)
}

func decoded(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

// at follows object keys and array indexes (as decimal text) from value.
func at(t *testing.T, value any, path ...string) any {
	t.Helper()
	for _, step := range path {
		switch v := value.(type) {
		case map[string]any:
			next, found := v[step]
			if !found {
				t.Fatalf("no %q in %v", step, v)
			}
			value = next
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(v) {
				t.Fatalf("no index %q in %v", step, v)
			}
			value = v[index]
		default:
			t.Fatalf("cannot step into %v at %q", value, step)
		}
	}
	return value
}

// Each operation prints the operation, the digest and the members the server stored, and
// the points of its material.
func TestPublicationViewPrintsTheStoredMaterial(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	unlock, err := r.store.Lock("coordinated-run")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
	_, _, err = r.store.Prepare(r.input(), requirements, nil, map[string][]byte{"task-context.md": []byte("frozen task")}, "test")
	prepare := r.stored(err)
	plan := board.ReviewPlan{Schema: 1, PlanID: "view-plan", Author: "coordinator", Basis: "view fixture", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: r.ids, Batches: []board.ReviewPlanBatch{{BatchID: "view-batch", TaskIDs: r.ids, Base: coordinatedBase, TargetCommit: coordinatedHead, Requirements: requirements}}}
	closing := map[string]any{
		"request":  board.ReviewCloseRequest{BatchID: "coordinated-batch", ExpectedRevision: 3, ViewHash: strings.Repeat("a", 64), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}},
		"evidence": board.ReviewGitEvidence{CWD: "/host-a/clone", Head: coordinatedHead, VerifiedAt: "2026-11-20T00:00:00Z", Edges: []board.ReviewGitEdge{{Ancestor: coordinatedBase, Descendant: coordinatedHead}}},
	}
	assignment := board.ReviewAssignment{RunID: "coordinated-run", BatchID: "coordinated-batch", Author: "coordinator", Basis: "view fixture", Items: map[string][]string{r.ids[0]: {"finding-1"}}}
	cases := []struct {
		publication api.Publication
		points      map[string]any
	}{
		{r.prepared("review-plan", plan), map[string]any{"plan_id": "view-plan", "batches/0/batch_id": "view-batch", "batches/0/task_ids/0": r.ids[0]}},
		{r.prepared("review-close:coordinated-batch", closing), map[string]any{"request/batch_id": "coordinated-batch", "evidence/head": coordinatedHead, "evidence/edges/0/ancestor": coordinatedBase}},
		{r.prepared("review-assign", assignment), map[string]any{"run_id": "coordinated-run", "items/" + r.ids[0] + "/0": "finding-1"}},
		{r.prepared("review-publish:coordinated-run", "coordinated-run"), map[string]any{"": "coordinated-run"}},
		{prepare, map[string]any{"input/run_id": "coordinated-run", "input/batch_id": "coordinated-batch", "requirements/PMQA": "required"}},
		{r.prepared("review-aggregate:coordinated-batch", "coordinated-batch"), map[string]any{"": "coordinated-batch"}},
	}
	for _, c := range cases {
		t.Run(c.publication.Operation, func(t *testing.T) {
			got := r.view(c.publication.ID)
			if got["publication_id"] != c.publication.ID || got["operation"] != c.publication.Operation || got["digest"] != c.publication.Digest || got["status"] != "prepared" {
				t.Fatalf("view=%v, stored=%+v", got, c.publication)
			}
			if got["sanitized"] != false {
				t.Fatalf("clean material marked sanitized: %v", got)
			}
			if created, err := time.Parse(time.RFC3339Nano, got["created_at"].(string)); err != nil || !created.Equal(c.publication.CreatedAt) {
				t.Fatalf("created_at %v, stored %v", got["created_at"], c.publication.CreatedAt)
			}
			member := c.publication.Members[0]
			want := map[string]any{"task_id": member.TaskID, "revision": float64(member.Revision), "generation": float64(member.Generation), "confirmed": false, "stale": false}
			if members := got["members"].([]any); len(members) != 1 || !equalObjects(members[0].(map[string]any), want) {
				t.Fatalf("members=%v, want %v", members, want)
			}
			for path, value := range c.points {
				var steps []string
				if path != "" {
					steps = strings.Split(path, "/")
				}
				if found := at(t, got["payload"], steps...); found != value {
					t.Fatalf("payload %q = %v, want %v (payload %v)", path, found, value, got["payload"])
				}
			}
		})
	}
}

func equalObjects(a, b map[string]any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// The files of a review-prepare material are printed as a name, a length and a digest
// each: no content, and two names that are equal once cleaned keep two entries.
func TestPublicationViewSummarizesOriginalsWithoutContent(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	content := "SECRET-FILE-CONTENT\n"
	originals := map[string][]byte{"task-context.md": []byte(content), "x<U+0001>.md": []byte("literal"), "x\x01.md": []byte("control")}
	request := api.ReviewPrepare{Input: r.input(), Requirements: map[string]string{"PMQA": "required"}, Originals: originals, Version: "test", WriterHash: strings.Repeat("b", 64)}
	_, err := r.coordinate.PrepareReviewAuthority(t.Context(), request, r.ids[:1], "")
	publication := r.stored(err)
	view, err := readPublicationView(t.Context(), r.stranger, publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	printed, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"SECRET-FILE-CONTENT", "literal", "control", "U0VDUkVU"} {
		if strings.Contains(string(printed), leaked) {
			t.Fatalf("the output holds file content %q: %s", leaked, printed)
		}
	}
	files, ok := at(t, decoded(t, view), "payload", "originals").([]any)
	if !ok || len(files) != 3 {
		t.Fatalf("originals=%v, want three entries", files)
	}
	sum := sha256.Sum256([]byte(content))
	first := files[0].(map[string]any)
	if len(first) != 3 || first["name"] != "task-context.md" || first["bytes"] != float64(len(content)) || first["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("first entry=%v", first)
	}
	for _, file := range files[1:] {
		if name := file.(map[string]any)["name"]; name != "x<U+0001>.md" {
			t.Fatalf("name=%v, want the cleaned name", name)
		}
	}
	if view.Sanitized != true {
		t.Fatal("a cleaned file name left sanitized false")
	}
}

// The material is written by the orchestrator. Whatever it holds, the strings a reader
// decodes from the output carry none of the characters that steer a terminal or a reader.
func TestPublicationViewCleansUntrustedText(t *testing.T) {
	hostile := "esc\x1b|bell\x07|nul\x00|del\x7f|c1\u0085|csi\x1b[31mred\x1b[0m|osc\x1b]0;title\x07|osc-st\x1b]8;;http://x\x1b\\|rlo‮|rli⁧|lrm‎|rlm‏|alm؜|ok\tline\nend"
	r := newCoordinatedRun(t, 1)
	payload := map[string]any{"note": hostile, "list": []string{hostile}, hostile: "value", "nested": map[string]any{hostile: hostile}, "count": json.Number("12345678901234567890")}
	publication := r.prepared("review-assign", payload)
	view, err := readPublicationView(t.Context(), r.stranger, publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded(t, view)
	if got["sanitized"] != true {
		t.Fatalf("sanitized=%v", got["sanitized"])
	}
	texts := stringsOf(got)
	if len(texts) == 0 {
		t.Fatal("the view has no strings")
	}
	for _, s := range texts {
		for _, r := range s {
			if unsafeRune(r) {
				t.Fatalf("the decoded string %q holds %U", s, r)
			}
		}
	}
	note := at(t, got["payload"], "note").(string)
	for _, marker := range []string{"<U+001B>", "<U+0007>", "<U+0000>", "<U+007F>", "<U+0085>", "<U+202E>", "<U+2067>", "<U+200E>", "<U+200F>", "<U+061C>", "ok\tline\nend"} {
		if !strings.Contains(note, marker) {
			t.Fatalf("note %q omits %q", note, marker)
		}
	}
	// A whole escape sequence is one marker, not a marker and the rest of the sequence.
	for _, rest := range []string{"[31m", "]0;title", "http://x"} {
		if strings.Contains(note, rest) {
			t.Fatalf("note %q keeps %q of an escape sequence", note, rest)
		}
	}
	// A number keeps every digit it was stored with.
	if printed, err := json.Marshal(view); err != nil || !strings.Contains(string(printed), `"count":12345678901234567890`) {
		t.Fatalf("count was rewritten: %s %v", printed, err)
	}
	if clean := r.view(r.prepared("review-assign", map[string]any{"note": "plain"}).ID); clean["sanitized"] != false {
		t.Fatalf("clean material: sanitized=%v", clean["sanitized"])
	}
}

// stringsOf collects every string value and object key of a decoded JSON value.
func stringsOf(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, stringsOf(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for key, item := range v {
			out = append(out, key)
			out = append(out, stringsOf(item)...)
		}
		return out
	}
	return nil
}

// A string above 4096 characters prints its start with its length and digest; one of exactly
// 4096 characters prints whole.
func TestPublicationViewTruncatesLongText(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	exact := strings.Repeat("界", 4096)
	long := strings.Repeat("界", 4097)
	got := r.view(r.prepared("review-assign", map[string]any{"exact": exact, "long": long}).ID)
	if at(t, got["payload"], "exact") != exact {
		t.Fatal("a value of 4096 characters was cut")
	}
	cut := at(t, got["payload"], "long").(map[string]any)
	sum := sha256.Sum256([]byte(long))
	if len(cut) != 4 || cut["text"] != exact || cut["truncated"] != true || cut["length"] != float64(4097) || cut["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("long value printed as %v", cut)
	}
}

// confirmed reads only the confirmation records of the publication; stale reads the card of
// each member now.
func TestPublicationViewConfirmedAndStale(t *testing.T) {
	member := func(got map[string]any) map[string]any { return at(t, got, "members", "0").(map[string]any) }
	prepare := func(t *testing.T) (*coordinatedRun, api.Publication) {
		r := newCoordinatedRun(t, 1)
		return r, r.prepared("review-assign", board.ReviewAssignment{RunID: "coordinated-run", BatchID: "coordinated-batch", Author: "coordinator", Basis: "stale fixture", Items: map[string][]string{}})
	}
	confirm := func(t *testing.T, r *coordinatedRun, publication api.Publication) {
		t.Helper()
		if _, err := r.holder.ConfirmReviewPublication(t.Context(), publication.ID, r.ids[0]); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("before and after the confirmation", func(t *testing.T) {
		r, publication := prepare(t)
		if got := member(r.view(publication.ID)); got["confirmed"] != false || got["stale"] != false {
			t.Fatalf("unconfirmed member=%v", got)
		}
		confirm(t, r, publication)
		if got := member(r.view(publication.ID)); got["confirmed"] != true || got["stale"] != false {
			t.Fatalf("confirmed member=%v", got)
		}
	})
	t.Run("the card changed after the confirmation", func(t *testing.T) {
		r, publication := prepare(t)
		confirm(t, r, publication)
		card, err := r.holder.Project.Show(t.Context(), r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		own, _, err := r.holder.Credential(r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.holder.Project.PutDocument(t.Context(), "", r.ids[0], api.Document{Path: "spec.md", Text: card.Text + "\nNote.\n", ExpectedRevision: card.Revision}, own.Claim()); err != nil {
			t.Fatal(err)
		}
		// The confirmation record is unchanged; the card is not the version it names.
		if got := member(r.view(publication.ID)); got["confirmed"] != true || got["stale"] != true {
			t.Fatalf("member of a changed card=%v", got)
		}
	})
	t.Run("another execution took the card over", func(t *testing.T) {
		now := time.Now()
		coordinatedClock = func() time.Time { return now }
		t.Cleanup(func() { coordinatedClock = nil })
		r, publication := prepare(t)
		confirm(t, r, publication)
		// The lease of the first execution runs out and a second execution takes the card.
		now = now.Add(24 * time.Hour)
		own, _, err := r.holder.Credential(r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		next, err := r.coordinate.Project.Takeover(t.Context(), "stale-takeover", r.ids[0], api.ClaimRequest{ClientID: "host-b", ExecutionID: "second-execution", Owner: "codex", Decision: "stale fixture", ExpectedGeneration: own.Generation})
		if err != nil || next.Claim.Generation == own.Generation {
			t.Fatalf("takeover: %+v %v", next, err)
		}
		if got := member(r.view(publication.ID)); got["stale"] != true {
			t.Fatalf("member of a card another execution holds=%v", got)
		}
	})
	t.Run("the card has no live claim", func(t *testing.T) {
		r, publication := prepare(t)
		own, _, err := r.holder.Credential(r.ids[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.holder.Project.Move(t.Context(), "", r.ids[0], api.Move{State: "trash", Result: "trashed", Reason: "left the batch", Decision: "stale fixture"}, own.Claim()); err != nil {
			t.Fatal(err)
		}
		if got := member(r.view(publication.ID)); got["stale"] != true {
			t.Fatalf("member of a card with no claim=%v", got)
		}
	})
}

func TestMemberConfirmedNeedsARecordThatMatchesThePublicationAndTheMember(t *testing.T) {
	member := api.PublicationMember{TaskID: "task", Revision: 4, Generation: 2}
	approval := api.PublicationApproval{TaskID: "task", Digest: "digest", Revision: 4, Generation: 2}
	publication := func(a *api.PublicationApproval) api.Publication {
		p := api.Publication{Digest: "digest", Approvals: map[string]api.PublicationApproval{}}
		if a != nil {
			p.Approvals["task"] = *a
		}
		return p
	}
	other := func(change func(*api.PublicationApproval)) *api.PublicationApproval {
		a := approval
		change(&a)
		return &a
	}
	for name, c := range map[string]struct {
		publication api.Publication
		want        bool
	}{
		"no record":          {publication(nil), false},
		"matching record":    {publication(&approval), true},
		"another digest":     {publication(other(func(a *api.PublicationApproval) { a.Digest = "changed" })), false},
		"another revision":   {publication(other(func(a *api.PublicationApproval) { a.Revision = 5 })), false},
		"another generation": {publication(other(func(a *api.PublicationApproval) { a.Generation = 3 })), false},
	} {
		if got := remote.MemberConfirmed(c.publication, member); got != c.want {
			t.Errorf("%s: confirmed=%v, want %v", name, got, c.want)
		}
	}
}

// The confirm result names the operation and the members that still wait.
func TestConfirmResultListsTheMembersThatStillWait(t *testing.T) {
	r := newCoordinatedRunKeeping(t, 2, 2, true)
	_, err := r.coordinate.ReviewAuthority(t.Context(), "review-assign", board.ReviewAssignment{RunID: "coordinated-run", BatchID: "coordinated-batch", Author: "coordinator", Basis: "two members", Items: map[string][]string{}}, r.ids)
	publication := r.stored(err)
	first, err := confirmPublication(t.Context(), r.holder, publication.ID, r.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Operation != "review-assign" || first.Status != "confirmed" || first.Digest != publication.Digest || first.TaskID != r.ids[0] || !slices.Equal(first.Pending, r.ids[1:]) {
		t.Fatalf("first=%+v, want %s pending", first, r.ids[1])
	}
	second, err := confirmPublication(t.Context(), r.holder, publication.ID, r.ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if second.Pending == nil || len(second.Pending) != 0 {
		t.Fatalf("second=%+v, want an empty pending array", second)
	}
	printed, err := json.Marshal(second)
	if err != nil || !strings.Contains(string(printed), `"pending":[]`) {
		t.Fatalf("printed=%s %v", printed, err)
	}
}

// A file the client left empty is a nil byte slice, which JSON writes as null: it prints as an
// entry of zero bytes, and the other files stay readable.
func TestPublicationViewReadsAnEmptyOriginal(t *testing.T) {
	r := newCoordinatedRun(t, 1)
	request := api.ReviewPrepare{Input: r.input(), Requirements: map[string]string{"PMQA": "required"}, Originals: map[string][]byte{"review-context.md": nil, "task-context.md": []byte("frozen")}, Version: "test", WriterHash: strings.Repeat("b", 64)}
	_, err := r.coordinate.PrepareReviewAuthority(t.Context(), request, r.ids[:1], "")
	publication := r.stored(err)
	if !strings.Contains(string(publication.Payload), `"review-context.md":null`) {
		t.Fatalf("the stored material does not hold the null: %s", publication.Payload)
	}
	files := at(t, r.view(publication.ID), "payload", "originals").([]any)
	empty := sha256.Sum256(nil)
	first := files[0].(map[string]any)
	if len(files) != 2 || first["name"] != "review-context.md" || first["bytes"] != float64(0) || first["sha256"] != hex.EncodeToString(empty[:]) {
		t.Fatalf("originals=%v", files)
	}
}
