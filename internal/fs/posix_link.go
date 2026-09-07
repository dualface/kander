//go:build unix

package fs

import (
	"golang.org/x/sys/unix"
)

// IsBusyFile reports whether err means a target file is occupied and cannot be replaced in place.
// POSIX always returns false: in-place replacement of a running binary is allowed.
func IsBusyFile(err error) bool {
	return false
}

// RemoveNonDirectoryIfExists unlinks a regular file or symlink. Directories are rejected.
func RemoveNonDirectoryIfExists(root, path string) (bool, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return false, err
	}
	defer parent.close()
	var st unix.Stat_t
	err = unix.Fstatat(parent.parentFD, parent.name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == unix.ENOENT {
		return false, nil
	}
	if err != nil {
		return false, mapOpenErr("lstat", parent.path, err)
	}
	if st.Mode&unix.S_IFMT == unix.S_IFDIR {
		return false, failClosed("remove", parent.path, "refusing to remove a directory")
	}
	if err := unix.Unlinkat(parent.parentFD, parent.name, 0); err != nil {
		if err == unix.ENOENT {
			return false, nil
		}
		return false, mapOpenErr("remove", parent.path, err)
	}
	return true, nil
}
