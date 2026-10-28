package notify

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte("fallback-notify-id"))
	}
	return hex.EncodeToString(buf)
}

func writeNotifyMessage(message string) (string, error) {
	temporaryRoot, err := notificationTempRoot()
	if err != nil {
		return "", err
	}
	var last error
	for range 128 {
		dir, err := fs.CreatePrivateTempDir(temporaryRoot, "kander-notify-")
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			last = err
			continue
		}
		path := filepath.Join(dir.Path, "message.txt")
		if err := fs.WriteTextAtomic(dir.Path, path, message+"\n", false); err != nil {
			_ = dir.Close()
			return "", notifyError(
				"notify.failed_to_create_notification_message", err.Error(),
			)
		}
		return path, nil
	}
	if last != nil {
		return "", notifyError("notify.unable_to_allocate_a_notification_message_directory", last.Error())
	}
	return "", notifyError("notify.unable_to_allocate_a_notification_message_directory_2")
}

func removeNotifyMessage(path string) error {
	_ = os.Remove(path)
	return os.Remove(filepath.Dir(path))
}

// notifyInstruction renders the line pasted into the agent pane. It ends with the reminder to
// communicate in language; the message file it names never carries that reminder. The line
// never carries the acknowledgement marker: the pane echoes it whether or not the agent
// submitted it, so only the message file names the marker (see withAckRequest).
func notifyInstruction(entry board.Entry, messagePath, language string) string {
	line := t(
		"notify.kander_notify_read_first_output_exactly_then_handle_task", process.AgentPath(messagePath), entry.TaskID,
	)
	return config.WithLanguageReminder(line, language)
}

func ackMarker() string {
	return "KANDER-NOTIFY-ACK:" + randomHex(16)
}

// withAckRequest prefixes the message file body with the fixed English request to output the
// marker. The marker reaches the pane only once the agent has read this file.
func withAckRequest(marker, message string) string {
	return "Acknowledge this notification: before anything else, output this line exactly:\n" + marker + "\n\n" + message
}
