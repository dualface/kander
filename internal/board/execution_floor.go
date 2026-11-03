package board

import (
	"context"
	"io"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// ExecutionStartFloor returns the time the pending original of a launched record
// was written. The launcher commits that original before it notes its own launch
// time, and the original is never rewritten, so no session the launch created can
// be older. A replayed recovery writes it later, which only excludes candidates.
// The original must still name the record, so a floor never belongs to another
// attempt.
func ExecutionStartFloor(root string, record ExecutionRecord) (time.Time, error) {
	var floor time.Time
	if record.Kind != ExecutionStart && record.Kind != ExecutionTakeover {
		return floor, coordinatorError("start attempt original")
	}
	err := withTransaction(context.Background(), root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{record.TaskID}, ReadOnly: true}, func(tx *Transaction) error {
		path, err := tx.groupPath(taskStartGroup, executionPath(record.TaskID, record.ID, firstOriginal(record.Kind)))
		if err != nil {
			return err
		}
		file, err := fs.OpenRegularFileIfExists(tx.root, path)
		if err != nil {
			return err
		}
		if file == nil {
			return coordinatorError("start attempt original")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		var original ExecutionRecord
		if err = DecodeReviewJSON(data, &original); err != nil {
			return err
		}
		if err = normalizeRecord(&original); err != nil {
			return err
		}
		if !sameRecordIdentity(original, record) {
			return coordinatorError("start attempt original")
		}
		floor = info.ModTime()
		return nil
	})
	return floor, err
}
