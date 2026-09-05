// Package fs 是 Kander 的跨平台安全文件边界.
//
// POSIX 使用 openat 与 O_NOFOLLOW 逐级打开, 私有文件 0600, 私有目录 0700,
// 阻塞独占锁用 flock, 原子替换用 rename/os.replace 等价语义.
// Windows 从卷/UNC anchor 逐分量拒绝全部 reparse point, 用已校验句柄完成
// 读写、原子替换与锁; 私有对象在创建瞬间即当前用户独占的受保护 DACL,
// 不得先按继承 ACL 发布再收紧. 任一安全后端失败显式报错, 不静默回落
// 普通路径 API.
package fs

import (
	"errors"
	"fmt"
	"os"
)

const (
	privateFileMode       os.FileMode = 0o600
	privateDirMode        os.FileMode = 0o700
	inheritedDirMode      os.FileMode = 0o777
	tempCleanupMaxDepth               = 64
	tempCleanupMaxEntries             = 4096
	tempCleanupMaxPasses              = 16
	tempNameAttempts                  = 128
)

// ErrUnsafe 表示路径越界, 含 reparse/symlink, 或最终对象类型不符合契约.
var ErrUnsafe = errors.New("unsafe path")

// ErrTempCleanup 表示私有临时目录无法通过固定句柄完整清理.
var ErrTempCleanup = errors.New("private temporary directory cleanup failed")

// Kind 是 no-follow 枚举得到的直接成员类型.
type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
	KindOther     Kind = "other"
)

// DirEntry 是 ListDirectory 返回的直接成员. 调用方随后仍须用本包原语打开叶对象.
type DirEntry struct {
	Name string
	Kind Kind
}

// Identity 是已固定对象的稳定身份. POSIX 使用 Dev/Ino; Windows 使用 Volume 与 FileIndex.
type Identity struct {
	Dev       uint64
	Ino       uint64
	Volume    uint32
	FileIndex uint64
}

func (id Identity) equal(other Identity) bool {
	return id.Dev == other.Dev && id.Ino == other.Ino && id.Volume == other.Volume && id.FileIndex == other.FileIndex
}

type pathError struct {
	Op   string
	Path string
	Err  error
}

func (e *pathError) Error() string {
	if e.Path == "" {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + " " + e.Path + ": " + e.Err.Error()
}

func (e *pathError) Unwrap() error { return e.Err }

func failClosed(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%w: %s", ErrUnsafe, msg)}
}

func wrap(op, path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUnsafe) || errors.Is(err, ErrTempCleanup) {
		if _, ok := err.(*pathError); ok {
			return err
		}
		return &pathError{Op: op, Path: path, Err: err}
	}
	return &pathError{Op: op, Path: path, Err: err}
}

func existError(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%s: %w", msg, os.ErrExist)}
}

func notExistError(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%s: %w", msg, os.ErrNotExist)}
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func isExist(err error) bool {
	return errors.Is(err, os.ErrExist)
}

// ExclusiveLock 是跨进程阻塞独占锁. Unlock 必须成对调用.
type ExclusiveLock struct {
	file *os.File
}

// AppendFile 是固定叶句柄上的追加流. Close 会先 Sync 再关闭.
type AppendFile struct {
	*os.File
}

// Close 先 Sync 再关闭, 对齐 onevoke 退出时 fsync.
func (f *AppendFile) Close() error {
	if f == nil || f.File == nil {
		return nil
	}
	syncErr := f.File.Sync()
	closeErr := f.File.Close()
	f.File = nil
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// TempDir 是已固定身份的私有临时目录. Close 从同一根句柄清理.
// POSIX 将成员中的 symlink 当非目录 unlink (只删链接本身); Windows 遇任何 reparse point 失败关闭.
type TempDir struct {
	Path string
	impl tempDirImpl
}

// Close 清理并删除临时目录.
func (d *TempDir) Close() error {
	if d == nil || d.impl == nil {
		return nil
	}
	err := d.impl.close()
	d.impl = nil
	return err
}

type tempDirImpl interface {
	close() error
}

type objectKind int

const (
	kindAny objectKind = iota
	kindFile
	kindDirectory
)
