//go:build darwin

package fs

import "golang.org/x/sys/unix"

func exclusivePublish(dirfd int, tempName, destName, path string) error {
	err := unix.RenameatxNp(dirfd, tempName, dirfd, destName, unix.RENAME_EXCL)
	if err == unix.EEXIST {
		return existError("write", path, "protected file already exists")
	}
	return mapOpenErr("rename", path, err)
}
