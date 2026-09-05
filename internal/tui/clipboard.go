package tui

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func copyViaOSC52(text string) (bool, string) {
	if text == "" {
		return false, ""
	}
	if runtime.GOOS == "windows" {
		return false, ""
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	sequence := "\033]52;c;" + encoded + "\033\\"
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return false, err.Error()
	}
	defer tty.Close()
	if _, err := tty.WriteString(sequence); err != nil {
		return false, err.Error()
	}
	return true, ""
}

var clipboardCommands = [][]string{
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
	{"pbcopy"},
	{"clip.exe"},
}

func copyToClipboard(text string) (bool, string) {
	if text == "" {
		return false, ""
	}
	payload := []byte(text)
	lastError := ""
	for _, command := range clipboardCommands {
		if _, err := exec.LookPath(command[0]); err != nil {
			continue
		}
		cmd := exec.Command(command[0], command[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			lastError = err.Error()
			continue
		}
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			lastError = err.Error()
			_ = stdin.Close()
			continue
		}
		_, _ = stdin.Write(payload)
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				return true, ""
			}
			lastError = err.Error()
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			lastError = command[0] + " timed out"
		}
	}
	ok, err := copyViaOSC52(text)
	if ok {
		return true, ""
	}
	if err != "" {
		lastError = err
	}
	return false, lastError
}
