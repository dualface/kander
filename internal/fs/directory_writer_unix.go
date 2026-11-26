//go:build unix

package fs

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type directoryWriterImpl = *posixDirectoryWriter

type posixDirectoryWriter struct{ dir *posixDir }

func (d *posixDirectoryWriter) close() error {
	d.dir.close()
	return nil
}

// OpenDirectoryWriter pins the directory path below root without following
// links.
func OpenDirectoryWriter(root, path string) (*DirectoryWriter, error) {
	dir, err := openPosixDirectory(root, path)
	if err != nil {
		return nil, err
	}
	return &DirectoryWriter{path: dir.path, impl: &posixDirectoryWriter{dir: dir}}, nil
}

// CreateInherited writes data to the new file name. The file is created with
// mode 0666 less the umask, like the other inherited writes of this package.
func (w *DirectoryWriter) CreateInherited(name string, data []byte) error {
	path := filepath.Join(w.path, name)
	if !validChildName(name) {
		return failClosed("create", path, "invalid file name")
	}
	fd, err := openat(w.impl.dir.fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o666)
	if err == unix.EEXIST {
		return existError("create", path, "protected file already exists")
	}
	if err != nil {
		return mapOpenErr("create", path, err)
	}
	if err := writeAllFD(fd, data); err != nil {
		unix.Close(fd)
		return wrap("write", path, err)
	}
	if err := unix.Close(fd); err != nil {
		return wrap("close", path, err)
	}
	return nil
}

// validChildName accepts exactly one POSIX path component. A backslash is
// an ordinary character here, as it is for the other writes of this package.
func validChildName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
