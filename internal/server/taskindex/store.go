// Package taskindex stores the rebuildable query projection of a server board.
package taskindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/fs"
	_ "modernc.org/sqlite"
)

const (
	filename = "index.sqlite"
	format   = 1
)

const schema = `
CREATE TABLE tasks (
	task_id TEXT NOT NULL UNIQUE,
	revision TEXT NOT NULL,
	state TEXT NOT NULL,
	state_rank INTEGER NOT NULL,
	title TEXT NOT NULL,
	type TEXT NOT NULL,
	kind TEXT NOT NULL,
	task_group TEXT NOT NULL,
	submitter TEXT NOT NULL,
	assignee TEXT NOT NULL,
	owner TEXT NOT NULL,
	session TEXT NOT NULL,
	window TEXT NOT NULL,
	created_at TEXT NOT NULL,
	started_at TEXT NOT NULL,
	completed_at TEXT NOT NULL,
	display_time TEXT NOT NULL,
	result TEXT NOT NULL,
	claim TEXT NOT NULL
);
CREATE INDEX tasks_order ON tasks(state_rank, display_time DESC, task_id DESC);
CREATE INDEX tasks_state ON tasks(state);
CREATE INDEX tasks_assignee ON tasks(assignee);
CREATE INDEX tasks_submitter ON tasks(submitter);
CREATE INDEX tasks_group ON tasks(task_group);
CREATE TABLE meta (id INTEGER PRIMARY KEY CHECK (id = 1), warnings TEXT NOT NULL, reconciled_at TEXT NOT NULL);
CREATE VIRTUAL TABLE tasks_fts USING fts5(title, content='tasks', tokenize='trigram');
CREATE TRIGGER tasks_insert AFTER INSERT ON tasks BEGIN
	INSERT INTO tasks_fts(rowid, title) VALUES(new.rowid, new.title);
END;
CREATE TRIGGER tasks_delete AFTER DELETE ON tasks BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, title) VALUES('delete', old.rowid, old.title);
END;
CREATE TRIGGER tasks_update AFTER UPDATE OF title ON tasks WHEN old.title <> new.title BEGIN
	INSERT INTO tasks_fts(tasks_fts, rowid, title) VALUES('delete', old.rowid, old.title);
	INSERT INTO tasks_fts(rowid, title) VALUES(new.rowid, new.title);
END;
PRAGMA user_version = 1;
`

// Row is a task summary and the inputs needed to render its claim at read time.
// Claim has the stored status, not a time-derived one. It carries no secrets.
type Row struct {
	Summary   api.TaskSummary
	Claim     *api.ClaimView
	StateRank int
}

// Store holds one project's SQLite connection. The project lock coordinates
// its queries with canonical file operations.
type Store struct{ db *sql.DB }

var sidecars = [...]string{"-journal", "-wal", "-shm", ""}

