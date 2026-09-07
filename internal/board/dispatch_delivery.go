package board

import (
	"errors"
	"github.com/dualface/kander/internal/fs"
)

// WithDispatchDelivery serializes transport attempts, never an Agent session.
// This OS-owned lease is acquired before board locks and released on process
// death. Receipt writers never acquire it and can acknowledge during a send.
func WithDispatchDelivery(root, id string, fn func() error) (err error) {
	if !validDispatchID(id) {
		return dispatchError(id)
	}
	if err = ensureControl(root); err != nil {
		return err
	}
	file, err := fs.OpenLockFile(root, control(root, "locks", "dispatch-"+id+".lock"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	return fn()
}

// ReadExecutionSnapshot refuses to lend a newer execution's cursor to a late
// transport continuation. Callers must retain the authorization they began with.
func ReadExecutionSnapshot(root, task string, a ExecutionAuthorization) (s Snapshot, err error) {
	err = WithTransaction(root, LockScope{Tasks: []string{task}, ReadOnly: true}, func(tx *Transaction) error {
		var e error
		s, e = tx.Snapshot(task)
		if e != nil {
			return e
		}
		return tx.requireExecution(s, a, false)
	})
	return
}
