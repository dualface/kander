package process

import "os"

// OpenFile is a kernel-observed file descriptor, including its inode identity.
type OpenFile struct {
	Path string
	Info os.FileInfo
}
