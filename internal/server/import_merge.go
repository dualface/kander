package server

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

type importStage struct {
	// source is the whole scanned source; copied is the part the import
	// copies, without the leftovers of interrupted atomic writes.
	source, copied, target treeScan
	remnants               []api.ImportWarning
	sourceFiles            []string
	sourceDirs             []string
	defaulted              map[string]string // relative spec path to normalized text
	merge                  bool
	added                  int
	tasks                  int
	existing               []string
	skipped                []api.ImportConflict
}

type mergeCard struct {
	files []string
	dirs  []string
	doc   string
}

type mergeTree struct {
	cards   map[string]*mergeCard
	records map[board.ImportRecordKey][]string
	refs    map[board.ImportRecordKey][]string
}

func catalogImport(view board.Board, root string, tree treeScan) mergeTree {
	out := mergeTree{cards: map[string]*mergeCard{}, records: map[board.ImportRecordKey][]string{}, refs: map[board.ImportRecordKey][]string{}}
	byPath := map[string]*mergeCard{}
	for id, entry := range view.Entries {
		doc, _ := filepath.Rel(root, entry.Document)
		path, _ := filepath.Rel(root, entry.Path)
		card := &mergeCard{doc: filepath.ToSlash(doc)}
		out.cards[id] = card
		byPath[filepath.ToSlash(path)] = card
	}
	cardAt := func(rel string) *mergeCard {
		state, path, ok := strings.Cut(rel, "/")
		if !ok {
			return nil
		}
		name, _, _ := strings.Cut(path, "/")
		return byPath[state+"/"+name]
	}
	for _, rel := range tree.files {
		if card := cardAt(rel); card != nil {
			card.files = append(card.files, rel)
		} else if key := board.ImportRecord(rel); key != (board.ImportRecordKey{}) && key.Kind != "runtime" {
			out.records[key] = append(out.records[key], rel)
		}
	}
	for _, rel := range tree.dirs {
		if card := cardAt(rel); card != nil {
			card.dirs = append(card.dirs, rel)
		}
	}
	for id, card := range out.cards {
		root := filepath.ToSlash(filepath.Dir(card.doc)) + "/"
		for _, rel := range card.files {
			if key := board.ImportCardRecordReference(strings.TrimPrefix(rel, root)); key != (board.ImportRecordKey{}) {
				out.refs[key] = append(out.refs[key], id)
			}
		}
	}
	return out
}

// checkImportRecordLayout refuses unknown files below .kander. The leftover
// of an interrupted atomic write is not refused: planImport skips it.
func checkImportRecordLayout(files []string) error {
	for _, rel := range files {
		top, _, _ := strings.Cut(rel, "/")
		if _, remnant := board.ImportAtomicRemnant(rel); remnant {
			continue
		}
		if strings.EqualFold(top, ".kander") && board.ImportRecord(rel) == (board.ImportRecordKey{}) {
			return importRejected(fmt.Sprintf("unrecognized or unrecovered v1 board record %q; nothing was published", rel))
		}
	}
	return nil
}

