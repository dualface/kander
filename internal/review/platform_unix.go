//go:build unix

package review

import "golang.org/x/sys/unix"

func applyUmask() {
	unix.Umask(0o077)
}

func windowsJobBootstrapMain(args []string) int {
	return 125
}
