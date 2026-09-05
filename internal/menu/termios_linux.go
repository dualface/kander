//go:build linux

package menu

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios       = unix.TCGETS
	ioctlWriteTermios      = unix.TCSETS
	ioctlWriteTermiosDrain = unix.TCSETSW
)