func Open(ctx context.Context, root string) (*Store, error) {
	path := filepath.Join(root, filename)
	// SQLite owns the database and its sidecars under the private project
	// directory. Refuse links and other non-regular entries before opening.
	for _, suffix := range sidecars {
		if _, err := fs.RegularFileExists(root, path+suffix); err != nil {
			return nil, err
		}
	}
	exists, err := fs.RegularFileExists(root, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := Reset(root); err != nil {
			return nil, err
		}
		if err := fs.WriteTextAtomic(root, path, "", false); err != nil {
			return nil, err
		}
	}
	name := filepath.ToSlash(path)
	if name[0] != '/' {
		name = "/" + name // Windows drive path in a SQLite file URI.
	}
	uri := url.URL{Scheme: "file", Path: name, RawQuery: "mode=rw"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.check(ctx, exists); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func (s *Store) check(ctx context.Context, exists bool) error {
	if !exists {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return err
		}
		return tx.Commit()
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != format {
		return fmt.Errorf("unsupported task index format %d", version)
	}
	var check string
	if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return errors.New("SQLite task index failed its integrity check")
	}
	// Rank 1 checks that the FTS posting list agrees with the content table.
	_, err := s.db.ExecContext(ctx, "INSERT INTO tasks_fts(tasks_fts, rank) VALUES('integrity-check', 1)")
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// Reset removes only the replaceable database files, with no link following.
func Reset(root string) error {
	for _, suffix := range sidecars {
		if _, err := fs.RemoveRegularFileIfExists(root, filepath.Join(root, filename+suffix)); err != nil {
			return err
		}
	}
	return nil
}

const columns = `task_id, revision, state, state_rank, title, type, kind, task_group, submitter, assignee, owner,
	session, window, created_at, started_at, completed_at, display_time, result, claim`

const upsert = `INSERT INTO tasks (` + columns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(task_id) DO UPDATE SET revision=excluded.revision, state=excluded.state, state_rank=excluded.state_rank,
	title=excluded.title, type=excluded.type, kind=excluded.kind, task_group=excluded.task_group, submitter=excluded.submitter,
	assignee=excluded.assignee, owner=excluded.owner, session=excluded.session, window=excluded.window, created_at=excluded.created_at,
	started_at=excluded.started_at, completed_at=excluded.completed_at, display_time=excluded.display_time, result=excluded.result,
	claim=excluded.claim`

func put(ctx context.Context, tx *sql.Tx, row Row) error {
	t := row.Summary
	claim, err := json.Marshal(row.Claim)
	if err != nil {
		return err
	}
	// Board revisions are uint64. Decimal text preserves the entire range.
	_, err = tx.ExecContext(ctx, upsert, t.TaskID, strconv.FormatUint(t.Revision, 10), t.State, row.StateRank, t.Title, t.Type,
		t.Kind, t.TaskGroup, t.Submitter, t.Assignee, t.Owner, t.Session, t.Window, t.CreatedAt, t.StartedAt, t.CompletedAt, t.Time, t.Result, string(claim))
	return err
}

func scanRow(src interface{ Scan(...any) error }) (row Row, err error) {
	t := &row.Summary
	var claim string
	err = src.Scan(&t.TaskID, &t.Revision, &t.State, &row.StateRank, &t.Title, &t.Type, &t.Kind, &t.TaskGroup, &t.Submitter,
		&t.Assignee, &t.Owner, &t.Session, &t.Window, &t.CreatedAt, &t.StartedAt, &t.CompletedAt, &t.Time, &t.Result, &claim)
	if err != nil {
		return row, err
	}
	err = json.Unmarshal([]byte(claim), &row.Claim)
	return row, err
}

// Apply publishes one board operation's affected rows and board-wide warnings.
func (s *Store) Apply(ctx context.Context, rows []Row, warnings []string) error {
	return s.write(ctx, rows, warnings, time.Time{})
}

// Reconcile verifies every derived row, including rows missing from the board.
func (s *Store) Reconcile(ctx context.Context, rows []Row, warnings []string, now time.Time) error {
	return s.write(ctx, rows, warnings, now)
}

func (s *Store) write(ctx context.Context, rows []Row, warnings []string, checked time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old map[string]Row
	if !checked.IsZero() {
		stored, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM tasks")
		if err != nil {
			return err
		}
		old, err = readRowsByID(stored)
		if err != nil {
			return err
		}
	}
	for _, row := range rows {
		id := row.Summary.TaskID
		if prior, ok := old[id]; !ok || !reflect.DeepEqual(prior, row) {
			if err := put(ctx, tx, row); err != nil {
				return err
			}
		}
		delete(old, id)
	}
	for id := range old {
		if _, err := tx.ExecContext(ctx, "DELETE FROM tasks WHERE task_id = ?", id); err != nil {
			return err
		}
	}
	data, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO meta VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET warnings=excluded.warnings,
		reconciled_at=CASE WHEN excluded.reconciled_at='' THEN meta.reconciled_at ELSE excluded.reconciled_at END
		WHERE meta.warnings <> excluded.warnings OR (excluded.reconciled_at <> '' AND meta.reconciled_at <> excluded.reconciled_at)`,
		string(data), timeText(checked))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func timeText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func readRowsByID(src *sql.Rows) (map[string]Row, error) {
	defer src.Close()
	rows := map[string]Row{}
	for src.Next() {
		row, err := scanRow(src)
		if err != nil {
			return nil, err
		}
		rows[row.Summary.TaskID] = row
	}
	return rows, src.Err()
}

func (s *Store) Warnings(ctx context.Context) ([]string, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT warnings FROM meta WHERE id=1").Scan(&raw); err != nil {
		return nil, err
	}
	var out []string
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

// Access is the projection that a host policy may consult.
func (s *Store) Access(ctx context.Context, id string) (out api.TaskAccess, found bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT submitter, assignee, state, revision FROM tasks WHERE task_id=?", id).
		Scan(&out.Submitter, &out.Assignee, &out.State, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	return out, err == nil, err
}
