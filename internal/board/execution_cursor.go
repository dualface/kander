package board

// StagedExecutionRecord reads the producer's current record as this transaction
// will publish it. The service binds its durable launcher cursor to that ID.
func (tx *Transaction) StagedExecutionRecord(task string) (record ExecutionRecord, err error) {
	data, found, err := tx.StagedGroup(taskStartGroup, task+"/current.json")
	if err != nil {
		return record, err
	}
	if !found {
		return record, coordinatorError("start attempt missing")
	}
	if err := DecodeReviewJSON(data, &record); err != nil {
		return record, err
	}
	return record, normalizeRecord(&record)
}

// RestoreLauncherCursor reconstructs a service-held launcher after restart.
// The service must authenticate its persisted claim before calling this. An
// exact record ID and the saved content cursor prevent adopting a successor or
// erasing evidence that the executor advanced the card while the service was down.
func RestoreLauncherCursor(root string, entry Entry, recordID string, revision uint64, evidence bool) error {
	if _, err := BindExecutionCursor(root, entry); err != nil {
		return err
	}
	v := entry.Version
	v.mu.Lock()
	defer v.mu.Unlock()
	scope := executionScope(entry)
	scope.ReadOnly = true
	err := WithTransaction(root, scope, func(tx *Transaction) error {
		snapshot, err := tx.Snapshot(entry.TaskID)
		if err != nil {
			return err
		}
		ledger, err := readExecutionLedger(tx, snapshot)
		if err != nil {
			return err
		}
		current := ledger.current
		if current == nil || current.ID != recordID || !current.StartUnfinished() || revision < current.Revision || revision > snapshot.Revision {
			return coordinatorError("start recovery cursor binding")
		}
		v.fence = ExecutionFence{RecordID: current.ID, Cycle: current.Cycle, launcher: true}
		v.revision, v.advanced = revision, evidence || revision != snapshot.Revision
		return nil
	})
	return err
}
