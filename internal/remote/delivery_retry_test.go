package remote_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/v2/api"
)

// B11: the delivery attestation is a write that carries the holder's claim
// token and resends its derived key from a later call. When the server
// committed the first attempt but the client saw a failure, the retry with
// the same key and the same credential finds the receipt of the first attempt
// in its scope: it gets the same delivery, and the server applies it once.
func TestDeliveryRetryAfterALostResponseReplaysTheReceipt(t *testing.T) {
	f := newIntegrationGitFixture(t)
	task := f.claimedTask()
	type sent struct{ key, token string }
	var (
		mu    sync.Mutex
		posts []sent
	)
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/tasks/"+task.TaskID+"/deliveries") {
			return false
		}
		mu.Lock()
		posts = append(posts, sent{key: r.Header.Get(api.HeaderIdempotencyKey), token: r.Header.Get(api.HeaderClaimToken)})
		first := len(posts) == 1
		mu.Unlock()
		if !first {
			return false
		}
		response := httptest.NewRecorder()
		next.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Errorf("first attestation: %d %s", response.Code, response.Body.String())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
	if _, err := f.a.PublishCommit(t.Context(), f.first, task.TaskID, f.commit, f.credential); err == nil {
		t.Fatal("the lost answer reached the client")
	}
	stored, err := f.a.Project.Delivery(t.Context(), task.TaskID, f.commit)
	if err != nil {
		t.Fatalf("the first attempt did not commit: %v", err)
	}
	retried, err := f.a.PublishCommit(t.Context(), f.first, task.TaskID, f.commit, f.credential)
	if err != nil || retried != stored {
		t.Fatalf("retry = %+v %v, want the committed delivery %+v", retried, err, stored)
	}
	if len(posts) != 2 || posts[0] != posts[1] || posts[0].token != f.credential.Token {
		t.Fatalf("attempts = %+v, want one key and the holder's token twice", posts)
	}
	// A second application of the same attestation would keep the first one
	// in the delivery history.
	err = filepath.WalkDir(filepath.Join(f.root, "server"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == "delivery-history" {
			t.Errorf("the attestation was applied twice: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
