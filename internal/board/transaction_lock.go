package board

import (
	"context"
	"errors"
	"github.com/dualface/kander/internal/fs"
	"os"
	"path/filepath"
	"sort"
)

func acquireContext(ctx context.Context, root string, scope LockScope) (locks lockSet, err error) {
	tasks, err := orderedIDs(scope.Tasks, false)
	if err != nil {
		return nil, err
	}
	groups, err := orderedIDs(scope.Groups, true)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = ensureControl(root); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, locks.close())
		}
	}()
	take := func(path string, shared bool) error {
		if shared {
			return locks.takeSharedContext(ctx, root, path)
		}
		f, e := fs.OpenLockFile(root, path)
		if e != nil {
			return e
		}
		l, e := fs.LockExclusiveContext(ctx, f)
		if e != nil {
			return errors.Join(e, f.Close())
		}
		locks = append(locks, heldLock{f, l})
		return nil
	}
	if err = take(control(root, "locks", "board.lock"), !scope.ExclusiveBoard); err != nil {
		return locks, err
	}
	for _, id := range append(groups, tasks...) {
		if err = take(control(root, "locks", id+".lock"), scope.ReadOnly); err != nil {
			return locks, err
		}
	}
	return locks, nil
}

// LockScope declares the complete lock set before any task is accessed. Ordering
// is board, sorted groups, sorted tasks, then the short-lived journal lock.
// Migration requires ExclusiveBoard.
type LockScope struct {
	warnings       *WarningLog
	Groups         []string
	Tasks          []string
	ExclusiveBoard bool
	ReadOnly       bool
}

type heldLock struct {
	file *os.File
	lock *fs.ExclusiveLock
}
type lockSet []heldLock

func (locks *lockSet) close() error {
	var result error
	for i := len(*locks) - 1; i >= 0; i-- {
		result = errors.Join(result, (*locks)[i].lock.Unlock(), (*locks)[i].file.Close())
	}
	*locks = nil
	return result
}
func (locks *lockSet) take(root, path string, shared bool) error {
	f, err := fs.OpenLockFile(root, path)
	if err != nil {
		return err
	}
	var l *fs.ExclusiveLock
	if shared {
		l, err = fs.LockShared(f)
	} else {
		l, err = fs.LockExclusive(f)
	}
	if err != nil {
		return errors.Join(err, f.Close())
	}
	*locks = append(*locks, heldLock{f, l})
	return nil
}
func control(root string, parts ...string) string {
	return filepath.Join(append([]string{root, ".kander"}, parts...)...)
}
func ensureControl(root string) error {
	for _, p := range []string{control(root), control(root, "locks"), control(root, "versions"), control(root, "operations"), control(root, "operations", "pending"), control(root, "operations", "committed"), control(root, "groups"), control(root, "migrations")} {
		if err := fs.EnsurePrivateDirectory(root, p, true); err != nil {
			return err
		}
	}
	return nil
}
func orderedIDs(values []string, group bool) ([]string, error) {
	values = append([]string(nil), values...)
	sort.Strings(values)
	result := values[:0]
	for _, id := range values {
		valid := taskIDRe.MatchString(id)
		if group {
			valid = taskGroupRe.MatchString(id)
		}
		if !valid {
			return nil, kanbanError("board.transaction_invalid", id)
		}
		if len(result) == 0 || result[len(result)-1] != id {
			result = append(result, id)
		}
	}
	return result, nil
}
func acquire(root string, scope LockScope) (locks lockSet, err error) {
	tasks, err := orderedIDs(scope.Tasks, false)
	if err != nil {
		return nil, err
	}
	groups, err := orderedIDs(scope.Groups, true)
	if err != nil {
		return nil, err
	}
	if err = ensureControl(root); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, locks.close())
		}
	}()
	if err = locks.take(root, control(root, "locks", "board.lock"), !scope.ExclusiveBoard); err != nil {
		return locks, err
	}
	for _, id := range append(groups, tasks...) {
		if err = locks.take(root, control(root, "locks", id+".lock"), scope.ReadOnly); err != nil {
			return locks, err
		}
	}
	return locks, nil
}
