//go:build !windows

package launch

import "os/exec"

func applyConsoleAttr(cmd *exec.Cmd) {}
