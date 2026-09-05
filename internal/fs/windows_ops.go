//go:build windows

package fs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procNtQueryDirectoryFile = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryDirectoryFile")

// IsReparsePoint 不跟随路径, 判断它是否为 reparse point 或 symlink.
func IsReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	return data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func ReadRegularFile(root, path string) ([]byte, error) {
	candidate, handle, cleanup, err := openChain(root, path, windows.GENERIC_READ|windows.FILE_READ_ATTRIBUTES, kindFile)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return readHandle(handle, candidate)
}

func ReadRegularFileIfExists(root, path string) ([]byte, bool, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, false, err
	}
	if len(parts) == 0 {
		return nil, false, failClosed("read", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		if isNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer cleanup()
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.GENERIC_READ|windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if handle == 0 {
		return nil, false, nil
	}
	defer closeHandle(handle)
	data, err := readHandle(handle, candidate)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func RegularFileExists(root, path string) (bool, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return false, err
	}
	if len(parts) == 0 {
		return false, failClosed("exists", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer cleanup()
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if handle == 0 {
		return false, nil
	}
	closeHandle(handle)
	return true, nil
}

func OpenRegularFileIfExists(root, path string) (*os.File, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, failClosed("open", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer cleanup()
	access := uint32(windows.GENERIC_READ | windows.FILE_READ_ATTRIBUTES)
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, access, kindFile, false, false)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if handle == 0 {
		return nil, nil
	}
	return os.NewFile(uintptr(handle), candidate), nil
}

func OpenWritableRegularFile(root, path string) (*os.File, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, failClosed("open", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	access := uint32(windows.GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES)
	handle, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, access, kindFile, false, false)
	if err != nil {
		return nil, err
	}
	if handle == 0 {
		return nil, notExistError("open", candidate, "protected file does not exist")
	}
	return os.NewFile(uintptr(handle), candidate), nil
}

// MakeRegularFileReadOnly 在 Windows 上保留原有 ACL/属性, 但仍通过固定句柄验证对象.
func MakeRegularFileReadOnly(root, path string) error {
	_, _, cleanup, err := openChain(root, path, windows.FILE_READ_ATTRIBUTES, kindFile)
	if err != nil {
		return err
	}
	cleanup()
	return nil
}

// RemoveRegularFileIfExists 通过固定叶句柄删除一个普通文件.
func RemoveRegularFileIfExists(root, path string) (bool, error) {
	exists, err := RegularFileExists(root, path)
	if err != nil || !exists {
		return false, err
	}
	abs, handle, cleanup, err := openChain(root, path, windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindFile)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer cleanup()
	if err := deleteHandle(handle, abs, true); err != nil {
		return false, err
	}
	return true, nil
}

func walkWindowsDirs(path string, createPrivate bool, inherited bool) error {
	candidate, err := absolutePath(path)
	if err != nil {
		return err
	}
	anchor, err := pathAnchor(candidate)
	if err != nil {
		return err
	}
	_, _, parts, err := relativeParts(anchor, candidate)
	if err != nil {
		return err
	}
	_, rootHandle, cleanup, err := openChain(anchor, anchor, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	parent := rootHandle
	var opened []windows.Handle
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			closeHandle(opened[i])
		}
	}()
	current := anchor
	for _, part := range parts {
		current = filepath.Join(current, part)
		handle, err := tryOpenLeaf(parent, part, current, windows.FILE_READ_ATTRIBUTES, kindDirectory, true, false)
		if err != nil {
			return err
		}
		if handle == 0 {
			access := uint32(windows.FILE_READ_ATTRIBUTES)
			priv := kindAny
			if createPrivate && !inherited {
				access = windows.DELETE | windows.READ_CONTROL | windows.WRITE_DAC | windows.FILE_READ_ATTRIBUTES
				priv = kindDirectory
			}
			handle, err = openRelative(parent, part, current, access, true, kindDirectory, true, false, priv)
			if err != nil {
				if !isExistWin(err) && !isExist(err) {
					return err
				}
				handle, err = tryOpenLeaf(parent, part, current, windows.FILE_READ_ATTRIBUTES, kindDirectory, true, false)
				if err != nil {
					return err
				}
				if handle == 0 {
					return notExistError("open", current, "protected directory does not exist")
				}
			} else if createPrivate && !inherited {
				opened = append(opened, handle)
				if err := tightenPrivateHandle(handle, current, kindDirectory); err != nil {
					if delErr := deleteHandle(handle, current, true); delErr != nil {
						return delErr
					}
					opened = opened[:len(opened)-1]
					closeHandle(handle)
					return err
				}
				parent = handle
				continue
			}
		}
		opened = append(opened, handle)
		parent = handle
	}
	return nil
}

func EnsureDirectoryPath(path string) error {
	return walkWindowsDirs(path, true, false)
}

func EnsureInheritedDirectoryPath(path string) error {
	return walkWindowsDirs(path, false, true)
}

func DirectoryIdentity(root, path string) (Identity, error) {
	candidate, handle, cleanup, err := openChain(root, path, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return Identity{}, err
	}
	defer cleanup()
	return handleIdentity(handle, candidate)
}

func DirectoryExists(root, path string) (bool, error) {
	_, err := DirectoryIdentity(root, path)
	if isNotExist(err) || isMissingWin(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func ntQueryDirectory(handle windows.Handle, buf []byte, restart bool) (uint32, windows.NTStatus) {
	var iosb windows.IO_STATUS_BLOCK
	var restartFlag uintptr
	if restart {
		restartFlag = 1
	}
	r0, _, _ := procNtQueryDirectoryFile.Call(
		uintptr(handle),
		0, 0, 0,
		uintptr(unsafe.Pointer(&iosb)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(fileNamesInformation),
		0, 0,
		restartFlag,
	)
	return uint32(iosb.Information), windows.NTStatus(r0)
}

func listNames(handle windows.Handle, path string, remaining int) ([]string, error) {
	var names []string
	restart := true
	buf := make([]byte, 1<<16)
	for {
		used, status := ntQueryDirectory(handle, buf, restart)
		if status == statusNoMoreFiles {
			break
		}
		if status != 0 {
			return nil, mapNTError("readdir", path, status)
		}
		restart = false
		offset := 0
		for offset+12 <= int(used) {
			next := *(*uint32)(unsafe.Pointer(&buf[offset]))
			nameLen := *(*uint32)(unsafe.Pointer(&buf[offset+8]))
			end := offset + 12 + int(nameLen)
			if end > int(used) {
				return nil, failClosed("readdir", path, "invalid directory enumeration result")
			}
			u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset+12])), int(nameLen)/2)
			name := windows.UTF16ToString(u16)
			if name != "." && name != ".." {
				names = append(names, name)
				if remaining >= 0 && len(names) > remaining {
					return nil, failClosed("cleanup", path, "private temporary directory cleanup entry budget exceeded")
				}
			}
			if next == 0 {
				break
			}
			offset += int(next)
		}
	}
	return names, nil
}

func ListDirectory(root, path string) ([]DirEntry, error) {
	candidate, handle, cleanup, err := openChain(root, path, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	names, err := listNames(handle, candidate, -1)
	if err != nil {
		return nil, err
	}
	entries := make([]DirEntry, 0, len(names))
	for _, name := range names {
		child := filepath.Join(candidate, name)
		h, err := tryOpenLeaf(handle, name, child, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
		if err != nil {
			if isMissingWin(err) || isNotExist(err) {
				continue
			}
			return nil, err
		}
		if h == 0 {
			continue
		}
		info, err := attributeInfo(h, child)
		closeHandle(h)
		if err != nil {
			return nil, err
		}
		kind := KindFile
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
			kind = KindDirectory
		}
		entries = append(entries, DirEntry{Name: name, Kind: kind})
	}
	return entries, nil
}

func CreatePrivateDirectory(root, path string) error {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return failClosed("mkdir", candidate, "protected directory cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	handle, err := openRelative(
		parent, filepath.Base(candidate), candidate,
		windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		true, kindDirectory, true, false, kindDirectory,
	)
	if err != nil {
		if isExistWin(err) || isExist(err) {
			return existError("mkdir", candidate, "protected directory already exists")
		}
		return err
	}
	defer closeHandle(handle)
	if err := tightenPrivateHandle(handle, candidate, kindDirectory); err != nil {
		_ = deleteHandle(handle, candidate, true)
		return err
	}
	return nil
}

func EnsurePrivateDirectory(root, path string, create bool) error {
	rootAbs, _, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	_, parent, cleanup, err := openChain(rootAbs, rootAbs, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	var opened []windows.Handle
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			closeHandle(opened[i])
		}
	}()
	current := rootAbs
	for _, part := range parts {
		current = filepath.Join(current, part)
		access := uint32(windows.READ_CONTROL | windows.WRITE_DAC | windows.FILE_READ_ATTRIBUTES)
		handle, err := tryOpenLeaf(parent, part, current, access, kindDirectory, true, false)
		if err != nil {
			return err
		}
		if handle == 0 {
			if !create {
				return notExistError("open", current, "protected directory does not exist")
			}
			handle, err = openRelative(parent, part, current, access, true, kindDirectory, true, false, kindDirectory)
			if err != nil {
				if !isExistWin(err) && !isExist(err) {
					return err
				}
				handle, err = tryOpenLeaf(parent, part, current, access, kindDirectory, true, false)
				if err != nil {
					return err
				}
				if handle == 0 {
					return notExistError("open", current, "protected directory does not exist")
				}
			}
		}
		if err := tightenPrivateHandle(handle, current, kindDirectory); err != nil {
			closeHandle(handle)
			return err
		}
		opened = append(opened, handle)
		parent = handle
	}
	return nil
}

func OpenAppendFile(root, path string) (*AppendFile, error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, failClosed("append", candidate, "protected file cannot be the root")
	}
	_, parent, cleanup, err := openChain(rootAbs, filepath.Dir(candidate), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	access := uint32(windows.GENERIC_READ | windows.FILE_APPEND_DATA | windows.FILE_READ_ATTRIBUTES)
	handle, err := openOrCreateFile(parent, filepath.Base(candidate), candidate, access, false)
	if err != nil {
		return nil, err
	}
	return &AppendFile{File: os.NewFile(uintptr(handle), candidate)}, nil
}

func WriteTextAtomic(root, path, text string, replace bool) error {
	return writeTextAtomic(root, path, text, replace, true)
}

// WriteTextAtomicInherited 以父目录继承的 ACL 创建替换文件, 不检查或收紧 DACL.
func WriteTextAtomicInherited(root, path, text string, replace bool) error {
	return writeTextAtomic(root, path, text, replace, false)
}

func writeTextAtomic(root, path, text string, replace bool, private bool) error {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return failClosed("write", candidate, "protected file cannot be the root")
	}
	parentPath := filepath.Dir(candidate)
	_, parent, cleanup, err := openChain(rootAbs, parentPath, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	existing, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
	if err != nil {
		return err
	}
	if existing != 0 {
		closeHandle(existing)
		if !replace {
			return existError("write", candidate, "protected file already exists")
		}
	}
	tempName := "." + filepath.Base(candidate) + "." + itoa(os.Getpid()) + "." + itoa64(time.Now().UnixNano()) + ".tmp"
	tempPath := filepath.Join(parentPath, tempName)
	access := uint32(windows.GENERIC_WRITE | windows.DELETE | windows.FILE_READ_ATTRIBUTES)
	creation := kindAny
	if private {
		access |= windows.READ_CONTROL | windows.WRITE_DAC
		creation = kindFile
	}
	temp, err := openRelative(parent, tempName, tempPath, access, true, kindFile, true, false, creation)
	if err != nil {
		return err
	}
	moved := false
	defer func() {
		if !moved {
			_ = deleteHandle(temp, tempPath, false)
		}
		closeHandle(temp)
	}()
	id, err := handleIdentity(temp, tempPath)
	if err != nil {
		return err
	}
	if private {
		if err := tightenPrivateHandle(temp, tempPath, kindFile); err != nil {
			return err
		}
	}
	if err := writeHandle(temp, tempPath, []byte(text)); err != nil {
		return err
	}
	if err := renameHandle(temp, parent, candidate, replace); err != nil {
		return err
	}
	moved = true
	replacement, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindFile, true, true)
	if err != nil {
		return err
	}
	if replacement == 0 {
		return failClosed("write", candidate, "atomic replacement disappeared after rename")
	}
	got, err := handleIdentity(replacement, candidate)
	closeHandle(replacement)
	if err != nil {
		return err
	}
	if !id.equal(got) {
		return failClosed("write", candidate, "atomic replacement identity changed after rename")
	}
	return nil
}

