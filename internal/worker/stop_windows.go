//go:build windows

package worker

import "os"

// stopProcess ends the agent; Windows has no portable polite signal for a
// console process outside its group.
func stopProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
