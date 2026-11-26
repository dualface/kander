package fs

import "golang.org/x/sys/windows"

// SyncTree makes the files below dir durable, listed in files as
// slash-separated paths relative to dir. Windows has no file system wide
// flush for an unprivileged process, so every file is flushed, in parallel.
// Directory entries are journaled by NTFS; dirs is not used.
func SyncTree(root, dir string, files, dirs []string) error {
	return eachParallel(treePaths(dir, files), func(path string) error {
		candidate, handle, cleanup, err := openChain(root, path, windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES, kindFile)
		if err != nil {
			return err
		}
		defer cleanup()
		if err := windows.FlushFileBuffers(handle); err != nil {
			return wrap("flush", candidate, err)
		}
		return nil
	})
}
