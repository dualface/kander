//go:build unix

package fs

import (
	"os"

	"golang.org/x/sys/unix"
)

func openFlags(extra int) int {
	return extra | unix.O_CLOEXEC | unix.O_NOFOLLOW
}

func openRootDir(path string) (int, error) {
	fd, err := unix.Open(path, openFlags(unix.O_RDONLY|unix.O_DIRECTORY), 0)
	if err != nil {
		if isSymlinkErr(err) {
			return -1, failClosed("open", path, "symlink is not allowed")
		}
		return -1, wrap("open", path, err)
	}
	return fd, nil
}

func openat(dirfd int, name string, flags int, mode uint32) (int, error) {
	fd, err := unix.Openat(dirfd, name, openFlags(flags), mode)
	if err != nil {
		if isSymlinkErr(err) {
			return -1, err
		}
		return -1, err
	}
	return fd, nil
}

func isSymlinkErr(err error) bool {
	return err == unix.ELOOP || err == unix.ENOTDIR
}

func mapOpenErr(op, path string, err error) error {
	if err == nil {
		return nil
	}
	if isSymlinkErr(err) {
		return failClosed(op, path, "symlink is not allowed")
	}
	if err == unix.ENOENT {
		return notExistError(op, path, "protected path does not exist")
	}
	if err == unix.EEXIST {
		return existError(op, path, "protected path already exists")
	}
	return wrap(op, path, err)
}

type posixParent struct {
	path     string
	parentFD int
	name     string
}

func openPosixParent(root, path string) (*posixParent, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, failClosed("open", candidate, "protected path cannot be the root")
	}
	dirfd, err := openRootDir(rootAbs)
	if err != nil {
		return nil, mapOpenErr("open", rootAbs, err)
	}
	for _, part := range parts[:len(parts)-1] {
		next, err := openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		unix.Close(dirfd)
		if err != nil {
			return nil, mapOpenErr("openat", candidate, err)
		}
		dirfd = next
	}
	return &posixParent{path: candidate, parentFD: dirfd, name: parts[len(parts)-1]}, nil
}

func (p *posixParent) close() {
	if p != nil && p.parentFD >= 0 {
		unix.Close(p.parentFD)
		p.parentFD = -1
	}
}

type posixDir struct {
	path string
	fd   int
}

func openPosixDirectory(root, path string) (*posixDir, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	dirfd, err := openRootDir(rootAbs)
	if err != nil {
		return nil, mapOpenErr("open", rootAbs, err)
	}
	for _, part := range parts {
		next, err := openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		unix.Close(dirfd)
		if err != nil {
			return nil, mapOpenErr("openat", candidate, err)
		}
		dirfd = next
	}
	return &posixDir{path: candidate, fd: dirfd}, nil
}

func (d *posixDir) close() {
	if d != nil && d.fd >= 0 {
		unix.Close(d.fd)
		d.fd = -1
	}
}

func requireRegular(fd int, path string) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return wrap("fstat", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return failClosed("fstat", path, "task document is not a regular file")
	}
	return nil
}

func posixIdentity(fd int) (Identity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return Identity{}, err
	}
	return Identity{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, nil
}

func readFD(fd int) ([]byte, error) {
	var chunks []byte
	buf := make([]byte, 1<<20)
	for {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			chunks = append(chunks, buf[:n]...)
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return chunks, nil
		}
	}
}

func writeFD(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return unix.EIO
		}
	}
	return unix.Fsync(fd)
}

// ReadRegularFile reads a regular file relative to a protected root without following links.
func ReadRegularFile(root, path string) ([]byte, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
	if err != nil {
		return nil, mapOpenErr("read", parent.path, err)
	}
	defer unix.Close(fd)
	if err := requireRegular(fd, parent.path); err != nil {
		return nil, err
	}
	data, err := readFD(fd)
	if err != nil {
		return nil, wrap("read", parent.path, err)
	}
	return data, nil
}

// ReadRegularFileIfExists reads an optional regular file; only genuine absence yields (nil, false).
func ReadRegularFileIfExists(root, path string) ([]byte, bool, error) {
	data, err := ReadRegularFile(root, path)
	if isNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// RegularFileExists detects a regular file with a non-blocking no-follow open, so a FIFO cannot hang it.
func RegularFileExists(root, path string) (bool, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		if err == unix.ENOENT {
			return false, nil
		}
		return false, mapOpenErr("exists", parent.path, err)
	}
	defer unix.Close(fd)
	if err := requireRegular(fd, parent.path); err != nil {
		return false, err
	}
	return true, nil
}

// OpenRegularFileIfExists safely opens an optional regular file. Absence returns (nil, nil).
func OpenRegularFileIfExists(root, path string) (*os.File, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
	if err != nil {
		if err == unix.ENOENT {
			return nil, nil
		}
		return nil, mapOpenErr("open", parent.path, err)
	}
	if err := requireRegular(fd, parent.path); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), parent.path), nil
}

