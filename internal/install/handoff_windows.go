//go:build windows

package install

import (
	"os"
	"os/exec"
)

func handoff(dest, lang string) error {
	cmd := exec.Command(dest, "--lang", lang)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()
	code := 1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil && cmd.ProcessState == nil {
		return err
	}
	os.Exit(code)
	return nil
}