func planImportMerge(source, target string, src, dst treeScan, sourceView, targetView board.Board, skipConflicts bool) (importStage, error) {
	plan := importStage{source: src, target: dst, merge: true, tasks: len(sourceView.Entries), defaulted: map[string]string{}}
	from, into := catalogImport(sourceView, source, src), catalogImport(targetView, target, dst)

	groups := map[string][]string{}
	identical := map[string]bool{}
	conflicts := map[api.ImportConflict]struct{}{}
	blocked := map[string]bool{}
	// A cursor still present for a task with no target card is not an
	// importable claim. Treat that ID as occupied even without a card entry.
	claims := importTargetClaims(dst)
	for _, id := range slices.Sorted(maps.Keys(from.cards)) {
		card, entry := from.cards[id], sourceView.Entries[id]
		raw, err := readScannedImportFile(source, card.doc, src)
		if err != nil {
			return importStage{}, err
		}
		group, size := board.ImportCardDetails(entry, raw)
		if group != "" {
			groups[group] = append(groups[group], id)
		}
		if size != "" {
			plan.defaulted[card.doc] = size
		}
		if existing := into.cards[id]; existing != nil {
			identical[id] = sameImportFiles(src, dst, card.files, existing.files, plan.defaulted)
			blocked[id] = !identical[id]
		} else if claims[id] || targetView.Blocked[id] != "" {
			blocked[id] = true
		}
	}
	owners, err := importRecordOwners(source, src, from.records, groups, from.refs)
	if err != nil {
		return importStage{}, err
	}
	same := map[board.ImportRecordKey]bool{}
	different := map[board.ImportRecordKey]bool{}
	for key, files := range from.records {
		targetFiles, present := into.records[key]
		if !present {
			continue
		}
		same[key] = sameImportFiles(src, dst, files, targetFiles, nil)
		if !same[key] {
			different[key] = true
			conflicts[api.ImportConflict{Kind: key.Kind, ID: key.ID}] = struct{}{}
			for _, owner := range owners[key] {
				if from.cards[owner] != nil {
					blocked[owner] = true
				}
			}
		}
	}

	// Do not introduce live evidence that binds a retained target version of
	// a conflicted card to an incoming version of another card. Identical
	// records already on the target are safe to reuse. Committed operations
	// are inert history, so ownership only controls whether to copy them.
	for changed := true; changed; {
		changed = false
		for key, ids := range owners {
			if same[key] || key.Kind == "operation" || !slices.ContainsFunc(ids, func(id string) bool { return blocked[id] }) {
				continue
			}
			for _, id := range ids {
				if from.cards[id] != nil && !identical[id] && !blocked[id] {
					blocked[id] = true
					changed = true
				}
			}
		}
	}
	for id, isBlocked := range blocked {
		if isBlocked && !identical[id] {
			conflicts[api.ImportConflict{Kind: "task", ID: id}] = struct{}{}
		}
	}
	plan.skipped = slices.Collect(maps.Keys(conflicts))
	slices.SortFunc(plan.skipped, func(a, b api.ImportConflict) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID))
	})
	if len(plan.skipped) != 0 && !skipConflicts {
		var ids []string
		for _, conflict := range plan.skipped {
			ids = append(ids, conflict.Kind+" "+conflict.ID)
		}
		return importStage{}, importRejected("board import conflicts; nothing was published: " + strings.Join(ids, ", "))
	}

	added := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(from.cards)) {
		card := from.cards[id]
		switch {
		case identical[id]:
			delete(plan.defaulted, card.doc)
			plan.existing = append(plan.existing, id)
		case blocked[id]:
			delete(plan.defaulted, card.doc)
		default:
			added[id] = true
			plan.sourceFiles = append(plan.sourceFiles, card.files...)
			plan.sourceDirs = append(plan.sourceDirs, card.dirs...)
		}
	}
	for key, files := range from.records {
		if same[key] || different[key] {
			continue
		}
		ids := owners[key]
		if key.Kind != "operation" && slices.ContainsFunc(ids, func(id string) bool { return blocked[id] }) {
			continue
		}
		// Retain unowned board history and records of removed cards. When all
		// owners that could import the record were skipped, leave it behind.
		if len(ids) == 0 || slices.ContainsFunc(ids, func(id string) bool {
			return added[id] || from.cards[id] == nil && into.cards[id] == nil
		}) {
			plan.sourceFiles = append(plan.sourceFiles, files...)
		}
	}
	plan.added = len(added)
	slices.Sort(plan.sourceFiles)
	slices.Sort(plan.sourceDirs)
	return plan, nil
}

func sameImportFiles(src, dst treeScan, a, b []string, defaulted map[string]string) bool {
	if !slices.Equal(a, b) {
		return false
	}
	for _, rel := range a {
		sum := src.sums[rel]
		if text, ok := defaulted[rel]; ok {
			sum = sha256.Sum256([]byte(text))
		}
		if sum != dst.sums[rel] {
			return false
		}
	}
	return true
}

func importTargetClaims(dst treeScan) map[string]bool {
	prefix := filepath.ToSlash(filepath.Dir(servicePath("claims", "task.json"))) + "/"
	prefix = strings.TrimPrefix(prefix, "board/")
	claims := map[string]bool{}
	for _, rel := range dst.files {
		if tail, ok := strings.CutPrefix(rel, prefix); ok && !strings.Contains(tail, "/") {
			if id, ok := strings.CutSuffix(tail, ".json"); ok {
				claims[id] = true
			}
		}
	}
	return claims
}
