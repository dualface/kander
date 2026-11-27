//go:build windows

package server

// setImportUmask has no umask to set on Windows.
func setImportUmask(int) int { return 0 }
