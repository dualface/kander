//go:build unix

package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

const (
	upgradeGroup = "20261118-upgrade-group"
	upgradeBase  = "3333333333333333333333333333333333333333"
	upgradeHead  = "4444444444444444444444444444444444444444"
)

// sentPrepare is a prepare request as the server received it.
type sentPrepare struct {
	publication string
	body        []byte
}

// legacyHistoryName is the replay record name of a client from before the
// version left the key: a hash of the operation and the whole material.
func legacyHistoryName(t *testing.T, request api.ReviewPrepare) string {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var material any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&material); err != nil {
		t.Fatal(err)
	}
	logical, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Operation string
		Material  json.RawMessage
	}{"review-prepare", logical})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ".json"
}

// TestPrepareAfterUpgradeReusesJointPublication prepares a run of a plan whose
// two cards are held by two clients, then prepares it again as a client of
// another version through the store, which reads the recorded version itself.
func TestPrepareAfterUpgradeReusesJointPublication(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "current history", true: "history of an older client"}[legacy], func(t *testing.T) {
			t.Setenv(remote.EnvExecutionID, "")
			t.Setenv(remote.EnvCoordinatorExecutionID, "")
			t.Setenv(config.EnvLang, "en")
			t.Setenv(config.EnvLangCLI, "1")
			dataDir := filepath.Join(t.TempDir(), "server")
			srv, err := server.Open(server.Options{DataDir: dataDir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			// The retry must send the frozen material of the first prepare.
			var mu sync.Mutex
			var prepares []sentPrepare
			handler := srv.Handler()
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews/runtime/prepare") {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					mu.Lock()
					prepares = append(prepares, sentPrepare{publication: r.Header.Get(api.HeaderPublication), body: body})
					mu.Unlock()
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
			a, _ := client.New(httpServer.URL, "host-a")
			b, _ := client.New(httpServer.URL, "host-b")
			info, err := a.Register(t.Context(), "upgrade-plan", "")
			if err != nil {
				t.Fatal(err)
			}
			var sessions []*remote.Session
			var ids []string
			for i, c := range []*client.Client{a, b} {
				s := &remote.Session{Client: c, Project: c.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: t.TempDir()}
				task, err := s.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: []string{"upgrade-one", "upgrade-two"}[i], Title: "Prepare across an upgrade", Language: "en"})
				if err != nil {
					t.Fatal(err)
				}
				text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
				text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+upgradeGroup, 1)
				text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
				if task, err = s.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
					t.Fatal(err)
				}
				if task, err = s.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
					t.Fatal(err)
				}
				grant, err := s.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: c.ClientID(), ExecutionID: "execution-" + task.TaskID, Owner: "codex"})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.SaveCredential(remote.Credential{ServerURL: httpServer.URL, ClientID: c.ClientID(), ProjectID: info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}); err != nil {
					t.Fatal(err)
				}
				sessions = append(sessions, s)
				ids = append(ids, task.TaskID)
			}
			holder, other := sessions[0], sessions[1]
			confirmFor := func(err error, task string, by *remote.Session) {
				t.Helper()
				id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
				if !api.IsCode(err, api.CodeEvidenceIncomplete) || id == "" {
					t.Fatalf("want a wait for the other holder: %v", err)
				}
				if _, err := by.ConfirmReviewPublication(t.Context(), id, task); err != nil {
					t.Fatal(err)
				}
			}

			plan := board.ReviewPlan{Schema: 1, PlanID: "upgrade-plan", Author: "coordinator", Basis: "upgrade", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "upgrade-batch", TaskIDs: ids, Base: upgradeBase, TargetCommit: upgradeHead, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}}}}
			_, err = holder.ReviewAuthority(t.Context(), "review-plan", plan, ids)
			confirmFor(err, ids[1], other)
			proof, err := holder.ReviewAuthority(t.Context(), "review-plan", plan, ids)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := holder.Project.ReviewPlan(t.Context(), client.NewKey(), plan, proof); err != nil {
				t.Fatal(err)
			}

			store, err := newRemoteReviewStore(holder)
			if err != nil {
				t.Fatal(err)
			}
			unlock, err := store.Lock("upgrade-run")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			input := board.ReviewInput{RunID: "upgrade-run", BatchID: "upgrade-batch", TaskIDs: ids, Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", Base: upgradeBase, Commit: upgradeHead, ReportLanguage: "en"}
			requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
			originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
			_, _, err = store.Prepare(input, requirements, nil, originals, "v-a")
			confirmFor(err, ids[1], other)
			run, fresh, err := store.Prepare(input, requirements, nil, originals, "v-a")
			if err != nil || !fresh || run.KanderVersion != "v-a" {
				t.Fatalf("confirmed prepare: %+v %v", run, err)
			}

			mu.Lock()
			first := prepares[len(prepares)-1]
			mu.Unlock()
			if first.publication == "" {
				t.Fatal("the joint prepare carried no publication")
			}

			var legacyPath string
			if legacy {
				// Replace the record of this client by the one an older client wrote.
				root, err := holder.LocalDataRoot()
				if err != nil {
					t.Fatal(err)
				}
				history := filepath.Join(root, "review-operations")
				entries, err := os.ReadDir(history)
				if err != nil {
					t.Fatal(err)
				}
				var published string
				for _, entry := range entries {
					path := filepath.Join(history, entry.Name())
					var recorded []string
					if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &recorded) == nil && len(recorded) > 0 {
						if publication, err := holder.Project.Publication(t.Context(), recorded[0]); err == nil && publication.Operation == "review-prepare" {
							published = recorded[0]
							if err := os.Remove(path); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if published == "" {
					t.Fatal("no replay record of the prepare")
				}
				older, err := json.Marshal([]string{published})
				if err != nil {
					t.Fatal(err)
				}
				request := api.ReviewPrepare{Input: input, Requirements: requirements, Originals: originals, Version: "v-a", WriterHash: board.ReviewDigest([]byte(store.writer))}
				request.Input.CWD = "project:" + info.ID
				legacyPath = filepath.Join(history, legacyHistoryName(t, request))
				if err := os.WriteFile(legacyPath, older, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			publications := func() int {
				count := 0
				err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() && filepath.Base(filepath.Dir(path)) == "publications" && strings.HasSuffix(path, ".json") {
						count++
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				return count
			}
			requests := func() int {
				all, err := holder.Project.Requests(t.Context(), ids[1])
				if err != nil {
					t.Fatal(err)
				}
				return len(all)
			}
			// Each retry is a new request that carries the first prepare's
			// publication and exact body, and it adds no publication or request.
			retry := func(version string) {
				t.Helper()
				mu.Lock()
				sentBefore := len(prepares)
				mu.Unlock()
				beforePublications, beforeRequests := publications(), requests()
				again, fresh, err := store.Prepare(input, requirements, nil, originals, version)
				if err != nil || fresh || again.KanderVersion != "v-a" {
					t.Fatalf("prepare as %s: %+v fresh=%v %v", version, again, fresh, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(prepares) != sentBefore+1 {
					t.Fatalf("prepare as %s sent %d requests", version, len(prepares)-sentBefore)
				}
				retried := prepares[len(prepares)-1]
				if retried.publication != first.publication || !bytes.Equal(retried.body, first.body) {
					t.Fatalf("prepare as %s did not send the frozen material of publication %s: %s", version, first.publication, retried.publication)
				}
				if publications() != beforePublications || requests() != beforeRequests {
					t.Fatalf("prepare as %s created publications %d -> %d or requests %d -> %d", version, beforePublications, publications(), beforeRequests, requests())
				}
			}
			retry("v-b")
			if legacyPath != "" {
				// The older record found the publication and wrote it under the
				// current key, so the older record is no longer needed.
				if err := os.Remove(legacyPath); err != nil {
					t.Fatal(err)
				}
				retry("v-c")
			}
		})
	}
}
