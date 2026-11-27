//go:build unix

package server

import "syscall"

// setImportUmask sets the process umask and returns the previous one.
func setImportUmask(mask int) int { return syscall.Umask(mask) }
