package server

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/server/taskindex"
)

const indexIOTimeout = 2 * time.Minute

func rebuildingError() *api.Error {
	return apiError(api.CodeIndexRebuilding, i18n.Text("en", "remote.error_index_rebuilding"))
}

// indexPublication is attached to board write producers. It uses the actual
// commit set, not the request's task ID, because group and review operations
// may change multiple cards. Claim-only transactions have no card revision.
func (p *project) indexPublication(changes board.TransactionChanges, err error) {
	for _, id := range changes.Tasks {
		p.markIndexTask(id)
	}
	// Claim records are producer-owned control files inside the board journal.
	claims := filepath.ToSlash(filepath.Dir(claimPath("task"))) + "/"
	claims = strings.TrimPrefix(claims, "board/")
	for _, name := range changes.Files {
		relative, ok := strings.CutPrefix(filepath.ToSlash(name), claims)
		if ok {
			id, ok := strings.CutSuffix(relative, ".json")
			if ok && !strings.Contains(id, "/") {
				p.markIndexTask(id)
			}
		}
	}
	if err != nil && p.index != nil {
		p.invalidateIndex(err)
	}
}

func (p *project) markIndexTask(id string) {
	if p.index == nil {
		return // Startup will reconcile every row after settling the leases.
	}
	if p.indexDirty == nil {
		p.indexDirty = map[string]struct{}{}
	}
	p.indexDirty[id] = struct{}{}
}

// unlockBoard is the publication barrier for every project operation. Its
// caller still holds the project lock, so readers cannot see committed files
// paired with stale index rows, even when an operation returns a domain error.
func (p *project) unlockBoard() {
	defer p.mu.Unlock()
	if len(p.indexDirty) == 0 && !p.indexFull && !p.indexRebuilding.Load() {
		return
	}
	defer clear(p.indexDirty)
	if p.importBroken {
		p.indexRebuilding.Store(true)
		return
	}
	if p.indexFull {
		p.indexFull = false
		if err := p.reconcileIndex(); err != nil {
			p.invalidateIndex(err)
		} else {
			clear(p.indexDirty)
			p.indexRebuilding.Store(false)
		}
	}
	if !p.indexRebuilding.Load() && len(p.indexDirty) > 0 {
		if err := p.refreshIndex(); err != nil {
			p.invalidateIndex(err)
		}
	}
	if p.indexRebuilding.Load() {
		p.startIndexRebuild()
	}
}

func (p *project) invalidateIndex(err error) {
	if !p.indexRebuilding.Swap(true) {
		p.log().Error("task index is untrusted", "error", err)
	}
}

func (p *project) startIndexRebuild() {
	if p.indexRunning || p.server.shutdown.Err() != nil {
		return
	}
	p.indexRunning = true
	p.server.indexWorkers.Go(func() {
		// Give readers a stable failure while rebuilding instead of letting them
		// wait behind the board scan and use a previous index.
		var faultErr error
		if fault := p.server.indexFault; fault != nil {
			faultErr = fault(p, "rebuild")
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.indexRunning = false
		if p.server.shutdown.Err() != nil || !p.indexRebuilding.Load() || p.importBroken {
			return
		}
		if faultErr != nil {
			p.log().Error("task index rebuild failed", "error", faultErr)
			return
		}
		if err := p.reconcileIndex(); err != nil {
			p.log().Error("task index rebuild failed", "error", err)
			return
		}
		p.indexRebuilding.Store(false)
	})
}

func (p *project) refreshIndex() error {
	if fault := p.server.indexFault; fault != nil {
		if err := fault(p, "update"); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(p.server.shutdown, indexIOTimeout)
	defer cancel()
	var warnings board.WarningLog
	captured, err := board.ScanTargetsContext(ctx, p.board, slices.Collect(maps.Keys(p.indexDirty)), &warnings)
	if err != nil {
		return err
	}
	rows := make([]taskindex.Row, 0, len(p.indexDirty))
	for id := range p.indexDirty {
		row, err := p.indexRow(captured, id)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	return p.index.Apply(ctx, rows, warnings.Messages())
}

// reconcileIndex scans the canonical board once, fixes its projection in
// place, and recreates the SQLite file if the existing copy is unusable.
func (p *project) reconcileIndex() error {
	if fault := p.server.indexFault; fault != nil {
		if err := fault(p, "reconcile"); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(p.server.shutdown, indexIOTimeout)
	defer cancel()
	rows, warnings, err := p.scanIndex(ctx)
	if err != nil {
		return err
	}
	if p.index != nil {
		if err := p.index.Close(); err != nil {
			return err
		}
		p.index = nil
	}
	p.index, err = p.openIndex(ctx, rows, warnings)
	if err == nil || ctx.Err() != nil {
		return err
	}
	p.log().Warn("recreating task index", "error", err)
	if resetErr := taskindex.Reset(p.root); resetErr != nil {
		return errors.Join(err, resetErr)
	}
	p.index, err = p.openIndex(ctx, rows, warnings)
	return err
}

func (p *project) openIndex(ctx context.Context, rows []taskindex.Row, warnings []string) (*taskindex.Store, error) {
	idx, err := taskindex.Open(ctx, p.root)
	if err != nil {
		return nil, err
	}
	if err := idx.Reconcile(ctx, rows, warnings, p.server.now()); err != nil {
		return nil, errors.Join(err, idx.Close())
	}
	return idx, nil
}

// scanIndex is the canonical scan used at startup and by differential tests.
// It retains the previous list's summaries, revisions, claims and advisories.
func (p *project) scanIndex(ctx context.Context) ([]taskindex.Row, []string, error) {
	var warnings board.WarningLog
	view, err := board.ScanContext(ctx, p.board, &warnings)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]taskindex.Row, 0, len(view.Entries))
	for id := range view.Entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		row, err := p.indexRow(view, id)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	return rows, warnings.Messages(), nil
}

func (p *project) indexRow(view board.Board, id string) (taskindex.Row, error) {
	revision, err := view.Revision(id)
	if err != nil {
		return taskindex.Row{}, err
	}
	text, err := view.Document(id)
	if err != nil {
		return taskindex.Row{}, err
	}
	t := board.TaskSummaryOf(view.Entries[id], text)
	row := taskindex.Row{
		StateRank: slices.Index(board.States, t.State),
		Summary: api.TaskSummary{
			TaskID: t.TaskID, Revision: revision, Title: t.Title, State: t.State, Kind: t.Kind, Type: t.Type, Session: t.Session, Window: t.Window,
			TaskGroup: t.TaskGroup, Owner: t.Owner, Submitter: t.Submitter, Assignee: t.Assignee, CreatedAt: t.CreatedAt,
			StartedAt: t.StartedAt, CompletedAt: t.CompletedAt, Time: t.Time, Result: t.Result,
		},
	}
	r, found, err := p.readClaim(t.TaskID)
	if err == nil && found {
		view := r.storedView()
		row.Claim = &view
	}
	return row, err
}

func (p *project) lockIndex() error {
	// Do not wait for a long scan once the index is known to be untrusted.
	if p.indexRebuilding.Load() {
		return rebuildingError()
	}
	if err := p.lockBoard(); err != nil {
		return err
	}
	if p.indexRebuilding.Load() || p.index == nil {
		p.mu.Unlock()
		return rebuildingError()
	}
	return nil
}

func (p *project) indexReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.invalidateIndex(err)
	return rebuildingError()
}
