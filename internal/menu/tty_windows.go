//go:build windows

package menu

import (
	"os"

	"golang.org/x/sys/windows"
)

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(f.Fd()), &mode)
	return err == nil
}
