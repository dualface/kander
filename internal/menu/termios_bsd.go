//go:build darwin || freebsd || netbsd || openbsd

package menu

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios       = unix.TIOCGETA
	ioctlWriteTermios      = unix.TIOCSETA
	ioctlWriteTermiosDrain = unix.TIOCSETAW
)
