package server

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// withoutRemnants returns the part of a scanned source the import copies:
// every file except the leftovers of interrupted atomic writes, which are
// skipped with one warning each, in path order. The source itself, its
// snapshot digest and its file count still include them.
func withoutRemnants(src treeScan) (treeScan, []api.ImportWarning) {
	var warnings []api.ImportWarning
	files := make([]string, 0, len(src.files))
	for _, rel := range src.files {
		target, ok := board.ImportAtomicRemnant(rel)
		if !ok {
			files = append(files, rel)
			continue
		}
		warnings = append(warnings, remnantWarning(src, rel, target))
	}
	if len(warnings) == 0 {
		return src, nil
	}
	return subsetTree(src, files), warnings
}

// remnantWarning compares the leftover with its target by their scanned
// digests. Both paths are board-relative, so every import path reports the
// same text.
func remnantWarning(src treeScan, rel, target string) api.ImportWarning {
	taskIDs := []string{} // A .kander record belongs to no card: encode [], not null.
	if state, rest, _ := strings.Cut(rel, "/"); !strings.EqualFold(state, ".kander") {
		id, _, _ := strings.Cut(rest, "/")
		taskIDs = []string{id}
	}
	key := "server.import_remnant_missing"
	if sum, ok := src.sums[target]; ok {
		key = "server.import_remnant_different"
		if sum == src.sums[rel] {
			key = "server.import_remnant_same"
		}
	}
	return api.ImportWarning{TaskIDs: taskIDs, Message: config.Text(key, rel, target)}
}

// subsetTree is the scan of the given files of tree, which stay in order.
func subsetTree(tree treeScan, files []string) treeScan {
	out := treeScan{files: files, dirs: tree.dirs, sums: tree.sums, sizes: tree.sizes}
	h := sha256.New()
	for _, rel := range files {
		hashImportEntry(h, rel, tree.sums[rel])
		out.bytes += tree.sizes[rel]
	}
	out.digest = hex.EncodeToString(h.Sum(nil))
	return out
}
