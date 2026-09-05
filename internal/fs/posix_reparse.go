//go:build unix

package fs

import "os"

// IsReparsePoint 不跟随路径, 判断它是否为 symlink.
func IsReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}
