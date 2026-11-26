package notify

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func notificationTempRoot() (string, error) {
	var buf [32768]uint16
	length, err := windows.GetTempPath(uint32(len(buf)), &buf[0])
	if err != nil {
		return "", err
	}
	if length == 0 || length >= uint32(len(buf)) {
		return "", errors.New("invalid Windows temporary directory")
	}
	return filepath.Clean(windows.UTF16ToString(buf[:length])), nil
}
