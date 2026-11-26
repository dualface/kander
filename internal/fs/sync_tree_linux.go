package fs

import "golang.org/x/sys/unix"

// SyncTree makes the tree below dir durable: its files, listed in files, and
// its directories, listed in dirs, both slash-separated and relative to dir.
// Linux flushes the whole file system that holds dir with one syncfs, which
// is far cheaper than one fsync per file. Before Linux 5.8, syncfs does not
// report writeback errors, so a failed writeback goes unnoticed there.
func SyncTree(root, dir string, files, dirs []string) error {
	d, err := openPosixDirectory(root, dir)
	if err != nil {
		return err
	}
	defer d.close()
	if err := unix.Syncfs(d.fd); err != nil {
		return wrap("syncfs", d.path, err)
	}
	return nil
}
