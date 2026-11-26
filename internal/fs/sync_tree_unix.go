//go:build unix && !linux

package fs

import "golang.org/x/sys/unix"

// SyncTree makes the tree below dir durable: its files, listed in files, and
// its directories, listed in dirs, both slash-separated and relative to dir.
// Without a file system wide flush, every file and directory is flushed, in
// parallel.
func SyncTree(root, dir string, files, dirs []string) error {
	err := eachParallel(treePaths(dir, files), func(path string) error {
		parent, err := openPosixParent(root, path)
		if err != nil {
			return err
		}
		defer parent.close()
		fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
		if err != nil {
			return mapOpenErr("open", path, err)
		}
		defer unix.Close(fd)
		if err := requireRegular(fd, path); err != nil {
			return err
		}
		return wrapNil("fsync", path, unix.Fsync(fd))
	})
	if err != nil {
		return err
	}
	return eachParallel(append(treePaths(dir, dirs), dir), func(path string) error {
		d, err := openPosixDirectory(root, path)
		if err != nil {
			return err
		}
		defer d.close()
		return wrapNil("fsync", path, unix.Fsync(d.fd))
	})
}

func wrapNil(op, path string, err error) error {
	if err == nil {
		return nil
	}
	return wrap(op, path, err)
}
