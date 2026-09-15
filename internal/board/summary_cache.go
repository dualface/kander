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
	lastStrong  time.Time
	strongEvery time.Duration
	now         func() time.Time
	stats       SummaryStats
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

// Invalidate marks task IDs (or the whole index) so the next View rereads them.
func (s *SummaryIndex) Invalidate(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
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
	s.cards = nil
	s.dirty = nil
	s.view = BoardView{}
}

// View returns list summaries. Unchanged incremental rounds reuse the last
// sorted result and do not reread bodies. A due strong pass rereads bodies.
func (s *SummaryIndex) View(ctx context.Context) (BoardView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BoardView{}, kanbanError("board.transaction_conflict", "summary index closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	strong := !s.rebuild && !s.lastStrong.IsZero() && !s.now().Before(s.lastStrong.Add(s.strongEvery))
	view, stats, err := s.refreshLocked(ctx, strong)
	s.stats = stats
	if err != nil {
		return BoardView{}, err
	}
	s.view = view
	if strong || s.rebuild || s.lastStrong.IsZero() {
		s.lastStrong = s.now()
	}
	s.rebuild = false
	s.dirty = map[string]struct{}{}
	return view, nil
}
