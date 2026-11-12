package server

import (
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

func openCreationServer(t *testing.T, data string, now func() time.Time) (*Server, *httptest.Server, *client.Project) {
	t.Helper()
	srv, err := Open(Options{DataDir: data, Now: now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewUnstartedServer(srv.Handler())
	httpServer.Config.ErrorLog = log.New(io.Discard, "", 0)
	httpServer.Config.SetKeepAlivesEnabled(false)
	httpServer.Start()
	connection, err := client.New(httpServer.URL, "creation-client")
	if err != nil {
		t.Fatal(err)
	}
	connection.Retries = 0
	info, err := connection.Register(t.Context(), "creation-recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	return srv, httpServer, connection.Project(info.ID)
}

func TestCreationRetryKeepsItsOriginalSnapshotAfterInterruptionAndLaterEdits(t *testing.T) {
	for _, change := range []string{"title-trim", "renamed", "moved"} {
		t.Run(change, func(t *testing.T) {
			data := t.TempDir()
			now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
			srv, httpServer, project := openCreationServer(t, data, func() time.Time { return now })
			createdChannel := make(chan api.Task, 1)
			// This is the production boundary after card publication and before the
			// HTTP operation's fallback finish. A handler interruption skips finish.
			srv.createFault = func(created api.Task) { createdChannel <- created; panic("injected creation interruption") }
			request := api.NewTask{Type: "chore", Slug: "creation-receipt", Title: "  Original title  ", Language: "en"}
			if _, err := project.NewTask(t.Context(), "creation-key", request); err == nil {
				t.Fatal("interrupted response appeared successful")
			}
			original := <-createdChannel
			httpServer.Close()
			if err := srv.Close(); err != nil {
				t.Fatal(err)
			}
			// A later service clock and mutable card fields cannot change operation
			// identity or its exact original answer.
			now = now.Add(48 * time.Hour)
			srv, httpServer, project = openCreationServer(t, data, func() time.Time { return now })
			defer httpServer.Close()
			defer srv.Close()
			current, err := project.Show(t.Context(), original.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if change != "title-trim" {
				text := strings.ReplaceAll(current.Text, "<FILL_IN>", "filled")
				text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: creation-test", 1)
				text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
				if change == "renamed" {
					text = strings.Replace(text, "# Original title\n", "# Changed after creation\n", 1)
				}
				current, err = project.PutDocument(t.Context(), "later-document", current.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: current.Revision}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if change == "moved" {
					current, err = project.Move(t.Context(), "later-move", current.TaskID, api.Move{State: "todo"}, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			replay, err := project.NewTask(t.Context(), "creation-key", request)
			if err != nil || !reflect.DeepEqual(replay, original) {
				t.Fatalf("original creation receipt lost after %s: err=%v original=%s/r%d replay=%s/r%d", change, err, original.TaskID, original.Revision, replay.TaskID, replay.Revision)
			}
			after, err := project.Show(t.Context(), original.TaskID)
			if err != nil || after.Text != current.Text || after.Revision != current.Revision || after.State != current.State {
				t.Fatalf("retry altered later task work: %v", err)
			}
			list, err := project.List(t.Context(), "")
			if err != nil || len(list.Tasks) != 1 {
				t.Fatalf("retry duplicated creation: %d %v", len(list.Tasks), err)
			}
			changedRequest := request
			changedRequest.Title = "Different request"
			if _, err := project.NewTask(t.Context(), "creation-key", changedRequest); !api.IsCode(err, api.CodeIdempotencyMismatch) {
				t.Fatalf("changed payload reused create key: %v", err)
			}
			// The exact 201 answer belongs to the card's original creation redo,
			// independently of later writes and the HTTP response path.
			stored, err := srv.project(project.ID())
			if err != nil {
				t.Fatal(err)
			}
			var record board.OperationRecord
			found, err := readJSON(stored.root, filepath.Join("board", ".kander", "operations", "committed", original.OperationID+".json"), &record)
			if err != nil || !found {
				t.Fatalf("creation redo: %v", err)
			}
			receiptFile, err := filepath.Rel(stored.board, filepath.Join(stored.root, receiptPath("creation-key")))
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, file := range record.Files {
				if file.Path == receiptFile {
					var receipt receipt
					if err := json.Unmarshal([]byte(file.After), &receipt); err != nil {
						t.Fatal(err)
					}
					var body api.Task
					if err := json.Unmarshal(receipt.Body, &body); err != nil {
						t.Fatal(err)
					}
					matched = receipt.Status == receiptDone && receipt.HTTP == 201 && reflect.DeepEqual(body, original)
				}
			}
			if !matched {
				t.Fatal("creation redo did not include the exact original response")
			}
		})
	}
}

func TestLegacyPendingCreationRequiresInspectionInsteadOfGuessing(t *testing.T) {
	srv, httpServer, project := openCreationServer(t, t.TempDir(), time.Now)
	defer httpServer.Close()
	defer srv.Close()
	stored, err := srv.project(project.ID())
	if err != nil {
		t.Fatal(err)
	}
	request := api.NewTask{Type: "chore", Slug: "legacy-uncertain", Title: "Original title", Language: "en"}
	pending := receipt{Key: "legacy-create-key", Operation: "new", Digest: requestDigest("new", request), Status: receiptPending}
	path := receiptPath(pending.Key)
	if err := writeJSON(stored.root, path, pending); err != nil {
		t.Fatal(err)
	}
	if _, err := project.NewTask(t.Context(), pending.Key, request); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("legacy pending intent was replayed without stable evidence: %v", err)
	}
	var after receipt
	if _, err := readJSON(stored.root, path, &after); err != nil || !reflect.DeepEqual(after, pending) {
		t.Fatalf("uncertain original was overwritten: %+v %v", after, err)
	}
	list, err := project.List(t.Context(), "")
	if err != nil || len(list.Tasks) != 0 {
		t.Fatalf("legacy retry created a duplicate: %d %v", len(list.Tasks), err)
	}
	// A current pending intent is safe to resume: a committed creation would
	// already have recovered its exact receipt in the same redo record.
	pending.Schema = receiptSchema
	if err := writeJSON(stored.root, path, pending); err != nil {
		t.Fatal(err)
	}
	created, err := project.NewTask(t.Context(), pending.Key, request)
	if err != nil || created.TaskID == "" {
		t.Fatalf("uncommitted current intent did not resume: %v", err)
	}
}
