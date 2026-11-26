package fs

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type directoryWriterImpl = *windowsDirectory

type windowsDirectory struct {
	handle  windows.Handle
	cleanup func()
}

func (d *windowsDirectory) close() error {
	if d != nil && d.cleanup != nil {
		d.cleanup()
		d.cleanup = nil
	}
	return nil
}

// OpenDirectoryWriter pins the directory path below root without following
// reparse points.
func OpenDirectoryWriter(root, path string) (*DirectoryWriter, error) {
	candidate, handle, cleanup, err := openChain(root, path, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	return &DirectoryWriter{path: candidate, impl: &windowsDirectory{handle: handle, cleanup: cleanup}}, nil
}

// CreateInherited writes data to the new file name. The file inherits the
// ACL of its directory, like the other inherited writes of this package.
func (w *DirectoryWriter) CreateInherited(name string, data []byte) error {
	path := filepath.Join(w.path, name)
	if !validChildName(name) {
		return failClosed("create", path, "invalid file name")
	}
	handle, err := openRelative(w.impl.handle, name, path, windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES,
		true, kindFile, false, false, kindAny)
	if isExistWin(err) || isExist(err) {
		return existError("create", path, "protected file already exists")
	}
	if err != nil {
		return err
	}
	defer closeHandle(handle)
	return writeAllHandle(handle, path, data)
}

// validChildName accepts exactly one Windows path component; openRelative
// validates the rest of the component rules.
func validChildName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}
