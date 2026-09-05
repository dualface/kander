//go:build unix

package fs

import (
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

func itoa(v int) string     { return strconv.Itoa(v) }
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
func unixNano() int64       { return time.Now().UnixNano() }

func walkEnsure(path string, mkdirMode uint32) (string, error) {
	candidate, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	anchor, err := pathAnchor(candidate)
	if err != nil {
		return "", err
	}
	_, _, parts, err := relativeParts(anchor, candidate)
	if err != nil {
		return "", err
	}
	dirfd, err := openRootDir(anchor)
	if err != nil {
		return "", mapOpenErr("open", candidate, err)
	}
	for _, part := range parts {
		next, err := openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err == unix.ENOENT {
			if err := unix.Mkdirat(dirfd, part, mkdirMode); err != nil && err != unix.EEXIST {
				unix.Close(dirfd)
				return "", mapOpenErr("mkdir", candidate, err)
			}
			next, err = openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		}
		if err != nil {
			unix.Close(dirfd)
			return "", mapOpenErr("openat", candidate, err)
		}
		unix.Close(dirfd)
		dirfd = next
	}
	unix.Close(dirfd)
	return candidate, nil
}

// EnsureDirectoryPath 从锚点逐分量打开或创建, 新建目录为 0700.
func EnsureDirectoryPath(path string) error {
	_, err := walkEnsure(path, uint32(privateDirMode))
	return err
}

// EnsureInheritedDirectoryPath 逐分量创建目录, 保留既有 mode 且让新目录应用调用方 umask.
func EnsureInheritedDirectoryPath(path string) error {
	_, err := walkEnsure(path, uint32(inheritedDirMode))
	return err
}

// DirectoryIdentity 逐级固定目录链并返回叶目录身份.
func DirectoryIdentity(root, path string) (Identity, error) {
	dir, err := openPosixDirectory(root, path)
	if err != nil {
		return Identity{}, err
	}
	defer dir.close()
	id, err := posixIdentity(dir.fd)
	if err != nil {
		return Identity{}, wrap("fstat", dir.path, err)
	}
	return id, nil
}

// DirectoryExists 安全区分目录缺失与 symlink/类型错误.
func DirectoryExists(root, path string) (bool, error) {
	_, err := DirectoryIdentity(root, path)
	if isNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListDirectory 通过固定目录描述符枚举直接成员并拒绝 symlink.
func ListDirectory(root, path string) ([]DirEntry, error) {
	dir, err := openPosixDirectory(root, path)
	if err != nil {
		return nil, err
	}
	defer dir.close()
	if _, err := unix.Seek(dir.fd, 0, 0); err != nil {
		return nil, wrap("seek", dir.path, err)
	}
	buf := make([]byte, 8192)
	var names []string
	for {
		n, err := unix.ReadDirent(dir.fd, buf)
		if err != nil {
			return nil, wrap("getdents", dir.path, err)
		}
		if n == 0 {
			break
		}
		_, _, names = unix.ParseDirent(buf[:n], -1, names)
	}
	entries := make([]DirEntry, 0, len(names))
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		child := filepath.Join(dir.path, name)
		var st unix.Stat_t
		if err := unix.Fstatat(dir.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, mapOpenErr("lstat", child, err)
		}
		if st.Mode&unix.S_IFMT == unix.S_IFLNK {
			return nil, failClosed("readdir", child, "symlink is not allowed")
		}
		kind := KindOther
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			kind = KindFile
		case unix.S_IFDIR:
			kind = KindDirectory
		}
		entries = append(entries, DirEntry{Name: name, Kind: kind})
	}
	return entries, nil
}