func CreateDirectoryWithTextFile(root, directory, filename, text string) error {
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
		return failClosed("create", filename, "invalid protected filename")
	}
	rootAbs, candidate, parts, err := relativeParts(root, directory)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return failClosed("create", candidate, "protected directory cannot be the root")
	}
	parentPath := filepath.Dir(candidate)
	_, parent, cleanup, err := openChain(rootAbs, parentPath, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer cleanup()
	collision, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		return err
	}
	if collision != 0 {
		closeHandle(collision)
		return existError("mkdir", candidate, "protected directory already exists")
	}
	tempName := "." + filepath.Base(candidate) + "." + itoa(os.Getpid()) + "." + itoa64(time.Now().UnixNano()) + ".tmp"
	tempPath := filepath.Join(parentPath, tempName)
	dirHandle, err := openRelative(
		parent, tempName, tempPath,
		windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES,
		true, kindDirectory, true, false, kindDirectory,
	)
	if err != nil {
		return err
	}
	moved := false
	defer func() {
		if !moved {
			child, _ := tryOpenLeaf(dirHandle, filename, filepath.Join(tempPath, filename), windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
			if child != 0 {
				_ = deleteHandle(child, filepath.Join(tempPath, filename), false)
				closeHandle(child)
			}
			_ = deleteHandle(dirHandle, tempPath, false)
		}
		closeHandle(dirHandle)
	}()
	if err := tightenPrivateHandle(dirHandle, tempPath, kindDirectory); err != nil {
		return err
	}
	id, err := handleIdentity(dirHandle, tempPath)
	if err != nil {
		return err
	}
	childPath := filepath.Join(tempPath, filename)
	child, err := openRelative(
		dirHandle, filename, childPath,
		windows.GENERIC_WRITE|windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		true, kindFile, true, false, kindFile,
	)
	if err != nil {
		return err
	}
	if err := tightenPrivateHandle(child, childPath, kindFile); err != nil {
		closeHandle(child)
		return err
	}
	if err := writeHandle(child, childPath, []byte(text)); err != nil {
		closeHandle(child)
		return err
	}
	closeHandle(child)
	if err := renameHandle(dirHandle, parent, candidate, false); err != nil {
		return err
	}
	moved = true
	published, err := tryOpenLeaf(parent, filepath.Base(candidate), candidate, windows.FILE_READ_ATTRIBUTES, kindDirectory, true, true)
	if err != nil {
		return err
	}
	if published == 0 {
		return failClosed("create", candidate, "published directory disappeared after rename")
	}
	got, err := handleIdentity(published, candidate)
	closeHandle(published)
	if err != nil {
		return err
	}
	if !id.equal(got) {
		return failClosed("create", candidate, "published directory identity changed after rename")
	}
	return nil
}

