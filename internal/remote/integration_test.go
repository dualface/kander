package remote_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

func TestIntegrationPushesReviewedSourceAcrossClonesAndReplaysReceipt(t *testing.T) {
	f := newIntegrationFixture(t)
	if _, err := f.b.FetchDelivery(t.Context(), f.second, f.task.TaskID, f.commit); err != nil {
		t.Fatal(err)
	}
	result, err := f.a.Integrate(t.Context(), f.first, f.options("integrate-reviewed"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Phase != "integrated" || result.Status != api.ClaimReleased || result.ObservedTarget != f.commit {
		t.Fatalf("completion: %+v", result)
	}
	if got := deliveryGit(t, f.second, "ls-remote", "--refs", "origin", "refs/heads/v2"); !strings.HasPrefix(got, f.commit+"\t") {
		t.Fatal(got)
	}
	reopened := *f.a
	again, err := reopened.Integrate(t.Context(), f.first, f.options("integrate-reviewed"))
	if err != nil || again.Revision != result.Revision || again.Generation != result.Generation {
		t.Fatalf("replay: %+v %v", again, err)
	}
	current, err := f.a.Project.Show(t.Context(), f.task.TaskID)
	if err != nil || current.Revision != f.task.Revision {
		t.Fatal("integration wrote card content")
	}
	credentials, err := remote.Credentials(f.a.Home)
	if err != nil || len(credentials) != 1 || credentials[0].ResourceKind != "" {
		t.Fatalf("integration secret not removed: %+v %v", credentials, err)
	}
	for _, dir := range []string{f.first, f.second} {
		if _, err := os.Stat(filepath.Join(dir, "kanban")); !os.IsNotExist(err) {
			t.Fatal("integration created a client board")
		}
	}
}
func TestIntegrationMissingDeliveryRefBlocksEvenWithCachedCommit(t *testing.T) {
	f := newIntegrationFixture(t)
	if _, err := f.b.FetchDelivery(t.Context(), f.second, f.task.TaskID, f.commit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.first, "push", "origin", ":"+f.task.Delivery.Ref)
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("missing-delivery")); err == nil {
		t.Fatal("cached object concealed missing delivery")
	}
	if _, err := f.a.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("missing ref acquired target: %v", err)
	}
	if got := deliveryGit(t, f.second, "ls-remote", "--refs", "origin", "refs/heads/v2"); !strings.HasPrefix(got, f.base+"\t") {
		t.Fatal("missing delivery changed target")
	}
}
func TestIntegrationRejectsDivergentRemoteBeforeTargetClaim(t *testing.T) {
	f := newIntegrationFixture(t)
	if err := os.WriteFile(filepath.Join(f.second, "other.txt"), []byte("foreign branch work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "add", "other.txt")
	deliveryGit(t, f.second, "commit", "-m", "Concurrent target work")
	other := deliveryGit(t, f.second, "rev-parse", "HEAD")
	deliveryGit(t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("divergent-delivery")); err == nil {
		t.Fatal("divergent target accepted")
	}
	if got := deliveryGit(t, f.first, "ls-remote", "--refs", "origin", "refs/heads/v2"); !strings.HasPrefix(got, other+"\t") {
		t.Fatal("divergent target overwritten")
	}
	if _, err := f.a.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("invalid ancestry claimed target: %v", err)
	}
}
func TestIntegrationRecoversCommittedConfirmationAfterResponseLoss(t *testing.T) {
	f := newIntegrationFixture(t)
	var hidden atomic.Bool
	hidden.Store(true)
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if !hidden.Load() || r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, api.IntegrationID("refs/heads/v2")) {
			return false
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		var command api.IntegrationCommand
		if json.Unmarshal(data, &command) != nil || command.Action != "confirm" {
			return false
		}
		recorded := httptest.NewRecorder()
		next.ServeHTTP(recorded, r)
		if recorded.Code != http.StatusOK {
			t.Errorf("confirmation before dropped response: %d %s", recorded.Code, recorded.Body.String())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
	options := f.options("lost-confirmation")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("confirmation response was not hidden")
	}
	stored, err := f.b.Project.Integration(t.Context(), api.IntegrationID(options.TargetRef))
	if err != nil || stored.Phase != "integrated" {
		t.Fatalf("server completion: %+v %v", stored, err)
	}
	hidden.Store(false)
	// A receipt remains recoverable after the source task no longer authorizes
	// new integration writes. The original push is not repeated.
	if _, err := f.a.Project.Move(t.Context(), "later-cancellation", f.task.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "fixture cancellation after integration", Decision: "fixture decision"}, f.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	if err := remote.RemoveCredential(f.a.Home, f.credential); err != nil {
		t.Fatal(err)
	}
	reopened := *f.a
	result, err := reopened.Integrate(t.Context(), f.first, options)
	if err != nil || result.Generation != stored.Generation || result.Phase != "integrated" {
		t.Fatalf("read-only completion recovery: %+v %v", result, err)
	}
}
