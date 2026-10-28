//go:build !windows

package install

func detectPackaged() bool { return false }