func Rename(root, source, target string) error {
	rootAbs, sourceAbs, sourceParts, err := relativeParts(root, source)
	if err != nil {
		return err
	}
	_, targetAbs, targetParts, err := relativeParts(root, target)
	if err != nil {
		return err
	}
	if len(sourceParts) == 0 || len(targetParts) == 0 {
		return failClosed("rename", sourceAbs, "cannot rename the protected root")
	}
	_, srcParent, srcCleanup, err := openChain(rootAbs, filepath.Dir(sourceAbs), windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer srcCleanup()
	sourceHandle, err := tryOpenLeaf(srcParent, filepath.Base(sourceAbs), sourceAbs, windows.DELETE|windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		return err
	}
	if sourceHandle == 0 {
		return notExistError("rename", sourceAbs, "protected path does not exist")
	}
	defer closeHandle(sourceHandle)
	id, err := handleIdentity(sourceHandle, sourceAbs)
	if err != nil {
		return err
	}
	info, err := attributeInfo(sourceHandle, sourceAbs)
	if err != nil {
		return err
	}
	expected := kindFile
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		expected = kindDirectory
	}
	_, dstParent, dstCleanup, err := openChain(rootAbs, filepath.Dir(targetAbs), windows.FILE_TRAVERSE|windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return err
	}
	defer dstCleanup()
	collision, err := tryOpenLeaf(dstParent, filepath.Base(targetAbs), targetAbs, windows.FILE_READ_ATTRIBUTES, kindAny, true, false)
	if err != nil {
		return err
	}
	if collision != 0 {
		closeHandle(collision)
		return existError("rename", targetAbs, "rename target already exists")
	}
	if err := renameHandle(sourceHandle, dstParent, targetAbs, false); err != nil {
		return err
	}
	moved, err := tryOpenLeaf(dstParent, filepath.Base(targetAbs), targetAbs, windows.FILE_READ_ATTRIBUTES, expected, true, true)
	if err != nil {
		return err
	}
	if moved == 0 {
		return failClosed("rename", targetAbs, "renamed path is missing after move")
	}
	got, err := handleIdentity(moved, targetAbs)
	closeHandle(moved)
	if err != nil {
		return err
	}
	if !id.equal(got) {
		return failClosed("rename", targetAbs, "renamed path identity changed after move")
	}
	leftover, err := tryOpenLeaf(srcParent, filepath.Base(sourceAbs), sourceAbs, windows.FILE_READ_ATTRIBUTES, kindAny, true, true)
	if err != nil {
		return err
	}
	if leftover != 0 {
		closeHandle(leftover)
		return failClosed("rename", sourceAbs, "source path still exists after move")
	}
	return nil
}

