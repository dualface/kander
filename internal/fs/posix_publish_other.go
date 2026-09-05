//go:build unix && !linux && !darwin

package fs

import "golang.org/x/sys/unix"

func exclusivePublish(dirfd int, tempName, destName, path string) error {
	if err := unix.Linkat(dirfd, tempName, dirfd, destName, 0); err != nil {
		if err == unix.EEXIST {
			return existError("write", path, "protected file already exists")
		}
		return mapOpenErr("link", path, err)
	}
	_ = unix.Unlinkat(dirfd, tempName, 0)
	return nil
}
