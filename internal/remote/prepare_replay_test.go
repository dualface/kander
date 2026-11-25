package remote_test

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

// preparedElsewhere is a review run prepared by a client of version v-a
// through a joint publication that another client confirmed.
type preparedElsewhere struct {
	t       *testing.T
	a, b    *remote.Session
	ids     []string
	dataDir string
	request api.ReviewPrepare
	secret  string
	id      string
}

func (p *preparedElsewhere) prepareRequest(version string) api.ReviewPrepare {
	request := p.request
	request.Version = version
	return request
}

func newPreparedElsewhere(t *testing.T) *preparedElsewhere {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	a, b, ids, dataDir := publicationSessionsAt(t)
	secret := strings.Repeat("a", 64)
	p := &preparedElsewhere{t: t, a: a, b: b, ids: ids, dataDir: dataDir, secret: secret}
	p.request = api.ReviewPrepare{
		Input:        board.ReviewInput{RunID: "replay-run", BatchID: "replay-batch", TaskIDs: ids, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", CWD: "project:" + a.Info.ID, Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"},
		Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"},
		Originals:    map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")},
		Version:      "v-a",
		WriterHash:   board.ReviewDigest([]byte(secret)),
	}
	_, err := a.PrepareReviewAuthority(t.Context(), p.prepareRequest("v-a"), ids, "")
	p.id = pendingPublicationID(t, err)
	if _, err := b.ConfirmReviewPublication(t.Context(), p.id, ids[1]); err != nil {
		t.Fatal(err)
	}
	proof, err := a.PrepareReviewAuthority(t.Context(), p.prepareRequest("v-a"), ids, "")
	if err != nil || proof.PublicationID != p.id {
		t.Fatalf("confirmed prepare: %+v %v", proof, err)
	}
	p.send(proof, p.prepareRequest("v-a"), true)
	return p
}

// send runs the prepare write with a proof and checks whether it launched.
func (p *preparedElsewhere) send(proof *client.Claim, request api.ReviewPrepare, fresh bool) {
	p.t.Helper()
	proof.ReviewWriter = p.secret
	raw, err := p.a.Project.ReviewRuntime(p.t.Context(), client.NewKey(), request.Input.RunID, "prepare", request, proof)
	var prepared board.ReviewPreparation
	if err == nil {
		err = json.Unmarshal(raw, &prepared)
	}
	if err != nil || prepared.Fresh != fresh || prepared.Run.KanderVersion != "v-a" {
		p.t.Fatalf("prepare write: fresh=%v want %v, version %q: %v", prepared.Fresh, fresh, prepared.Run.KanderVersion, err)
	}
}

func (p *preparedElsewhere) publications() int {
	p.t.Helper()
	count := 0
	err := filepath.WalkDir(p.dataDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(filepath.Dir(path)) == "publications" && strings.HasSuffix(path, ".json") {
			count++
		}
		return err
	})
	if err != nil {
		p.t.Fatal(err)
	}
	return count
}

func (p *preparedElsewhere) requests() int {
	p.t.Helper()
	requests, err := p.a.Project.Requests(p.t.Context(), p.ids[1])
	if err != nil {
		p.t.Fatal(err)
	}
	return len(requests)
}

// retryAs prepares the same run again as a client of another version and
// expects the completed publication back, with nothing new on the server.
func (p *preparedElsewhere) retryAs(version, recorded string) {
	p.t.Helper()
	publications, requests := p.publications(), p.requests()
	request := p.prepareRequest(version)
	proof, err := p.a.PrepareReviewAuthority(p.t.Context(), request, p.ids, recorded)
	if err != nil || proof.PublicationID != p.id {
		p.t.Fatalf("retry as %s named another publication: %+v %v", version, proof, err)
	}
	if p.publications() != publications || p.requests() != requests {
		p.t.Fatalf("retry as %s created publications %d -> %d or requests %d -> %d", version, publications, p.publications(), requests, p.requests())
	}
	// The write carries the frozen material of the first prepare, so the run
	// keeps the version it was created with.
	p.send(proof, request, false)
}

func TestPrepareRetryAfterUpgradeFindsCompletedPublication(t *testing.T) {
	p := newPreparedElsewhere(t)
	p.retryAs("v-b", "v-a")
	p.retryAs("v-c", "")
}

func TestNewPrepareKeepsRequestVersionInPublication(t *testing.T) {
	p := newPreparedElsewhere(t)
	published, err := p.a.Project.Publication(t.Context(), p.id)
	if err != nil {
		t.Fatal(err)
	}
	var payload api.ReviewPrepare
	if err := json.Unmarshal(published.Payload, &payload); err != nil || payload.Version != "v-a" {
		t.Fatalf("publication material lost the request version: %+v %v", payload, err)
	}

	// Another run is a new identity: its material names the new version and
	// gets a publication of its own.
	publications := p.publications()
	other := p.prepareRequest("v-b")
	other.Input.RunID = "replay-run-two"
	if _, err := p.a.PrepareReviewAuthority(t.Context(), other, p.ids, ""); pendingPublicationID(t, err) == p.id {
		t.Fatal("a new run reused the publication of another run")
	}
	if p.publications() != publications+1 {
		t.Fatalf("publications %d -> %d", publications, p.publications())
	}
}
