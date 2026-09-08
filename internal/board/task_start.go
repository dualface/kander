package board

import (
	"errors"
	"os"
	"reflect"

	"github.com/dualface/kander/internal/fs"
)

const taskStartGroup = "00000000-start-group"

// taskStart records one launcher attempt independently of task revisions. Its
// pending original is atomic with metadata; rollback is atomic with restoration.
type taskStart struct {
	ID               string `json:"id"`
	TaskID           string `json:"task_id"`
	PreviousID       string `json:"previous_id,omitempty"`
	Cycle            string `json:"cycle"`
	Owner            string `json:"owner"`
	Session          string `json:"session"`
	Revision         uint64 `json:"revision"`
	Status           string `json:"status"`
	FinishedRevision uint64 `json:"finished_revision,omitempty"`
}

func taskStartPath(task, id, name string) string { return task + "/" + id + "/" + name + ".json" }

func readTaskStartJSON(tx *Transaction, path string) (a taskStart, exists bool, err error) {
	b, exists, err := tx.ReadGroup(taskStartGroup, path)
	if err == nil && exists {
		err = DecodeReviewJSON(b, &a)
	}
	return
}

func readTaskStart(tx *Transaction, task, id string) (a taskStart, exists bool, err error) {
	path := task + "/current.json"
	if id != "" {
		path = taskStartPath(task, id, "result")
	}
	a, exists, err = readTaskStartJSON(tx, path)
	if err == nil && !exists && id == "" {
		entries, e := fs.ListDirectory(tx.root, control(tx.root, "groups", taskStartGroup, task))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return a, false, e
		}
		if len(entries) != 0 {
			return a, false, coordinatorError("start pointer missing with retained originals")
		}
	}
	if err != nil || !exists {
		return
	}
	if a.TaskID != task || !validDispatchID(a.ID) || id != "" && a.ID != id || a.Revision == 0 || a.Cycle == "" || a.Owner == "" || (a.Status != "pending" && a.Status != "succeeded" && a.Status != "rolled-back") {
		return a, true, coordinatorError("start attempt identity")
	}
	original, ok, e := readTaskStartJSON(tx, taskStartPath(task, a.ID, "pending"))
	if e != nil {
		return a, true, e
	}
	base := a
	base.Status, base.FinishedRevision = "pending", 0
	if !ok || !reflect.DeepEqual(original, base) {
		return a, true, coordinatorError("start attempt original")
	}
	if a.Status != "pending" {
		result, ok, e := readTaskStartJSON(tx, taskStartPath(task, a.ID, "result"))
		if e != nil {
			return a, true, e
		}
		if !ok || !reflect.DeepEqual(result, a) || a.FinishedRevision < a.Revision {
			return a, true, coordinatorError("start result original")
		}
	} else if a.FinishedRevision != 0 {
		return a, true, coordinatorError("pending start result")
	}
	return
}

func putTaskStart(tx *Transaction, a taskStart, name string) error {
	path := taskStartPath(a.TaskID, a.ID, name)
	if _, ok, err := tx.ReadGroup(taskStartGroup, path); err != nil {
		return err
	} else if ok {
		return coordinatorError("immutable start original")
	}
	if err := tx.PutGroup(taskStartGroup, path, reviewJSON(a)); err != nil {
		return err
	}
	return tx.PutGroup(taskStartGroup, a.TaskID+"/current.json", reviewJSON(a))
}

func stageTaskStart(tx *Transaction, s Snapshot, text, state string) error {
	before, after := MetadataFrom(s.Text, FieldStartedAt), MetadataFrom(text, FieldStartedAt)
	if before == after {
		return nil
	}
	a, exists, err := readTaskStart(tx, s.Entry.TaskID, "")
	if err != nil {
		return err
	}
	if before == "" && after != "" && state == "" && s.Entry.State == "working" {
		if exists && (a.Status != "rolled-back" || s.Revision < a.FinishedRevision) {
			return coordinatorError("unfinished or confirmed prior start")
		}
		previous := a.ID
		a = taskStart{ID: tx.record.ID, TaskID: s.Entry.TaskID, PreviousID: previous, Cycle: planCycle(Snapshot{Entry: s.Entry, Text: text}), Owner: MetadataFrom(text, FieldOwner), Session: MetadataFrom(text, FieldSession), Revision: s.Revision + 1, Status: "pending"}
		if a.Owner == "" {
			return coordinatorError("start owner required")
		}
		return putTaskStart(tx, a, "pending")
	}
	if exists && after == "" && state == "todo" {
		if a.Status != "pending" || a.Cycle != planCycle(s) || s.Revision < a.Revision {
			return coordinatorError("start rollback binding")
		}
		a.Status, a.FinishedRevision = "rolled-back", s.Revision+1
		return putTaskStart(tx, a, "result")
	}
	return nil
}

// ConfirmTaskStart records launcher success without overwriting or revising task
// work produced by a fast executor. The original operation cursor fences retries.
func ConfirmTaskStart(root string, entry Entry) error {
	if entry.Version == nil {
		return coordinatorError("start cursor missing")
	}
	entry.Version.mu.Lock()
	defer entry.Version.mu.Unlock()
	return WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{entry.TaskID}, warnings: entryWarningLog(entry)}, func(tx *Transaction) error {
		a, exists, err := readTaskStart(tx, entry.TaskID, "")
		if err != nil {
			return err
		}
		s, err := tx.Snapshot(entry.TaskID)
		if err != nil {
			return err
		}
		if !exists || a.Status == "rolled-back" || a.Revision > entry.Version.revision || a.Cycle != planCycle(s) || a.Owner != MetadataFrom(s.Text, FieldOwner) || a.Session != MetadataFrom(s.Text, FieldSession) {
			return coordinatorError("start success binding")
		}
		if a.Status == "succeeded" {
			return nil
		}
		a.Status, a.FinishedRevision = "succeeded", s.Revision
		return putTaskStart(tx, a, "result")
	})
}
