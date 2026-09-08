package board

import (
	"os"
	"sync"
)

// WarningLog collects deduplicated advisory messages for one caller operation.
// Its zero value is ready to use. It never invokes callbacks while locks are held.
type WarningLog struct {
	mu       sync.Mutex
	messages []string
}

// Messages returns a copy that the caller can present after the operation.
func (log *WarningLog) Messages() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.messages...)
}

func journalWarningLog(logs []*WarningLog) *WarningLog {
	if len(logs) > 0 {
		return logs[0]
	}
	return nil
}

func journalWarning(message string, logs ...*WarningLog) {
	log := journalWarningLog(logs)
	if log == nil {
		_, _ = os.Stderr.WriteString(message + "\n")
		return
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, old := range log.messages {
		if old == message {
			return
		}
	}
	log.messages = append(log.messages, message)
}

func entryWarningLog(entry Entry) *WarningLog {
	if entry.Version == nil {
		return nil
	}
	return entry.Version.warnings
}
