package board

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SummaryStats reports the work of the last View call.
type SummaryStats struct {
	DocumentReads int
	Parses        int
	Strong        bool
	Reused        bool
	Cards         int
}

type cardFingerprint struct {
	state    string
	path     string
	form     string
	revision uint64
	size     int64
	mtime    int64
	volume   uint64
	index    uint64
}

type cachedCard struct {
	summary TaskSummary
	finger  cardFingerprint
	digest  string
}

type summarySnapshot struct {
	gen         uint64
	rebuild     bool
	dirty       map[string]struct{}
	cards       map[string]cachedCard
	view        BoardView
	lastStrong  time.Time
	strongEvery time.Duration
	now         time.Time
	beforeScan  func()
	afterScan   func()
}

// SummaryIndex is a process-local board summary cache isolated by canonical root.
// It is not an authorization source: writers still use ReadSnapshot/Expect/CAS.
type SummaryIndex struct {
	mu          sync.Mutex
	root        string
	cards       map[string]cachedCard
	view        BoardView
	dirty       map[string]struct{}
	rebuild     bool
	closed      bool
	gen         uint64
	lastStrong  time.Time
	strongEvery time.Duration
	now         func() time.Time
	stats       SummaryStats
	beforeScan  func()
	afterScan   func()
}

// CanonicalRoot returns the cache key for a board path without following links.
func CanonicalRoot(root string) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return "", kanbanError("board.board_directory_not_found_run_inside_a_project_or")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// StrongInterval is max(60s, the configured TUI refresh interval).
func StrongInterval(refreshSecs int) time.Duration {
	if refreshSecs > 60 {
		return time.Duration(refreshSecs) * time.Second
	}
	return 60 * time.Second
}

// NewSummaryIndex starts empty; the first View performs a coordinated full read.
func NewSummaryIndex(root string, strongEvery time.Duration) (*SummaryIndex, error) {
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return nil, err
	}
	if strongEvery <= 0 {
		strongEvery = StrongInterval(0)
	}
	return &SummaryIndex{
		root:        canonical,
		cards:       map[string]cachedCard{},
		dirty:       map[string]struct{}{},
		rebuild:     true,
		strongEvery: strongEvery,
		now:         time.Now,
	}, nil
}

// Stats returns counters from the last View.
func (s *SummaryIndex) Stats() SummaryStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// SetStrongEvery updates the strong-verification interval without rebuilding.
func (s *SummaryIndex) SetStrongEvery(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if d <= 0 {
		d = StrongInterval(0)
	}
	s.strongEvery = d
}

// StrongEvery returns the current strong-verification interval.
func (s *SummaryIndex) StrongEvery() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.strongEvery
}

// Invalidate marks task IDs (or the whole index) so the next View rereads them.
func (s *SummaryIndex) Invalidate(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.gen++
	if len(ids) == 0 {
		s.rebuild = true
		s.dirty = map[string]struct{}{}
		return
	}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			s.dirty[id] = struct{}{}
		}
	}
}

// Close releases cached cards. Further View calls fail.
func (s *SummaryIndex) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.gen++
	s.cards = nil
	s.dirty = nil
	s.view = BoardView{}
}

// View returns list summaries. Unchanged incremental rounds reuse the last
// sorted result and do not reread bodies. A due strong pass rereads bodies.
// Scan I/O runs without the index mutex so Invalidate/Close stay non-blocking.
func (s *SummaryIndex) View(ctx context.Context) (BoardView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		snap, err := s.beginView()
		if err != nil {
			return BoardView{}, err
		}
		if err = ctx.Err(); err != nil {
			return BoardView{}, err
		}
		if snap.beforeScan != nil {
			snap.beforeScan()
		}
		strong := !snap.rebuild && !snap.lastStrong.IsZero() && !snap.now.Before(snap.lastStrong.Add(snap.strongEvery))
		view, stats, next, err := refreshFrom(ctx, s.root, snap, strong)
		if snap.afterScan != nil {
			snap.afterScan()
		}
		published, pubErr := s.finishView(snap, view, stats, next, err, strong)
		if pubErr != nil {
			return BoardView{}, pubErr
		}
		if published {
			return view, nil
		}
	}
}

func (s *SummaryIndex) beginView() (summarySnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return summarySnapshot{}, kanbanError("board.transaction_conflict", "summary index closed")
	}
	return summarySnapshot{
		gen:         s.gen,
		rebuild:     s.rebuild,
		dirty:       cloneDirty(s.dirty),
		cards:       cloneCards(s.cards),
		view:        s.view,
		lastStrong:  s.lastStrong,
		strongEvery: s.strongEvery,
		now:         s.now(),
		beforeScan:  s.beforeScan,
		afterScan:   s.afterScan,
	}, nil
}

func (s *SummaryIndex) finishView(snap summarySnapshot, view BoardView, stats SummaryStats, next map[string]cachedCard, scanErr error, strong bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, kanbanError("board.transaction_conflict", "summary index closed")
	}
	if s.gen != snap.gen {
		return false, nil
	}
	s.stats = stats
	if scanErr != nil {
		return false, scanErr
	}
	s.cards = next
	s.view = view
	if strong || snap.rebuild || snap.lastStrong.IsZero() {
		s.lastStrong = s.now()
	}
	s.rebuild = false
	s.dirty = map[string]struct{}{}
	return true, nil
}

func cloneDirty(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for id := range in {
		out[id] = struct{}{}
	}
	return out
}

func cloneCards(in map[string]cachedCard) map[string]cachedCard {
	out := make(map[string]cachedCard, len(in))
	for id, card := range in {
		out[id] = card
	}
	return out
}
