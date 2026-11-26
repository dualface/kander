package fs

import (
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// CopyRegularFile copies at most limit bytes without following reparse points.
func CopyRegularFile(dst io.Writer, root, path string, limit int64) (int64, error) {
	candidate, handle, cleanup, err := openChain(root, path, windows.GENERIC_READ|windows.FILE_READ_ATTRIBUTES, kindFile)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return 0, wrap("stat", candidate, err)
	}
	// Lock files can hold a Windows byte-range lock even while empty.
	if info.FileSizeHigh == 0 && info.FileSizeLow == 0 {
		return 0, nil
	}
	proc := windows.CurrentProcess()
	var copyHandle windows.Handle
	if err := windows.DuplicateHandle(proc, handle, proc, &copyHandle, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, wrap("duplicate", candidate, err)
	}
	f := os.NewFile(uintptr(copyHandle), candidate)
	defer f.Close()
	return copyFileBytes(dst, f, limit)
}

// CreatePrivateRegularFile opens a new writable file with a protected DACL,
// through a pinned parent. The caller owns the returned file.
func CreatePrivateRegularFile(root, path string) (*os.File, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, failClosed("create", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	handle, err := openRelative(parent, filepath.Base(candidate), candidate, windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES,
		true, kindFile, false, false, kindFile)
	if isExistWin(err) || isExist(err) {
		return nil, existError("create", candidate, "protected file already exists")
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), candidate), nil
}
