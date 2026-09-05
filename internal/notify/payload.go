package notify

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
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

func notifyInstruction(entry board.Entry, messagePath, marker string) string {
	return t(
		"notify.kander_notify_read_first_output_exactly_then_handle_task", messagePath, marker, entry.TaskID,
	)
}

func ackMarker() string {
	return "KANDER-NOTIFY-ACK:" + randomHex(16)
}
