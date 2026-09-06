//go:build windows

package install

import (
	"os"
	"os/exec"
)

func defaultHandoff(dest string, argv, env []string) error {
	cmd := exec.Command(dest, argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
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
