package tui

import (
	"os/exec"
	"runtime"
)

// openExternalURL hands one already validated URL to the platform opener as a
// direct argv element. No shell is involved, the child gets no terminal and its
// exit status is never waited on, so a missing or slow browser cannot block the
// board.
var openExternalURL = func(target string) error {
	program, args := browserCommand(target)
	command := exec.Command(program, args...)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func browserCommand(target string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{target}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		return "xdg-open", []string{target}
	}
}
