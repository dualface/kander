package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func (s *SummaryIndex) refreshLocked(ctx context.Context, strong bool) (BoardView, SummaryStats, error) {
	stats := SummaryStats{Strong: strong}
	var warnings WarningLog
	var locks lockSet
	defer func() { _ = locks.close() }()
	scanned, selected, err := captureCommittedScan(ctx, s.root, nil, &locks, &warnings)
	if err != nil {
		return BoardView{}, stats, err
	}
	live := make(map[string]Entry, len(scanned.Entries))
	for id, entry := range scanned.Entries {
		live[id] = entry
	}
	next := make(map[string]cachedCard, len(live))
	changed := s.rebuild
	for _, id := range selected {
		if err = ctx.Err(); err != nil {
			return BoardView{}, stats, err
		}
		entry, ok := live[id]
		if !ok {
			continue
		}
		version, err := revision(s.root, id)
		if err != nil {
			return BoardView{}, stats, err
		}
		finger, err := documentFingerprint(entry, version)
		if err != nil {
			return BoardView{}, stats, err
		}
		cached, have := s.cards[id]
		_, dirty := s.dirty[id]
		needBody := strong || s.rebuild || dirty || !have || cached.finger != finger
		if !needBody {
			next[id] = cached
			continue
		}
		text, err := readDocument(entry)
		if err != nil {
			return BoardView{}, stats, err
		}
		stats.DocumentReads++
		digest := sha256Hex(text)
		if have && !s.rebuild && !dirty && cached.finger == finger && cached.digest == digest {
			next[id] = cached
			continue
		}
		entry = attachSize(entry, text)
		stats.Parses++
		changed = true
		next[id] = cachedCard{
			summary: TaskSummaryOf(entry, text),
			finger:  finger,
			digest:  digest,
		}
	}
	if !s.rebuild {
		for id := range s.cards {
			if _, ok := live[id]; !ok {
				changed = true
				break
			}
		}
	} else {
		changed = true
	}
	s.cards = next
	stats.Cards = len(s.cards)
	if !changed {
		stats.Reused = true
		view := s.view
		view.Warnings = warnings.Messages()
		view.Root = s.root
		return view, stats, nil
	}
	tasks := make([]TaskSummary, 0, len(s.cards))
	for _, card := range s.cards {
		tasks = append(tasks, card.summary)
	}
	sortTaskSummaries(tasks)
	return BoardView{
		Warnings:    warnings.Messages(),
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Root:        s.root,
		Tasks:       tasks,
	}, stats, nil
}

func documentFingerprint(entry Entry, revision uint64) (cardFingerprint, error) {
	file, err := fs.OpenRegularFileIfExists(boardRootFromEntry(entry), entry.Document)
	if err != nil {
		return cardFingerprint{}, err
	}
	if file == nil {
		return cardFingerprint{}, kanbanError("board.large_task_is_missing_spec_md", entry.Path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return cardFingerprint{}, err
	}
	volume, index, err := fileIndex(file, info)
	if err != nil {
		return cardFingerprint{}, err
	}
	form := "file"
	if entry.IsDirectory() {
		form = "dir"
	}
	return cardFingerprint{
		state:    entry.State,
		path:     entry.Path,
		form:     form,
		revision: revision,
		size:     info.Size(),
		mtime:    info.ModTime().UnixNano(),
		volume:   volume,
		index:    index,
	}, nil
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
