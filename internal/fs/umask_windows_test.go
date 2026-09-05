//go:build windows

package fs

func unixUmask(int) int { return 0 }
