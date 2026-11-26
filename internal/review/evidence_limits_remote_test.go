//go:build unix

package review

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
)

func TestEvidenceRemoteFullAndIncremental(t *testing.T) {
	h, session, task, requirements := remoteReviewHarness(t, nil)
	t.Setenv("HOME", h.home)
	t.Setenv("FAKE_CODEX_REPORT", emptyStructuredReview)
	const path = "generated.js"
	large := strings.Repeat("a", 4<<20)
	generated := func(marker string) string { return large + marker + large[:3<<20] + "\n" }
	h.base = commitFile(t, h.repo, path, generated("before"), "generated base")
	h.head = commitFile(t, h.repo, path, generated("reviewed"), "generate again")
	args := []string{"codex", "--task", task.TaskID, "--run-id", "first", "--batch-id", "remote-batch", "--requirements-file", requirements}
	if code, _, stderr := captureRun(t, append(args, h.repo, h.base, h.head, "PMQA", "Frozen remote task")); code != 0 {
		t.Fatalf("initial remote review: %d %s", code, stderr)
	}
	store, err := newRemoteReviewStore(session)
	if err != nil {
		t.Fatal(err)
	}
	readOriginal := func(run, name string) string {
		t.Helper()
		raw, err := session.Project.ReviewEvidence(t.Context(), run, "original", name)
		if err != nil {
			t.Fatal(err)
		}
		var data []byte
		if err := json.Unmarshal(raw, &data); err != nil || len(data) == 0 {
			t.Fatalf("%s/%s was not uploaded: %v", run, name, err)
		}
		return string(data)
	}
	checkReview := func(run string, sections ...string) {
		t.Helper()
		status, err := store.Run(run)
		if err != nil || status.ExecutionStatus != "ok" || status.LaunchStatus != "started" || !status.Published[task.TaskID] {
			t.Fatalf("reviewer did not finish after accepting artifacts: %+v (%v)", status, err)
		}
		if _, err := os.Stat(h.argvLog); err != nil {
			t.Fatalf("reviewer did not run: %v", err)
		}
		readOriginal(run, "prompt.txt")
		contract := readOriginal(run, config.ReviewContractFilename)
		if !strings.Contains(contract, "These files remain in scope") || !strings.Contains(contract, "--stat") {
			t.Fatal("review contract did not include inspection guidance")
		}
		evidence := readOriginal(run, "evidence.txt")
		item := omittedEvidence(t, evidence)
		items := strings.Split(strings.TrimPrefix(item, "- section: "), "\n- section: ")
		if len(items) != len(sections) || !strings.Contains(evidence, evidenceOmissionNotice(len(sections))) {
			t.Fatalf("wrong omission count: %s", item)
		}
		for i, section := range sections {
			item := "- section: " + items[i]
			if evidenceField(item, "section") != section || evidenceField(item, "shown") != "preview" ||
				evidenceField(item, "path") != `"generated.js"` || evidenceField(item, "numstat") != "1\t1" ||
				evidenceField(item, "status") != "M" || len(evidenceField(item, "blob_before")) != 40 ||
				len(evidenceField(item, "blob_after")) != 40 || evidenceField(item, "reason") != "per-file limit" {
				t.Fatalf("incomplete omission: %s", item)
			}
			_, patch, found := strings.Cut(evidence, evidenceHeading(section))
			patch, _, _ = strings.Cut(patch, "\n=== ")
			if !found || !strings.Contains(patch, "@@ -1 +1 @@") || strings.Contains(patch, large) {
				t.Fatalf("%s has no bounded hunk", section)
			}
			for line := range strings.SplitSeq(patch, "\n") {
				if len(line)+1 > evidencePreviewLine {
					t.Fatalf("%s has a %d-byte line", section, len(line))
				}
			}
		}
	}
	checkReview("first", "PATCH")

	assignment := board.ReviewAssignment{RunID: "first", BatchID: "remote-batch", Author: "tester",
		Basis: "empty structured report", Items: map[string][]string{}}
	assignmentFile := writeEvidenceTestJSON(t, h.root, "assignment.json", assignment)
	if _, err := remoteEvidenceAction(t.Context(), session, h.repo, "assign", assignmentFile, nil); err != nil {
		t.Fatal(err)
	}

	target := commitFile(t, h.repo, path, generated("fixed"), "fix generated line")
	advance := board.ReviewAdvance{PreviousTarget: h.head, Target: target, Reason: "fix generated line",
		Deliveries: map[string]string{target: task.TaskID}}
	advanceFile := writeEvidenceTestJSON(t, h.root, "advance.json", advance)
	ctx := reviewContext{root: h.repo, base: h.base, commit: target, reviewed: h.head}
	full := []byte(originalEvidence(t, ctx))
	unbounded, err := json.Marshal(api.ReviewArtifact{Name: "evidence.txt", Data: full, Digest: board.ReviewDigest(full)})
	if err != nil || len(unbounded) <= artifactRequestLimit {
		t.Fatalf("regression fixture would not exceed limit without omissions: %d bytes (%v)", len(unbounded), err)
	}
	if err := os.Remove(h.argvLog); err != nil {
		t.Fatal(err)
	}
	args = []string{"codex", "--task", task.TaskID, "--run-id", "incremental", "--batch-id", "remote-batch",
		"--previous-run-id", "first", "--advance-file", advanceFile,
		h.repo, h.base, target, "PMQA", "Frozen remote task"}
	if code, _, stderr := captureRun(t, args); code != 0 {
		t.Fatalf("incremental remote review: %d %s", code, stderr)
	}
	checkReview("incremental", "PATCH", "FIX RANGE PATCH")
}