// OpenWritableRegularFile opens an existing regular file for overwriting relative to a protected root, without following links.
func OpenWritableRegularFile(root, path string) (*os.File, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_WRONLY, 0)
	if err != nil {
		return nil, mapOpenErr("open", parent.path, err)
	}
	if err := requireRegular(fd, parent.path); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), parent.path), nil
}

// MakeRegularFileReadOnly sets a regular file to 0400 through an already validated handle.
func MakeRegularFileReadOnly(root, path string) error {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
	if err != nil {
		return mapOpenErr("open", parent.path, err)
	}
	defer unix.Close(fd)
	if err := requireRegular(fd, parent.path); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, 0o400); err != nil {
		return wrap("fchmod", parent.path, err)
	}
	return nil
}

// RemoveRegularFileIfExists removes one regular file without following links.
func RemoveRegularFileIfExists(root, path string) (bool, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return false, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
	if err == unix.ENOENT {
		return false, nil
	}
	if err != nil {
		return false, mapOpenErr("remove", parent.path, err)
	}
	if err := requireRegular(fd, parent.path); err != nil {
		unix.Close(fd)
		return false, err
	}
	if err := unix.Close(fd); err != nil {
		return false, wrap("close", parent.path, err)
	}
	if err := unix.Unlinkat(parent.parentFD, parent.name, 0); err != nil {
		if err == unix.ENOENT {
			return false, nil
		}
		return false, mapOpenErr("remove", parent.path, err)
	}
	return true, nil
}

// OpenAppendFile pins a regular file descriptor for reading and appending, leaving the existing mode untouched.
func OpenAppendFile(root, path string) (*AppendFile, error) {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.close()
	fd, err := openat(parent.parentFD, parent.name, unix.O_RDWR|unix.O_CREAT|unix.O_APPEND, 0o666)
	if err != nil {
		return nil, mapOpenErr("append", parent.path, err)
	}
	if err := requireRegular(fd, parent.path); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return &AppendFile{File: os.NewFile(uintptr(fd), parent.path)}, nil
}

// WriteTextAtomic atomically writes UTF-8 text relative to a pinned parent directory.
// With replace=false it claims the new entry exclusively (Renameat2 on Linux, RenameatxNp on Darwin, Linkat elsewhere) and fails when it already exists.
func WriteTextAtomic(root, path, text string, replace bool) error {
	return writeTextAtomic(root, path, text, replace, uint32(privateFileMode))
}

// WriteTextAtomicInherited keeps the existing file mode and creates new files as 0666 masked by the process umask.
func WriteTextAtomicInherited(root, path, text string, replace bool) error {
	return writeTextAtomic(root, path, text, replace, 0o666)
}

func writeTextAtomic(root, path, text string, replace bool, createMode uint32) error {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return err
	}
	defer parent.close()
	var mode *uint32
	existing, err := openat(parent.parentFD, parent.name, unix.O_RDONLY, 0)
	if err != nil && err != unix.ENOENT {
		return mapOpenErr("write", parent.path, err)
	}
	if existing >= 0 {
		if err := requireRegular(existing, parent.path); err != nil {
			unix.Close(existing)
			return err
		}
		var st unix.Stat_t
		if err := unix.Fstat(existing, &st); err != nil {
			unix.Close(existing)
			return wrap("fstat", parent.path, err)
		}
		m := uint32(st.Mode & 0o777)
		mode = &m
		unix.Close(existing)
		if !replace {
			return existError("write", parent.path, "protected file already exists")
		}
	}
	tempName := tempSiblingName(parent.name)
	tempFD, err := openat(parent.parentFD, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, createMode)
	if err != nil {
		return mapOpenErr("write", parent.path, err)
	}
	moved := false
	defer func() {
		if tempFD >= 0 {
			unix.Close(tempFD)
		}
		if !moved {
			_ = unix.Unlinkat(parent.parentFD, tempName, 0)
		}
	}()
	if mode != nil {
		if err := unix.Fchmod(tempFD, *mode); err != nil {
			return wrap("fchmod", parent.path, err)
		}
	}
	if err := writeFD(tempFD, []byte(text)); err != nil {
		return wrap("write", parent.path, err)
	}
	if err := unix.Close(tempFD); err != nil {
		tempFD = -1
		return wrap("close", parent.path, err)
	}
	tempFD = -1
	if replace {
		if err := unix.Renameat(parent.parentFD, tempName, parent.parentFD, parent.name); err != nil {
			return mapOpenErr("rename", parent.path, err)
		}
		moved = true
		return nil
	}
	if err := exclusivePublish(parent.parentFD, tempName, parent.name, parent.path); err != nil {
		return err
	}
	moved = true
	return nil
}

func tempSiblingName(name string) string {
	return "." + name + "." + itoa(os.Getpid()) + "." + itoa64(unixNano()) + ".tmp"
}
