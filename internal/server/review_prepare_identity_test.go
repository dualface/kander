package server_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// preparedIdentity is a run prepared by a client of one version, ready to be
// retried by a client of another.
type preparedIdentity struct {
	f       *fixture
	p       *client.Project
	request api.ReviewPrepare
	proof   client.Claim
}

func prepareWithVersion(t *testing.T, version string) preparedIdentity {
	t.Helper()
	f := newFixture(t)
	p := f.project("host-a")
	task := todoTask(t, p, "identity-version")
	grant, err := claim(t, p, task.TaskID, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 64)
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation, ReviewWriter: secret}
	request := api.ReviewPrepare{
		Input:        board.ReviewInput{RunID: "identity-run", BatchID: "identity-batch", TaskIDs: []string{task.TaskID}, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "/independent/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"},
		Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"},
		Originals:    map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")},
		Version:      version,
		WriterHash:   board.ReviewDigest([]byte(secret)),
	}
	raw, err := p.ReviewRuntime(t.Context(), "prepare", request.Input.RunID, "prepare", request, &proof)
	if err != nil {
		t.Fatal(err)
	}
	var prepared board.ReviewPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil || !prepared.Fresh {
		t.Fatalf("first prepare: %+v %v", prepared, err)
	}
	return preparedIdentity{f: f, p: p, request: request, proof: proof}
}

// retry sends a same-ID prepare as a separate request, so it never matches the
// receipt of the first one.
func (s preparedIdentity) retry(t *testing.T, key string, request api.ReviewPrepare, proof client.Claim) (board.ReviewPreparation, error) {
	t.Helper()
	raw, err := s.p.ReviewRuntime(t.Context(), key, request.Input.RunID, "prepare", request, &proof)
	var prepared board.ReviewPreparation
	if err == nil {
		err = json.Unmarshal(raw, &prepared)
	}
	return prepared, err
}

// records reads the two files a retry must never touch.
func (s preparedIdentity) records(t *testing.T) (run, writer []byte) {
	t.Helper()
	find := func(dir, name string) []byte {
		var found string
		err := filepath.WalkDir(s.f.dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == name && filepath.Base(filepath.Dir(path)) == dir {
				found = path
			}
			return err
		})
		if err != nil || found == "" {
			t.Fatalf("record %s/%s not found: %v", dir, name, err)
		}
		data, err := os.ReadFile(found)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return find("identity-run", "run.json"), find("review-writers", "identity-run.json")
}

func TestPrepareRetryIgnoresRequestVersion(t *testing.T) {
	s := prepareWithVersion(t, "v-a")
	runBefore, writerBefore := s.records(t)

	again := s.request
	again.Version = "v-b"
	prepared, err := s.retry(t, "retry-b", again, s.proof)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Fresh || prepared.Run.KanderVersion != "v-a" {
		t.Fatalf("retry from another version: %+v", prepared)
	}
	runAfter, writerAfter := s.records(t)
	if string(runBefore) != string(runAfter) || string(writerBefore) != string(writerAfter) {
		t.Fatal("a retry from another version rewrote the run or its writer record")
	}
	var run board.ReviewRun
	if err := json.Unmarshal(runAfter, &run); err != nil || run.KanderVersion != "v-a" {
		t.Fatalf("recorded version changed: %+v %v", run, err)
	}
}

func TestPrepareRetryStillRejectsChangedInput(t *testing.T) {
	cases := map[string]func(*api.ReviewPrepare){
		"originals": func(r *api.ReviewPrepare) {
			r.Originals = map[string][]byte{"task-context.md": []byte("changed intent"), "review-context.md": []byte("context")}
		},
		"requirements": func(r *api.ReviewPrepare) {
			r.Requirements = map[string]string{"PMQA": "required", "Security": "required"}
		},
		"advance": func(r *api.ReviewPrepare) {
			r.Advance = &board.ReviewAdvance{PreviousTarget: strings.Repeat("a", 40), Target: strings.Repeat("b", 40), Reason: "moved"}
		},
		"model":  func(r *api.ReviewPrepare) { r.Input.Model = "other" },
		"commit": func(r *api.ReviewPrepare) { r.Input.Commit = strings.Repeat("c", 40) },
	}
	s := prepareWithVersion(t, "v-a")
	runBefore, writerBefore := s.records(t)
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			request := s.request
			request.Originals = maps.Clone(request.Originals)
			request.Requirements = maps.Clone(request.Requirements)
			request.Version = "v-b"
			change(&request)
			_, err := s.retry(t, "changed-"+name, request, s.proof)
			expectCode(t, err, api.CodeIdempotencyMismatch)
			runAfter, writerAfter := s.records(t)
			if string(runBefore) != string(runAfter) || string(writerBefore) != string(writerAfter) {
				t.Fatal("a rejected retry rewrote the run or its writer record")
			}
		})
	}
}

func TestPrepareRetryKeepsWriterBinding(t *testing.T) {
	s := prepareWithVersion(t, "v-a")

	mismatched := s.request
	mismatched.Version = "v-b"
	mismatched.WriterHash = board.ReviewDigest([]byte(strings.Repeat("b", 64)))
	prepared, err := s.retry(t, "hash-mismatch", mismatched, s.proof)
	expectCode(t, err, api.CodeInvalidRequest)
	if prepared.Run.RunID != "" {
		t.Fatalf("a mismatched writer hash returned the run: %+v", prepared)
	}

	foreign := s.proof
	foreign.ReviewWriter = strings.Repeat("b", 64)
	other := s.request
	other.Version = "v-b"
	other.WriterHash = board.ReviewDigest([]byte(foreign.ReviewWriter))
	prepared, err = s.retry(t, "foreign-secret", other, foreign)
	expectCode(t, err, api.CodeHeldByOther)
	if prepared.Run.RunID != "" {
		t.Fatalf("a foreign writer secret returned the run: %+v", prepared)
	}
}

func TestPrepareRetryAcceptsDigestWrittenByOlderBinary(t *testing.T) {
	const older = "v2.0.0-dev+a658b2c"
	s := prepareWithVersion(t, older)
	_, writerRecord := s.records(t)

	// The digest an older binary wrote: the whole request, version included,
	// with the working directory the server normalizes before comparing.
	legacy := s.request
	legacy.Input.CWD = "project:" + s.p.ID()
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append([]byte("review-prepare\n"), data...))
	var writer struct {
		PrepareDigest string `json:"prepare_digest"`
	}
	if err := json.Unmarshal(writerRecord, &writer); err != nil || writer.PrepareDigest != hex.EncodeToString(sum[:]) {
		t.Fatalf("writer digest %q is not the pre-change digest: %v", writer.PrepareDigest, err)
	}

	again := s.request
	again.Version = "v2.0.0-dev+07e79ad"
	prepared, err := s.retry(t, "retry-newer", again, s.proof)
	if err != nil || prepared.Fresh {
		t.Fatalf("retry of a run an older binary prepared: %+v %v", prepared, err)
	}
}
