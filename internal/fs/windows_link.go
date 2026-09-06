//go:build windows

package fs

import (
	"errors"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsBusyFile reports whether err means the target executable is occupied and cannot be replaced in place.
func IsBusyFile(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION, windows.ERROR_ACCESS_DENIED:
			return true
		}
	}
	return false
}

// CreateRelativeSymlink creates a file symlink whose target is a single path component.
func CreateRelativeSymlink(root, path, target string) error {
	if err := validateLinkTarget(target); err != nil {
		return err
	}
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return failClosed("symlink", candidate, "protected path cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	existing, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		return err
	}
	if existing != 0 {
		closeHandle(existing)
		return existError("symlink", candidate, "protected path already exists")
	}
	if err := windows.CreateSymbolicLink(windows.StringToUTF16Ptr(candidate), windows.StringToUTF16Ptr(target), windows.SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE); err != nil {
		return wrap("symlink", candidate, err)
	}
	return nil
}

// CreateRelativeHardLink creates a hard link to an existing file in the same directory.
func CreateRelativeHardLink(root, path, target string) error {
	if err := validateLinkTarget(target); err != nil {
		return err
	}
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return failClosed("link", candidate, "protected path cannot be the root")
	}
	parentPath := filepath.Dir(candidate)
	_, parent, cleanup, err := openChain(rootAbs, parentPath, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	existing, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		return err
	}
	if existing != 0 {
		closeHandle(existing)
		return existError("link", candidate, "protected path already exists")
	}
	source := filepath.Join(parentPath, target)
	sourceHandle, err := tryOpenLeaf(parent, target, source, windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
	if err != nil {
		return err
	}
	if sourceHandle == 0 {
		return notExistError("link", source, "protected path does not exist")
	}
	closeHandle(sourceHandle)
	if err := windows.CreateHardLink(windows.StringToUTF16Ptr(candidate), windows.StringToUTF16Ptr(source), 0); err != nil {
		return wrap("link", candidate, err)
	}
	return nil
}

// RemoveNonDirectoryIfExists unlinks a regular file or reparse point. Directories are rejected.
func RemoveNonDirectoryIfExists(root, path string) (bool, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return false, err
	}
	if len(parts) == 0 {
		return false, failClosed("remove", candidate, "protected path cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return false, err
	}
	defer cleanup()
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if handle == 0 {
		return false, nil
	}
	defer closeHandle(handle)
	info, err := attributeInfo(handle, candidate)
	if err != nil {
		return false, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false, failClosed("remove", candidate, "refusing to remove a directory")
	}
	if err := deleteHandle(handle, candidate, true); err != nil {
		return false, err
	}
	return true, nil
}

func validateLinkTarget(target string) error {
	if target == "" || target != filepath.Base(target) || target == "." || target == ".." {
		return failClosed("link", target, "link target must be a single relative name")
	}
	return validateComponent(target, target)
}
