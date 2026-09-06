//go:build unix

package fs

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// IsBusyFile reports whether err means a target file is occupied and cannot be replaced in place.
// POSIX always returns false: in-place replacement of a running binary is allowed.
func IsBusyFile(err error) bool {
	return false
}

// CreateRelativeSymlink creates a symlink whose target is a single path component, relative to a pinned parent.
func CreateRelativeSymlink(root, path, target string) error {
	if err := validateLinkTarget(target); err != nil {
		return err
	}
	parent, err := openPosixParent(root, path)
	if err != nil {
		return err
	}
	defer parent.close()
	var st unix.Stat_t
	err = unix.Fstatat(parent.parentFD, parent.name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return existError("symlink", parent.path, "protected path already exists")
	}
	if err != unix.ENOENT {
		return mapOpenErr("lstat", parent.path, err)
	}
	if err := unix.Symlinkat(target, parent.parentFD, parent.name); err != nil {
		return mapOpenErr("symlink", parent.path, err)
	}
	return nil
}

// CreateRelativeHardLink creates a hard link to an existing regular file in the same directory.
func CreateRelativeHardLink(root, path, target string) error {
	if err := validateLinkTarget(target); err != nil {
		return err
	}
	parent, err := openPosixParent(root, path)
	if err != nil {
		return err
	}
	defer parent.close()
	var st unix.Stat_t
	err = unix.Fstatat(parent.parentFD, parent.name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return existError("link", parent.path, "protected path already exists")
	}
	if err != unix.ENOENT {
		return mapOpenErr("lstat", parent.path, err)
	}
	if err := unix.Linkat(parent.parentFD, target, parent.parentFD, parent.name, 0); err != nil {
		return mapOpenErr("link", parent.path, err)
	}
	return nil
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

func validateLinkTarget(target string) error {
	if target == "" || target != filepath.Base(target) || target == "." || target == ".." {
		return failClosed("link", target, "link target must be a single relative name")
	}
	return validateComponent(target, target)
}
