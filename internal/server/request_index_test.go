package server

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

func TestRequestCardIndexRebuildsFromFormatTwo(t *testing.T) {
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	t.Cleanup(func() { h.Close(); _ = s.Close() })
	ctx := t.Context()
	c, err := client.New(h.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Register(ctx, "requests", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	task, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: "request", Title: "Request", Description: "Rebuild this projection"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj.root, "index.sqlite")
	h.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Restore the old schema and leave its ordinary rows in place.
	editTestIndex(t, path, "ALTER TABLE tasks DROP COLUMN request; PRAGMA user_version=2;")
	reopened, err := Open(Options{DataDir: dir, Logger: quietIndexLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	access, err := reopened.TaskAccess(ctx, info.ID, task.TaskID)
	if err != nil || !access.Request || access.Revision != task.Revision {
		t.Fatalf("access after rebuild: %+v, %v", access, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var format int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&format); err != nil || format != 3 {
		t.Fatalf("rebuilt index format = %d, err = %v", format, err)
	}
}
