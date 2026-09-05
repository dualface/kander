//go:build windows

package menu

func needsAdmin() bool {
	return false
}