func itoa(v int) string     { return fmt.Sprintf("%d", v) }
func itoa64(v int64) string { return fmt.Sprintf("%d", v) }

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

type windowsTempDir struct {
	handle windows.Handle
	path   string
}

func (d *windowsTempDir) close() error {
	if d == nil {
		return nil
	}
	var cleanup error
	if err := validateHandleKind(d.handle, d.path, kindDirectory); err != nil {
		cleanup = err
	} else if err := removeContentsWin(d.handle, d.path, 0, new(int)); err != nil {
		cleanup = err
	} else if err := deleteHandle(d.handle, d.path, true); err != nil {
		cleanup = err
	}
	closeHandle(d.handle)
	d.handle = 0
	if cleanup != nil {
		return &pathError{Op: "cleanup", Path: d.path, Err: fmt.Errorf("%w: %v", ErrTempCleanup, cleanup)}
	}
	return nil
}

func removeContentsWin(directory windows.Handle, path string, depth int, seen *int) error {
	if depth > tempCleanupMaxDepth {
		return failClosed("cleanup", path, "private temporary directory cleanup depth exceeded")
	}
	for range tempCleanupMaxPasses {
		remaining := tempCleanupMaxEntries - *seen
		if remaining < 0 {
			return failClosed("cleanup", path, "private temporary directory cleanup entry budget exceeded")
		}
		names, err := listNames(directory, path, remaining)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		*seen += len(names)
		for _, name := range names {
			child := filepath.Join(path, name)
			probe, err := tryOpenLeaf(directory, name, child, windows.FILE_READ_ATTRIBUTES, kindAny, false, false)
			if err != nil {
				if isMissingWin(err) || isNotExist(err) {
					continue
				}
				return err
			}
			if probe == 0 {
				continue
			}
			info, err := attributeInfo(probe, child)
			closeHandle(probe)
			if err != nil {
				return err
			}
			isDir := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
			access := uint32(windows.DELETE | windows.FILE_READ_ATTRIBUTES)
			expected := kindFile
			if isDir {
				access |= windows.FILE_LIST_DIRECTORY
				expected = kindDirectory
			}
			h, err := tryOpenLeaf(directory, name, child, access, expected, false, false)
			if err != nil {
				if isMissingWin(err) || isNotExist(err) {
					continue
				}
				return err
			}
			if h == 0 {
				continue
			}
			if isDir {
				if err := removeContentsWin(h, child, depth+1, seen); err != nil {
					closeHandle(h)
					return err
				}
			}
			if err := deleteHandle(h, child, true); err != nil {
				closeHandle(h)
				return err
			}
			closeHandle(h)
		}
	}
	return failClosed("cleanup", path, "private temporary directory cleanup did not become stable")
}

