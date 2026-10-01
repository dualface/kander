package process

import (
	"errors"
	"os"
)

// ErrOpenFilesUnsupported means this platform cannot prove a process's open files.
var ErrOpenFilesUnsupported = errors.New("process open-file observation is unavailable on this platform")

// OpenFile is a kernel-observed file descriptor, including its inode identity.
type OpenFile struct {
	Path string
	Info os.FileInfo
}

// ProcessFiles identifies one process incarnation and its currently open files.
type ProcessFiles struct {
	Identity string
	Files    []OpenFile
}
