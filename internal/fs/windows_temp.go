//go:build windows

package fs

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type windowsTempDir struct {
	handle windows.Handle
	anchor string
	path   string
}

// tempDirBeforeReopen runs between closing the lease and reopening the directory for removal. Tests use it to
// replace the directory; production leaves it nil.
var tempDirBeforeReopen func(path string)

func (d *windowsTempDir) close() error {
	if d == nil {
		return nil
	}
	var cleanup error
	if err := validateHandleKind(d.handle, d.path, kindDirectory); err != nil {
		cleanup = err
	} else if err := removeContentsWin(d.handle, d.path, 0, new(int)); err != nil {
		cleanup = err
	} else {
		cleanup = releaseAndDeleteTempDir(d.handle, d.anchor, d.path)
		d.handle = 0
	}
	if d.handle != 0 {
		closeHandle(d.handle)
		d.handle = 0
	}
	if cleanup != nil {
		return &pathError{Op: "cleanup", Path: d.path, Err: fmt.Errorf("%w: %v", ErrTempCleanup, cleanup)}
	}
	return nil
}

// releaseAndDeleteTempDir closes the lease of a private temporary directory and removes the directory through a
// fresh DELETE handle. The lease holds no DELETE access, so it cannot delete the directory itself. The directory is
// reopened from the volume anchor without following reparse points, and it is deleted only when the new handle
// carries the lease's identity; otherwise the directory was replaced after the lease closed and nothing is deleted.
// The lease handle is always closed.
func releaseAndDeleteTempDir(lease windows.Handle, anchor, path string) error {
	id, err := handleIdentity(lease, path)
	closeHandle(lease)
	if err != nil {
		return err
	}
	if tempDirBeforeReopen != nil {
		tempDirBeforeReopen(path)
	}
	_, handle, cleanup, err := openChain(anchor, path, windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	got, err := handleIdentity(handle, path)
	if err != nil {
		return err
	}
	if !id.equal(got) {
		return failClosed("cleanup", path, "private temporary directory identity changed before removal")
	}
	return deleteHandle(handle, path, true)
}

func removeContentsWin(directory windows.Handle, path string, depth int, seen *int) error {
	if depth > tempCleanupMaxDepth {
		return failClosed("cleanup", path, "private temporary directory cleanup depth exceeded")
	}
	for range tempCleanupMaxPasses {
		remaining := tempCleanupMaxEntries - *seen
		if remaining < 0 {
			return failClosed("cleanup", path, "private temporary directory cleanup entry budget exceeded")
		}
		names, err := listNames(directory, path, remaining)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		*seen += len(names)
		for _, name := range names {
			child := filepath.Join(path, name)
			probe, err := tryOpenLeaf(directory, name, child, windows.FILE_READ_ATTRIBUTES, kindAny, false, false)
			if err != nil {
				if isMissingWin(err) || isNotExist(err) {
					continue
				}
				return err
			}
			if probe == 0 {
				continue
			}
			info, err := attributeInfo(probe, child)
			closeHandle(probe)
			if err != nil {
				return err
			}
			isDir := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
			access := uint32(windows.DELETE | windows.FILE_READ_ATTRIBUTES)
			expected := kindFile
			if isDir {
				access |= windows.FILE_LIST_DIRECTORY
				expected = kindDirectory
			}
			h, err := tryOpenLeaf(directory, name, child, access, expected, false, false)
			if err != nil {
				if isMissingWin(err) || isNotExist(err) {
					continue
				}
				return err
			}
			if h == 0 {
				continue
			}
			if isDir {
				if err := removeContentsWin(h, child, depth+1, seen); err != nil {
					closeHandle(h)
					return err
				}
			}
			if err := deleteHandle(h, child, true); err != nil {
				closeHandle(h)
				return err
			}
			closeHandle(h)
		}
	}
	return failClosed("cleanup", path, "private temporary directory cleanup did not become stable")
}

func CreatePrivateTempDir(parent, prefix string) (*TempDir, error) {
	parentAbs, err := absolutePath(parent)
	if err != nil {
		return nil, err
	}
	anchor, err := pathAnchor(parentAbs)
	if err != nil {
		return nil, err
	}
	_, parentHandle, cleanup, err := openChain(anchor, parentAbs, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	for range tempNameAttempts {
		name := prefix + randomHex(16)
		candidate := filepath.Join(parentAbs, name)
		// The lease handle lives until Close, so shared write is required: when renameHandle renames the temporary file into
		// that directory, the kernel opens the target parent directory for write, which is a sharing violation without shared write.
		// The lease holds no DELETE access: another process setting the directory as its working directory opens it without
		// FILE_SHARE_DELETE, which would conflict with a lease holding DELETE. Delete is not shared either, so while the lease
		// is open no other handle can rename or delete the directory. Close reopens it with DELETE after releasing the lease.
		handle, err := openRelative(
			parentHandle, name, candidate,
			windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES,
			true, kindDirectory, true, false, kindDirectory,
		)
		if err != nil {
			if isExistWin(err) || isExist(err) {
				continue
			}
			return nil, err
		}
		if err := tightenPrivateHandle(handle, candidate, kindDirectory); err != nil {
			_ = releaseAndDeleteTempDir(handle, anchor, candidate)
			return nil, err
		}
		return &TempDir{Path: candidate, impl: &windowsTempDir{handle: handle, anchor: anchor, path: candidate}}, nil
	}
	return nil, existError("mkdir", parentAbs, "cannot allocate unique private directory")
}