func CreatePrivateTempDir(parent, prefix string) (*TempDir, error) {
	parentAbs, err := absolutePath(parent)
	if err != nil {
		return nil, err
	}
	anchor, err := pathAnchor(parentAbs)
	if err != nil {
		return nil, err
	}
	_, parentHandle, cleanup, err := openChain(anchor, parentAbs, windows.FILE_READ_ATTRIBUTES, kindDirectory)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	for range tempNameAttempts {
		name := prefix + randomHex(16)
		candidate := filepath.Join(parentAbs, name)
		// 租约句柄常驻到 Close, 共享 write 是必需的: renameHandle 把临时文件改名到
		// 该目录时, 内核会以写权限打开目标父目录, 不共享 write 就 sharing violation.
		// 不共享 delete: 没有调用点会对这个目录再请求 DELETE, 保持最小放开.
		handle, err := openRelative(
			parentHandle, name, candidate,
			windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES,
			true, kindDirectory, true, false, kindDirectory,
		)
		if err != nil {
			if isExistWin(err) || isExist(err) {
				continue
			}
			return nil, err
		}
		if err := tightenPrivateHandle(handle, candidate, kindDirectory); err != nil {
			_ = deleteHandle(handle, candidate, true)
			closeHandle(handle)
			return nil, err
		}
		return &TempDir{Path: candidate, impl: &windowsTempDir{handle: handle, path: candidate}}, nil
	}
	return nil, existError("mkdir", parentAbs, "cannot allocate unique private directory")
}

func LockExclusive(file *os.File) (*ExclusiveLock, error) {
	if file == nil {
		return nil, wrap("lock", "", os.ErrInvalid)
	}
	var ov windows.Overlapped
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 0xFFFFFFFF, 0xFFFFFFFF, &ov)
	if err != nil {
		return nil, wrap("lock", file.Name(), err)
	}
	return &ExclusiveLock{file: file}, nil
}

func (l *ExclusiveLock) Unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	var ov windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &ov)
	l.file = nil
	if err != nil {
		return wrap("unlock", "", err)
	}
	return nil
}

func tightenPath(path string, dir bool) error {
	candidate, err := absolutePath(path)
	if err != nil {
		return err
	}
	anchor, err := pathAnchor(candidate)
	if err != nil {
		return err
	}
	expected := kindFile
	if dir {
		expected = kindDirectory
	}
	_, handle, cleanup, err := openChain(anchor, candidate, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES, expected)
	if err != nil {
		return err
	}
	defer cleanup()
	return tightenPrivateHandle(handle, candidate, expected)
}

func TightenPrivateFile(path string) error {
	return tightenPath(path, false)
}

func TightenPrivateDirectory(path string) error {
	return tightenPath(path, true)
}
