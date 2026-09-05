//go:build unix

package fs

import "syscall"

func unixUmask(mask int) int { return syscall.Umask(mask) }
