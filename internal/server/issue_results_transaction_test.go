package server

import (
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/issue"
)

func TestIssueResultPartialRedoRecoversStateAndExactReceipt(t *testing.T) {
	for _, restore := range []string{"state", "receipt"} {
		t.Run(restore, func(t *testing.T) {
			f := newIssueResultFixture(t)
			query := api.IssueResultQuery{SourceKey: f.sourceKey, TaskID: f.cardID}
			state, err := f.a.Project.IssueResult(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			var work issue.ResultWork
			var store issue.ResultStore
			if err := json.Unmarshal(state.Work, &work); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(state.Store, &store); err != nil {
				t.Fatal(err)
			}
			store.IssueID = 42
			raw, err := json.Marshal(store)
			if err != nil {
				t.Fatal(err)
			}
			input := api.IssueResultWrite{Query: query, ExpectedRevision: state.Revision, WorkDigest: work.Digest, Store: raw}
			key := "result-redo-recovery"
			saved, err := f.a.Project.SaveIssueResult(t.Context(), key, input)
			if err != nil {
				t.Fatal(err)
			}
			p, err := f.srv.project(f.infoID)
			if err != nil {
				t.Fatal(err)
			}
			receiptRel, err := filepath.Rel(p.board, filepath.Join(p.root, receiptPath(key)))
			if err != nil {
				t.Fatal(err)
			}
			var record board.OperationRecord
			var committed string
			journalRoot := filepath.Join(p.board, ".kander", "operations")
			err = filepath.WalkDir(journalRoot, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var candidate board.OperationRecord
				if json.Unmarshal(data, &candidate) != nil {
					return nil
				}
				for _, file := range candidate.Files {
					if filepath.FromSlash(file.Path) == receiptRel {
						record, committed = candidate, path
					}
				}
				return nil
			})
			if err != nil || committed == "" {
				t.Fatalf("produced transaction not found: %v", err)
			}
			f.http.Close()
			dataDir := f.srv.data
			if err := f.srv.Close(); err != nil {
				t.Fatal(err)
			}
			// Recreate a partial application of the exact production redo, with one
			// effect already present and its paired state/receipt still at Before.
			restored := false
			for _, file := range record.Files {
				isReceipt := filepath.FromSlash(file.Path) == receiptRel
				if (restore == "receipt") != isReceipt {
					continue
				}
				path := filepath.Join(p.board, filepath.FromSlash(file.Path))
				if file.Before == nil {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte(*file.Before), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				restored = true
			}
			if !restored {
				t.Fatal("no paired redo effect restored")
			}
			record.Phase = "prepared"
			pending := filepath.Join(journalRoot, "pending", record.ID+".json")
			if err := os.MkdirAll(filepath.Dir(pending), 0700); err != nil {
				t.Fatal(err)
			}
			raw, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pending, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(committed); err != nil {
				t.Fatal(err)
			}
			f.srv, err = Open(Options{DataDir: dataDir})
			if err != nil {
				t.Fatal(err)
			}
			f.http = httptest.NewServer(f.srv.Handler())
			cl, err := client.New(f.http.URL, "result-a")
			if err != nil {
				t.Fatal(err)
			}
			project := cl.Project(f.infoID)
			replay, err := project.SaveIssueResult(t.Context(), key, input)
			if err != nil || replay.Revision != saved.Revision {
				t.Fatalf("partial redo repeated producer instead of recovering its exact result: %+v %v", replay, err)
			}
			current, err := project.IssueResult(t.Context(), query)
			if err != nil || current.Revision != saved.Revision {
				t.Fatalf("recovered state differs from receipt: %+v %v", current, err)
			}
		})
	}
}