// CreatePrivateDirectory 相对 no-follow 父目录严格创建单个 0700 目录.
func CreatePrivateDirectory(root, path string) error {
	parent, err := openPosixParent(root, path)
	if err != nil {
		return err
	}
	defer parent.close()
	if err := unix.Mkdirat(parent.parentFD, parent.name, uint32(privateDirMode)); err != nil {
		if err == unix.EEXIST {
			return existError("mkdir", parent.path, "protected directory already exists")
		}
		return mapOpenErr("mkdir", parent.path, err)
	}
	directoryFD := -1
	defer func() {
		if directoryFD >= 0 {
			unix.Close(directoryFD)
		}
	}()
	directoryFD, err = openat(parent.parentFD, parent.name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		_ = unix.Unlinkat(parent.parentFD, parent.name, unix.AT_REMOVEDIR)
		return mapOpenErr("openat", parent.path, err)
	}
	if err := unix.Fchmod(directoryFD, uint32(privateDirMode)); err != nil {
		unix.Close(directoryFD)
		directoryFD = -1
		_ = unix.Unlinkat(parent.parentFD, parent.name, unix.AT_REMOVEDIR)
		return wrap("fchmod", parent.path, err)
	}
	return nil
}

// EnsurePrivateDirectory 逐级创建或打开目录, 并把新旧叶目录都收紧为 0700.
func EnsurePrivateDirectory(root, path string, create bool) error {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	dirfd, err := openRootDir(rootAbs)
	if err != nil {
		return mapOpenErr("open", candidate, err)
	}
	for _, part := range parts {
		next, err := openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err == unix.ENOENT {
			if !create {
				unix.Close(dirfd)
				return notExistError("openat", candidate, "protected directory does not exist")
			}
			if err := unix.Mkdirat(dirfd, part, uint32(privateDirMode)); err != nil && err != unix.EEXIST {
				unix.Close(dirfd)
				return mapOpenErr("mkdir", candidate, err)
			}
			next, err = openat(dirfd, part, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		}
		if err != nil {
			unix.Close(dirfd)
			return mapOpenErr("openat", candidate, err)
		}
		if err := unix.Fchmod(next, uint32(privateDirMode)); err != nil {
			unix.Close(next)
			unix.Close(dirfd)
			return wrap("fchmod", candidate, err)
		}
		unix.Close(dirfd)
		dirfd = next
	}
	unix.Close(dirfd)
	return nil
}

// CreateDirectoryWithTextFile 以父目录 fd 原子占用任务目录, 再安全写入其文档.
func CreateDirectoryWithTextFile(root, directory, filename, text string) error {
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
		return failClosed("create", filename, "invalid protected filename")
	}
	parent, err := openPosixParent(root, directory)
	if err != nil {
		return err
	}
	defer parent.close()
	candidate := parent.path
	if err := unix.Mkdirat(parent.parentFD, parent.name, uint32(privateDirMode)); err != nil {
		if err == unix.EEXIST {
			return existError("mkdir", candidate, "protected directory already exists")
		}
		return mapOpenErr("mkdir", candidate, err)
	}
	if err := WriteTextAtomic(root, filepath.Join(candidate, filename), text, false); err != nil {
		dirfd, openErr := openat(parent.parentFD, parent.name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if openErr == nil {
			_ = unix.Unlinkat(dirfd, filename, 0)
			unix.Close(dirfd)
		}
		_ = unix.Unlinkat(parent.parentFD, parent.name, unix.AT_REMOVEDIR)
		return err
	}
	return nil
}

// Rename 在受保护根下原子改名, 目标必须不存在. POSIX 要求入口位于状态目录之下.
func Rename(root, source, target string) error {
	rootAbs, sourceAbs, sourceParts, err := relativeParts(root, source)
	if err != nil {
		return err
	}
	_, targetAbs, targetParts, err := relativeParts(root, target)
	if err != nil {
		return err
	}
	if len(sourceParts) < 2 || len(targetParts) < 2 {
		return failClosed("rename", sourceAbs, "protected rename must stay below state directories")
	}
	srcParent, err := openPosixParent(rootAbs, sourceAbs)
	if err != nil {
		return err
	}
	defer srcParent.close()
	dstParent, err := openPosixParent(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	defer dstParent.close()
	var st unix.Stat_t
	if err := unix.Fstatat(dstParent.parentFD, dstParent.name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return existError("rename", targetAbs, "rename target already exists")
	} else if err != unix.ENOENT {
		return mapOpenErr("lstat", targetAbs, err)
	}
	if err := unix.Renameat(srcParent.parentFD, srcParent.name, dstParent.parentFD, dstParent.name); err != nil {
		return mapOpenErr("rename", targetAbs, err)
	}
	return nil
}
