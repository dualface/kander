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

// ReadRegularFile 相对受保护根 no-follow 读取普通文件.
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

// ReadRegularFileIfExists 读取可选普通文件; 只把真实缺失视为 (nil, false).
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

// RegularFileExists 用非阻塞 no-follow 打开判断普通文件, 避免 FIFO 挂起.
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

// OpenRegularFileIfExists 安全打开可选普通文件. 缺失时返回 (nil, nil).
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

// OpenWritableRegularFile 相对受保护根 no-follow 打开已有普通文件供覆写.
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

// MakeRegularFileReadOnly 通过已验证句柄把普通文件设为 0400.
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

// RemoveRegularFileIfExists 不跟随链接删除一个普通文件.
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

// OpenAppendFile 固定普通文件描述符读取和追加, 且不修改既有 mode.
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

// WriteTextAtomic 相对固定父目录原子写入 UTF-8 文本.
// replace=false 时独占占用新入口 (Linux Renameat2, Darwin RenameatxNp, 其余 Linkat), 已存在则失败.
func WriteTextAtomic(root, path, text string, replace bool) error {
	return writeTextAtomic(root, path, text, replace, uint32(privateFileMode))
}

// WriteTextAtomicInherited 保留既有文件 mode, 新文件按 0666 与进程 umask 创建.
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
