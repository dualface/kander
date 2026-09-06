//go:build unix

package fs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type posixTempDir struct {
	parentFD int
	rootFD   int
	path     string
	name     string
}

func (d *posixTempDir) close() error {
	if d == nil {
		return nil
	}
	var cleanup error
	if d.rootFD >= 0 {
		if err := removeContents(d.rootFD, d.path, 0, new(int)); err != nil {
			cleanup = err
		}
	}
	if d.parentFD >= 0 && d.name != "" {
		if err := unix.Unlinkat(d.parentFD, d.name, unix.AT_REMOVEDIR); err != nil && cleanup == nil {
			cleanup = err
		}
	}
	if d.rootFD >= 0 {
		unix.Close(d.rootFD)
		d.rootFD = -1
	}
	if d.parentFD >= 0 {
		unix.Close(d.parentFD)
		d.parentFD = -1
	}
	if cleanup != nil {
		return &pathError{
			Op:   "cleanup",
			Path: d.path,
			Err:  fmt.Errorf("%w: %v", ErrTempCleanup, cleanup),
		}
	}
	return nil
}

func directoryNames(directoryFD int, directory string, remaining int) ([]string, error) {
	if _, err := unix.Seek(directoryFD, 0, 0); err != nil {
		return nil, wrap("seek", directory, err)
	}
	buf := make([]byte, 8192)
	var names []string
	for {
		n, err := unix.ReadDirent(directoryFD, buf)
		if err != nil {
			return nil, wrap("getdents", directory, err)
		}
		if n == 0 {
			break
		}
		_, _, names = unix.ParseDirent(buf[:n], -1, names)
	}
	filtered := names[:0]
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		filtered = append(filtered, name)
		if len(filtered) > remaining {
			return nil, failClosed("cleanup", directory, "private temporary directory cleanup entry budget exceeded")
		}
	}
	return filtered, nil
}

func removeContents(directoryFD int, directory string, depth int, seen *int) error {
	if depth > tempCleanupMaxDepth {
		return failClosed("cleanup", directory, "private temporary directory cleanup depth exceeded")
	}
	if err := unix.Fchmod(directoryFD, uint32(privateDirMode)); err != nil {
		return wrap("fchmod", directory, err)
	}
	for range tempCleanupMaxPasses {
		remaining := tempCleanupMaxEntries - *seen
		if remaining < 0 {
			return failClosed("cleanup", directory, "private temporary directory cleanup entry budget exceeded")
		}
		names, err := directoryNames(directoryFD, directory, remaining)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		*seen += len(names)
		for _, name := range names {
			child := filepath.Join(directory, name)
			var st unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if err == unix.ENOENT {
					continue
				}
				return mapOpenErr("lstat", child, err)
			}
			if st.Mode&unix.S_IFMT != unix.S_IFDIR {
				if err := unix.Unlinkat(directoryFD, name, 0); err != nil && err != unix.ENOENT {
					return mapOpenErr("unlink", child, err)
				}
				continue
			}
			if err := unix.Fchmodat(directoryFD, name, uint32(privateDirMode), unix.AT_SYMLINK_NOFOLLOW); err != nil && err != unix.ENOENT && err != unix.ENOTSUP {
				return mapOpenErr("chmod", child, err)
			}
			childFD, err := openat(directoryFD, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
			if err != nil {
				if err == unix.ENOENT {
					continue
				}
				return mapOpenErr("openat", child, err)
			}
			opened, err := posixIdentity(childFD)
			if err != nil {
				unix.Close(childFD)
				return wrap("fstat", child, err)
			}
			want := Identity{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}
			if !opened.equal(want) {
				unix.Close(childFD)
				return failClosed("openat", child, "protected path identity changed while opening")
			}
			err = removeContents(childFD, child, depth+1, seen)
			unix.Close(childFD)
			if err != nil {
				return err
			}
			var current unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return mapOpenErr("lstat", child, err)
			}
			got := Identity{Dev: uint64(current.Dev), Ino: uint64(current.Ino)}
			if !got.equal(want) {
				return failClosed("unlink", child, "protected path identity changed before removal")
			}
			if err := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR); err != nil && err != unix.ENOENT {
				return mapOpenErr("rmdir", child, err)
			}
		}
	}
	return failClosed("cleanup", directory, "private temporary directory cleanup did not become stable")
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", unixNano())
	}
	return hex.EncodeToString(buf)
}

// CreatePrivateTempDir creates a 0700 private temporary directory through pinned parent/root fds.
func CreatePrivateTempDir(parent, prefix string) (*TempDir, error) {
	parentAbs, err := absolutePath(parent)
	if err != nil {
		return nil, err
	}
	anchor, err := pathAnchor(parentAbs)
	if err != nil {
		return nil, err
	}
	dir, err := openPosixDirectory(anchor, parentAbs)
	if err != nil {
		return nil, err
	}
	var candidate string
	rootFD := -1
	var name string
	for range tempNameAttempts {
		name = prefix + randomHex(16)
		possible := filepath.Join(parentAbs, name)
		if err := unix.Mkdirat(dir.fd, name, uint32(privateDirMode)); err != nil {
			if err == unix.EEXIST {
				continue
			}
			dir.close()
			return nil, mapOpenErr("mkdir", possible, err)
		}
		candidate = possible
		rootFD, err = openat(dir.fd, name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			_ = unix.Unlinkat(dir.fd, name, unix.AT_REMOVEDIR)
			dir.close()
			return nil, mapOpenErr("openat", possible, err)
		}
		if err := unix.Fchmod(rootFD, uint32(privateDirMode)); err != nil {
			unix.Close(rootFD)
			_ = unix.Unlinkat(dir.fd, name, unix.AT_REMOVEDIR)
			dir.close()
			return nil, wrap("fchmod", possible, err)
		}
		break
	}
	if candidate == "" || rootFD < 0 {
		dir.close()
		return nil, existError("mkdir", parentAbs, "cannot allocate unique private directory")
	}
	impl := &posixTempDir{parentFD: dir.fd, rootFD: rootFD, path: candidate, name: name}
	dir.fd = -1
	return &TempDir{Path: candidate, impl: impl}, nil
}
