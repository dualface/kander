//go:build linux

package fs

import "golang.org/x/sys/unix"

func exclusivePublish(dirfd int, tempName, destName, path string) error {
	err := unix.Renameat2(dirfd, tempName, dirfd, destName, unix.RENAME_NOREPLACE)
	if err == unix.EEXIST {
		return existError("write", path, "protected file already exists")
	}
	return mapOpenErr("rename", path, err)
}
