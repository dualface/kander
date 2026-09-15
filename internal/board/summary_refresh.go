package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func refreshFrom(ctx context.Context, root string, snap summarySnapshot, strong bool) (BoardView, SummaryStats, map[string]cachedCard, error) {
	stats := SummaryStats{Strong: strong}
	var warnings WarningLog
	var locks lockSet
	scanned, selected, err := captureCommittedScan(ctx, root, nil, &locks, &warnings)
	if err != nil {
		return BoardView{}, stats, nil, errors.Join(err, locks.close())
	}
	live := make(map[string]Entry, len(scanned.Entries))
	for id, entry := range scanned.Entries {
		live[id] = entry
	}
	next := make(map[string]cachedCard, len(live))
	changed := snap.rebuild
	for _, id := range selected {
		if err = ctx.Err(); err != nil {
			return BoardView{}, stats, nil, errors.Join(err, locks.close())
		}
		entry, ok := live[id]
		if !ok {
			continue
		}
		version, err := revision(root, id)
		if err != nil {
			return BoardView{}, stats, nil, errors.Join(err, locks.close())
		}
		finger, err := documentFingerprint(entry, version)
		if err != nil {
			return BoardView{}, stats, nil, errors.Join(err, locks.close())
		}
		cached, have := snap.cards[id]
		_, dirty := snap.dirty[id]
		needBody := strong || snap.rebuild || dirty || !have || cached.finger != finger
		if !needBody {
			next[id] = cached
			continue
		}
		text, err := readDocument(entry)
		if err != nil {
			return BoardView{}, stats, nil, errors.Join(err, locks.close())
		}
		stats.DocumentReads++
		digest := sha256Hex(text)
		if have && !snap.rebuild && !dirty && cached.finger == finger && cached.digest == digest {
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
	if !snap.rebuild {
		for id := range snap.cards {
			if _, ok := live[id]; !ok {
				changed = true
				break
			}
		}
	} else {
		changed = true
	}
	if err = locks.close(); err != nil {
		return BoardView{}, stats, nil, err
	}
	stats.Cards = len(next)
	if !changed {
		stats.Reused = true
		view := snap.view
		view.Warnings = warnings.Messages()
		view.Root = root
		return view, stats, next, nil
	}
	tasks := make([]TaskSummary, 0, len(next))
	for _, card := range next {
		tasks = append(tasks, card.summary)
	}
	sortTaskSummaries(tasks)
	return BoardView{
		Warnings:    warnings.Messages(),
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Root:        root,
		Tasks:       tasks,
	}, stats, next, nil
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