func TestEvidenceRemoteArtifactLimitMatchesHandler(t *testing.T) {
	_, session, task, _ := remoteReviewHarness(t, nil)
	store, err := newRemoteReviewStore(session)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := store.Lock("byte-limit")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	input := board.ReviewInput{RunID: "byte-limit", BatchID: "byte-limit-batch", TaskIDs: []string{task.TaskID},
		Role: "PMQA", Reviewer: "codex", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: test"}
	originals := map[string][]byte{"task-context.md": []byte("test"), "review-context.md": []byte{}}
	if _, fresh, err := store.Prepare(input, requirements, nil, originals, "test"); err != nil || !fresh {
		t.Fatalf("prepare: fresh=%v %v", fresh, err)
	}
	if err := store.Artifact(input.RunID, "evidence.txt", make([]byte, evidenceHardLimit)); err != nil {
		t.Fatalf("service did not accept evidence at the hard limit: %v", err)
	}

	payload := api.ReviewArtifact{Name: "evidence.txt", Data: []byte{}, Digest: strings.Repeat("0", 64)}
	empty, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	size := ((artifactRequestLimit-len(empty))/4 + 1) * 3
	payload.Data = make([]byte, size)
	payload.Digest = board.ReviewDigest(payload.Data)
	body, err := json.Marshal(payload)
	if err != nil || len(body) <= artifactRequestLimit || len(body) > artifactRequestLimit+4 {
		t.Fatalf("fixture is %d bytes, not just beyond the mirror: %v", len(body), err)
	}
	// Bypass the preflight to pin the actual handler's limit independently.
	_, err = session.Project.ReviewRuntime(t.Context(), client.NewKey(), input.RunID, "artifact",
		json.RawMessage(body), &client.Claim{ReviewWriter: store.writer})
	if !api.IsCode(err, api.CodeInvalidRequest) || !strings.Contains(err.Error(), "request body is too large") {
		t.Fatalf("request beyond the mirror was not rejected at the body boundary: %v", err)
	}
}

func TestEvidenceRemoteOversizeFailsBeforeReviewLaunch(t *testing.T) {
	var artifactRequests atomic.Int32
	h, session, task, requirements := remoteReviewHarness(t, func(_ http.ResponseWriter, req *http.Request) bool {
		if strings.HasSuffix(req.URL.Path, "/artifact") {
			artifactRequests.Add(1)
		}
		return false
	})
	// An incompressible evidence budget would also fail, but a fixed log
	// section is cheaper to construct than millions of small Git tree entries.
	size := artifactRequestLimit/4*3 + 1
	message := filepath.Join(h.root, "message.txt")
	if err := os.WriteFile(message, []byte(strings.Repeat("m", size)), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, h.repo, "commit", "--allow-empty", "--cleanup=verbatim", "-q", "-F", message)
	h.head = gitRepo(t, h.repo, "rev-parse", "HEAD")
	args := []string{"codex", "--task", task.TaskID, "--run-id", "oversized", "--batch-id", "oversized-batch",
		"--requirements-file", requirements, h.repo, h.base, h.head, "PMQA", "Frozen remote task"}
	code, _, stderr := captureRun(t, args)
	if code != 2 || artifactRequests.Load() != 2 || !strings.Contains(stderr, "Largest content source: COMMITS") {
		t.Fatalf("preflight: exit %d, %d requests, %s", code, artifactRequests.Load(), stderr)
	}
	if _, err := os.Stat(h.argvLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reviewer ran despite failed preflight: %v", err)
	}
	store, err := newRemoteReviewStore(session)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.Run("oversized")
	if err != nil {
		t.Fatal(err)
	}
	rawBytes := len(originalEvidence(t, reviewContext{root: h.repo, base: h.base, commit: h.head}))
	empty, err := json.Marshal(api.ReviewArtifact{Name: "evidence.txt", Data: []byte{}, Digest: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	want := config.Text("review.artifact_request_too_large", "evidence.txt", rawBytes,
		len(empty)+base64.StdEncoding.EncodedLen(rawBytes), artifactRequestLimit, fmt.Sprintf("COMMITS (%d bytes)",
			len(evidenceTestGit(t, h.repo, []string{"log", "--no-ext-diff", "--no-textconv", "--format=fuller", "--no-patch", h.base + ".." + h.head}))))
	if run.FailureReason != want || !strings.Contains(stderr, want) || run.ExecutionStatus != "not_started" {
		t.Fatalf("wrong final diagnostic (%s): %s (want %s)", run.ExecutionStatus, run.FailureReason, want)
	}
}

func writeEvidenceTestJSON(t *testing.T, dir, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
