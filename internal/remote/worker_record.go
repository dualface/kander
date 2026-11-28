package remote

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// WorkerRecordName is the file in the client home that the running worker
// writes about itself. It only serves diagnostics: no authorization or lease
// decision reads it.
const WorkerRecordName = "worker.json"

// workerRecordSchema is the only schema of the worker record. A change that an
// older reader cannot read as it is raises it, and the older reader then treats
// the version of the worker as unknown.
const workerRecordSchema = 1

// WorkerRecord is the content of worker.json. The producer is the worker and
// the consumers are the CLI and doctor, which can be another version of kander,
// so a field is only ever added, never changed in meaning.
type WorkerRecord struct {
	Schema int `json:"schema"`
	// Version is the version string of the binary the worker runs.
	Version string `json:"version"`
	PID     int    `json:"pid"`
	// Executable is the file the worker started from; empty when the worker
	// could not read it.
	Executable string `json:"executable"`
	// Handover reports whether the worker watches its executable and hands over
	// to a replaced file.
	Handover bool `json:"handover"`
	// Managed reports a worker a service manager started (KANDER_MANAGED=1), or the
	// successor such a worker handed over to.
	Managed   bool      `json:"managed"`
	StartedAt time.Time `json:"started_at"`
}

// WorkerRecordState says what a read of the worker record found.
type WorkerRecordState int

const (
	// WorkerRecordMissing means the file does not exist: a worker that is older
	// than the record, or one whose write failed.
	WorkerRecordMissing WorkerRecordState = iota
	// WorkerRecordRecorded means the file holds a record of the known schema.
	WorkerRecordRecorded
	// WorkerRecordUnknown means the file cannot be used: it is not valid JSON,
	// its schema is not known, or it could not be read.
	WorkerRecordUnknown
)

// WriteWorkerRecord writes the worker record of home atomically and privately.
// The schema is always the current one.
func WriteWorkerRecord(home string, r WorkerRecord) error {
	r.Schema = workerRecordSchema
	r.StartedAt = r.StartedAt.UTC()
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(home, filepath.Join(home, WorkerRecordName), string(data)+"\n", true)
}

// RemoveWorkerRecord deletes the worker record of home, if there is one. A
// worker that cannot publish its own record uses it so that the record of an
// earlier worker does not pass for its own.
func RemoveWorkerRecord(home string) error {
	_, err := fs.RemoveRegularFileIfExists(home, filepath.Join(home, WorkerRecordName))
	return err
}

// ReadWorkerRecord reads the worker record of home. Only a record of a known
// schema is WorkerRecordRecorded; unknown fields are accepted. A file that
// cannot be used is WorkerRecordUnknown and never stops the caller; err then
// says why when the file could not be read at all.
func ReadWorkerRecord(home string) (WorkerRecord, WorkerRecordState, error) {
	data, exists, err := fs.ReadRegularFileIfExists(home, filepath.Join(home, WorkerRecordName))
	if err != nil {
		return WorkerRecord{}, WorkerRecordUnknown, err
	}
	if !exists {
		return WorkerRecord{}, WorkerRecordMissing, nil
	}
	var r WorkerRecord
	if json.Unmarshal(data, &r) != nil || r.Schema != workerRecordSchema {
		return WorkerRecord{}, WorkerRecordUnknown, nil
	}
	return r, WorkerRecordRecorded, nil
}
