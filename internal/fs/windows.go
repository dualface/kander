//go:build windows

package fs

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	statusNoMoreFiles             windows.NTStatus = 0x80000006
	statusReparsePointEncountered windows.NTStatus = 0xC000050B
	statusIOReparseTagNotHandled  windows.NTStatus = 0xC0000279
	statusStoppedOnSymlink        windows.NTStatus = 0x8000002D
	errorCantResolveFilename                       = syscall.Errno(1921)
	fileNamesInformation                           = 12
	// The x/sys release in use has no FileRenameInformationEx class for NtSetInformationFile.
	fileRenameInformationExClass = 65
)

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

type fileDispositionInfo struct {
	DeleteFile uint8
}

type fileRenameInformation struct {
	ReplaceIfExists uint8
	_               [7]byte
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

// fileRenameInformationEx is FILE_RENAME_INFORMATION_EX. Like fileRenameInformation it assumes the 64-bit layout, where
// RootDirectory sits at offset 8.
type fileRenameInformationEx struct {
	Flags          uint32
	_              [4]byte
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// renameStats counts what renameHandle does so that tests can tell which path a replacement took. exCalls counts the
// system calls that carry the Ex information class, classicFallbacks the replacements that fell back to the classic
// call, and busyRetries the waits taken after a busy failure.
var renameStats struct {
	exCalls          atomic.Int64
	classicFallbacks atomic.Int64
	busyRetries      atomic.Int64
}

func closeHandle(h windows.Handle) {
	if h != 0 && h != windows.InvalidHandle {
		_ = windows.CloseHandle(h)
	}
}

func isMissingWin(err error) bool {
	if err == nil {
		return false
	}
	if isNotExist(err) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == windows.ERROR_FILE_NOT_FOUND || errno == windows.ERROR_PATH_NOT_FOUND
	}
	var st windows.NTStatus
	if errors.As(err, &st) {
		errno = st.Errno()
		return errno == windows.ERROR_FILE_NOT_FOUND || errno == windows.ERROR_PATH_NOT_FOUND
	}
	return false
}

func isExistWin(err error) bool {
	if isExist(err) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == windows.ERROR_ALREADY_EXISTS || errno == windows.ERROR_FILE_EXISTS
	}
	var st windows.NTStatus
	if errors.As(err, &st) {
		errno = st.Errno()
		return errno == windows.ERROR_ALREADY_EXISTS || errno == windows.ERROR_FILE_EXISTS
	}
	return false
}

// isReparseNTError reports whether err is an NT status that means the path crossed a reparse point.
func isReparseNTError(err error) bool {
	var st windows.NTStatus
	if !errors.As(err, &st) {
		return false
	}
	switch st {
	case statusReparsePointEncountered, statusIOReparseTagNotHandled, statusStoppedOnSymlink:
		return true
	}
	return st.Errno() == errorCantResolveFilename
}

func mapNTError(op, path string, err error) error {
	if err == nil {
		return nil
	}
	var st windows.NTStatus
	if errors.As(err, &st) {
		if isReparseNTError(err) {
			return failClosed(op, path, "reparse point is not allowed")
		}
		errno := st.Errno()
		if errno == windows.ERROR_FILE_NOT_FOUND || errno == windows.ERROR_PATH_NOT_FOUND {
			return notExistError(op, path, "protected path does not exist")
		}
		if errno == windows.ERROR_ALREADY_EXISTS || errno == windows.ERROR_FILE_EXISTS {
			return existError(op, path, "protected path already exists")
		}
		return wrap(op, path, errno)
	}
	if isMissingWin(err) {
		return notExistError(op, path, "protected path does not exist")
	}
	if isExistWin(err) {
		return existError(op, path, "protected path already exists")
	}
	return wrap(op, path, err)
}

func attributeInfo(handle windows.Handle, path string) (fileAttributeTagInfo, error) {
	var info fileAttributeTagInfo
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return info, wrap("stat", path, err)
	}
	return info, nil
}

func validateHandleKind(handle windows.Handle, path string, expected objectKind) error {
	info, err := attributeInfo(handle, path)
	if err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		if expected == kindAnyWithReparse {
			return nil
		}
		return failClosed("stat", path, "reparse point is not allowed")
	}
	isDir := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if expected == kindDirectory && !isDir {
		return failClosed("stat", path, "path component is not a directory")
	}
	if expected == kindFile && isDir {
		return failClosed("stat", path, "task document is not a regular file")
	}
	return nil
}

func handleIdentity(handle windows.Handle, path string) (Identity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return Identity{}, wrap("identify", path, err)
	}
	return Identity{
		Volume:    info.VolumeSerialNumber,
		FileIndex: (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow),
	}, nil
}

func createVolumeHandle(path string, access uint32, expected objectKind) (windows.Handle, error) {
	if expected == kindDirectory {
		access |= windows.FILE_TRAVERSE
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT | windows.FILE_FLAG_BACKUP_SEMANTICS)
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		access,
		share,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return 0, mapNTError("open", path, err)
	}
	if err := validateHandleKind(handle, path, expected); err != nil {
		closeHandle(handle)
		return 0, err
	}
	return handle, nil
}

func privateSecurityDescriptor(kind objectKind) (*windows.SECURITY_DESCRIPTOR, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, wrap("token", "", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, wrap("token", "", err)
	}
	sid := user.User.Sid.String()
	inherit := ""
	if kind == kindDirectory {
		inherit = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%s;FA;;;%s)", inherit, sid)
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, wrap("acl", "", err)
	}
	return sd, nil
}

func tightenPrivateHandle(handle windows.Handle, path string, kind objectKind) error {
	sd, err := privateSecurityDescriptor(kind)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return wrap("acl", path, err)
	}
	err = windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
	if err != nil {
		return wrap("acl", path, err)
	}
	return nil
}

func openRelative(
	parent windows.Handle,
	name, path string,
	access uint32,
	createNew bool,
	expected objectKind,
	shareWrite, shareDelete bool,
	privateCreate objectKind,
) (windows.Handle, error) {
	if err := validateComponent(name, path); err != nil {
		return 0, err
	}
	if privateCreate != kindAny && !createNew {
		return 0, wrap("open", path, errors.New("private_creation requires CREATE_NEW"))
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, wrap("open", path, err)
	}
	options := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_FOR_BACKUP_INTENT | windows.FILE_OPEN_REPARSE_POINT)
	if expected == kindDirectory {
		access |= windows.FILE_TRAVERSE
		options |= windows.FILE_DIRECTORY_FILE
	} else if expected == kindFile {
		options |= windows.FILE_NON_DIRECTORY_FILE
	}
	disposition := uint32(windows.FILE_OPEN)
	if createNew {
		disposition = windows.FILE_CREATE
	}
	share := uint32(windows.FILE_SHARE_READ)
	if shareWrite {
		share |= windows.FILE_SHARE_WRITE
	}
	if shareDelete {
		share |= windows.FILE_SHARE_DELETE
	}
	oa := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	if privateCreate != kindAny {
		sd, err := privateSecurityDescriptor(privateCreate)
		if err != nil {
			return 0, err
		}
		oa.SecurityDescriptor = sd
	}
	attrs := uint32(0)
	if expected != kindDirectory {
		attrs = windows.FILE_ATTRIBUTE_NORMAL
	}
	var handle windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&handle,
		access|windows.SYNCHRONIZE,
		&oa,
		&iosb,
		nil,
		attrs,
		share,
		disposition,
		options,
		0,
		0,
	)
	if err != nil {
		return 0, mapNTError("open", path, err)
	}
	if err := validateHandleKind(handle, path, expected); err != nil {
		closeHandle(handle)
		return 0, err
	}
	return handle, nil
}

// leafOpenHook runs for every leaf open that is not attribute-only, just before the open that returns the handle:
// between the probe and the reopen when the leaf open still probes, and before the single open of a read-only leaf
// open. Tests use it to change the leaf inside that window; production leaves it nil.
var leafOpenHook func(path string, access uint32)

// readOnlyLeafAccess reports whether access asks for nothing but reading file data. It is a whitelist: only
// GENERIC_READ, FILE_READ_ATTRIBUTES and SYNCHRONIZE may be set, and GENERIC_READ must be. A write, delete, ACL,
// directory listing or traversal right, or any bit this function does not know, makes the access not read-only.
func readOnlyLeafAccess(access uint32) bool {
	const allowed = windows.GENERIC_READ | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
	return access&windows.GENERIC_READ != 0 && access&^allowed == 0
}

// openReadOnlyLeaf opens a regular file for reading with one by-name open. A read-only caller observes the file
// through the returned handle only, so comparing it with a separately probed object adds no protection: an atomic
// replacement between the probe and the reopen can only turn a valid read into an identity error. The open keeps
// the guarantees of the probe: it is relative to the pinned parent, it carries FILE_OPEN_REPARSE_POINT so a
// reparse point is opened as itself and rejected, and it does not ask for a non-directory so that a junction is
// reported as a reparse point rather than as a directory. The returned handle is then validated as a regular file.
func openReadOnlyLeaf(
	parent windows.Handle,
	name, path string,
	access uint32,
	shareWrite, shareDelete bool,
) (windows.Handle, error) {
	if leafOpenHook != nil {
		leafOpenHook(path, access)
	}
	handle, err := openRelative(parent, name, path, access, false, kindAny, shareWrite, shareDelete, kindAny)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := validateHandleKind(handle, path, kindFile); err != nil {
		closeHandle(handle)
		return 0, err
	}
	return handle, nil
}

func tryOpenLeaf(
	parent windows.Handle,
	name, path string,
	access uint32,
	expected objectKind,
	shareWrite, shareDelete bool,
) (windows.Handle, error) {
	if expected == kindFile && readOnlyLeafAccess(access) {
		return openReadOnlyLeaf(parent, name, path, access, shareWrite, shareDelete)
	}
	probeKind := kindAny
	if expected == kindAnyWithReparse {
		probeKind = kindAnyWithReparse
	}
	probe, err := openRelative(parent, name, path, windows.FILE_READ_ATTRIBUTES, false, probeKind, shareWrite, shareDelete || access&windows.DELETE != 0, kindAny)
	if err != nil {
		if isMissingWin(err) || isNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := validateHandleKind(probe, path, expected); err != nil {
		closeHandle(probe)
		return 0, err
	}
	if access == windows.FILE_READ_ATTRIBUTES {
		return probe, nil
	}
	id, err := handleIdentity(probe, path)
	if err != nil {
		closeHandle(probe)
		return 0, err
	}
	if leafOpenHook != nil {
		leafOpenHook(path, access)
	}
	result, err := openRelative(parent, name, path, access, false, expected, shareWrite, shareDelete, kindAny)
	closeHandle(probe)
	if err != nil {
		return 0, err
	}
	got, err := handleIdentity(result, path)
	if err != nil {
		closeHandle(result)
		return 0, err
	}
	if !id.equal(got) {
		closeHandle(result)
		return 0, failClosed("open", path, "protected path identity changed while opening")
	}
	return result, nil
}

func openChain(root, path string, finalAccess uint32, finalExpected objectKind) (string, windows.Handle, func(), error) {
	rootAbs, candidate, parts, err := relativeParts(root, path)
	if err != nil {
		return "", 0, func() {}, err
	}
	anchor, err := pathAnchor(rootAbs)
	if err != nil {
		return "", 0, func() {}, err
	}
	_, _, rootParts, err := relativeParts(anchor, rootAbs)
	if err != nil {
		return "", 0, func() {}, err
	}
	chain := append(append([]string{}, rootParts...), parts...)
	var handles []windows.Handle
	cleanup := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			closeHandle(handles[i])
		}
	}
	current := anchor
	firstExpected := kindDirectory
	firstAccess := uint32(windows.FILE_READ_ATTRIBUTES)
	if len(chain) == 0 {
		firstExpected = finalExpected
		firstAccess = finalAccess
	}
	h, err := createVolumeHandle(current, firstAccess, firstExpected)
	if err != nil {
		return "", 0, func() {}, err
	}
	handles = append(handles, h)
	if len(chain) == 0 {
		return candidate, h, cleanup, nil
	}
	for index, part := range chain {
		current = filepath.Join(current, part)
		final := index == len(chain)-1
		expected := kindDirectory
		if final {
			expected = finalExpected
			// The directory leaf is opened with shared delete so that it never conflicts with another handle holding DELETE on
			// the same directory, such as the removal handle of a private temporary directory.
			// File leaves keep the narrow sharing, governed by the caller's own DELETE access.
			leaf, err := tryOpenLeaf(handles[len(handles)-1], part, current, finalAccess, expected, true, expected == kindDirectory)
			if err != nil {
				cleanup()
				return "", 0, func() {}, err
			}
			if leaf == 0 {
				cleanup()
				return "", 0, func() {}, notExistError("open", current, "protected path does not exist")
			}
			handles = append(handles, leaf)
			continue
		}
		// Same as above: any component of the chain may be held open by another handle with DELETE access.
		next, err := openRelative(handles[len(handles)-1], part, current, windows.FILE_READ_ATTRIBUTES, false, kindDirectory, true, true, kindAny)
		if err != nil {
			cleanup()
			return "", 0, func() {}, err
		}
		handles = append(handles, next)
	}
	return candidate, handles[len(handles)-1], cleanup, nil
}

func deleteHandle(handle windows.Handle, path string, required bool) error {
	info := fileDispositionInfo{DeleteFile: 1}
	err := windows.SetFileInformationByHandle(
		handle,
		windows.FileDispositionInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil && required {
		return wrap("delete", path, err)
	}
	return nil
}

func readHandle(handle windows.Handle, path string) ([]byte, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, wrap("stat", path, err)
	}
	size := int64(uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow))
	// A zero-length regular file has no bytes to read. Windows rejects even an
	// EOF read through another handle when an exclusive byte-range lock is held.
	if size == 0 {
		return nil, nil
	}
	scratch := acquireReadScratch()
	defer releaseReadScratch(scratch)
	buf := *scratch
	chunks := destForSize(size)
	for {
		var done uint32
		err := windows.ReadFile(handle, buf, &done, nil)
		if err != nil {
			return nil, wrap("read", path, err)
		}
		if done == 0 {
			return chunks, nil
		}
		chunks = append(chunks, buf[:done]...)
	}
}

func writeHandle(handle windows.Handle, path string, data []byte) error {
	if err := writeAllHandle(handle, path, data); err != nil {
		return err
	}
	if err := windows.FlushFileBuffers(handle); err != nil {
		return wrap("flush", path, err)
	}
	return nil
}

// writeAllHandle writes data without flushing it to stable storage.
func writeAllHandle(handle windows.Handle, path string, data []byte) error {
	for len(data) > 0 {
		var done uint32
		piece := data
		if len(piece) > 1<<20 {
			piece = piece[:1<<20]
		}
		if err := windows.WriteFile(handle, piece, &done, nil); err != nil {
			return wrap("write", path, err)
		}
		if done == 0 {
			return wrap("write", path, errors.New("short write to protected file"))
		}
		data = data[done:]
	}
	return nil
}

// renameHandle renames the open handle to the name of targetPath inside targetParent. Only the two pinned handles take
// part, so the retries below never resolve a path again.
//
// A replacement first asks for POSIX rename semantics, which let the rename succeed while another handle that shares
// DELETE is still open on the target; the classic call fails in that case. A handle that does not share DELETE, such as
// the scan of an antivirus product, still makes the rename fail with a busy error. Such handles are usually short lived,
// so a busy error is retried with a bounded backoff before it is returned.
func renameHandle(handle, targetParent windows.Handle, targetPath string, replace bool) error {
	name := filepath.Base(targetPath)
	if err := validateComponent(name, targetPath); err != nil {
		return err
	}
	encoded, err := windows.UTF16FromString(name)
	if err != nil {
		return wrap("rename", targetPath, err)
	}
	encoded = encoded[:len(encoded)-1]
	for attempt := 0; ; attempt++ {
		err = renameOnce(handle, targetParent, encoded, replace)
		if err == nil {
			return nil
		}
		// Reparse statuses come first so that a status which also maps to a busy errno is never retried.
		if isReparseNTError(err) {
			return mapNTError("rename", targetPath, err)
		}
		if !replace && isExistWin(err) {
			return existError("rename", targetPath, "rename target already exists")
		}
		wait, retry := nextRenameWait(renameBackoff, attempt, isBusyRename(err))
		if !retry {
			return mapNTError("rename", targetPath, err)
		}
		renameStats.busyRetries.Add(1)
		time.Sleep(wait)
	}
}

// isBusyRename reports whether err says that another handle on the target blocks the rename.
func isBusyRename(err error) bool {
	if isReparseNTError(err) {
		return false
	}
	var st windows.NTStatus
	if !errors.As(err, &st) {
		return false
	}
	errno := st.Errno()
	return errno == windows.ERROR_ACCESS_DENIED || errno == windows.ERROR_SHARING_VIOLATION
}

// isRenameExUnsupported reports whether err says the system does not know the Ex information class or one of its flags.
func isRenameExUnsupported(err error) bool {
	var st windows.NTStatus
	if !errors.As(err, &st) {
		return false
	}
	switch st {
	case windows.STATUS_INVALID_INFO_CLASS, windows.STATUS_NOT_IMPLEMENTED, windows.STATUS_NOT_SUPPORTED, windows.STATUS_INVALID_PARAMETER:
		return true
	}
	return false
}

// renameOnce makes one rename attempt on the handle. A replacement tries the POSIX semantics first and falls back to
// the classic call on the same handle when the system does not support them. Nothing is cached between attempts.
func renameOnce(handle, targetParent windows.Handle, name []uint16, replace bool) error {
	if replace {
		err := setRenameInformationEx(handle, targetParent, name)
		if !isRenameExUnsupported(err) {
			return err
		}
		renameStats.classicFallbacks.Add(1)
	}
	return setRenameInformation(handle, targetParent, name, replace)
}

func setRenameInformation(handle, targetParent windows.Handle, name []uint16, replace bool) error {
	var dummy fileRenameInformation
	nameLen := len(name) * 2
	bufferSize := int(unsafe.Offsetof(dummy.FileName)) + nameLen
	buffer := make([]byte, bufferSize)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	if replace {
		info.ReplaceIfExists = 1
	}
	info.RootDirectory = targetParent
	info.FileNameLength = uint32(nameLen)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	return ntSetRename(handle, windows.FileRenameInformation, buffer)
}

func setRenameInformationEx(handle, targetParent windows.Handle, name []uint16) error {
	var dummy fileRenameInformationEx
	nameLen := len(name) * 2
	bufferSize := int(unsafe.Offsetof(dummy.FileName)) + nameLen
	buffer := make([]byte, bufferSize)
	info := (*fileRenameInformationEx)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.RootDirectory = targetParent
	info.FileNameLength = uint32(nameLen)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	return ntSetRename(handle, fileRenameInformationExClass, buffer)
}

// ntSetRename is the only place that hands a rename request to the system, so renameStats.exCalls counts the information
// class that was actually passed.
func ntSetRename(handle windows.Handle, class uint32, buffer []byte) error {
	if class == fileRenameInformationExClass {
		renameStats.exCalls.Add(1)
	}
	var iosb windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(handle, &iosb, &buffer[0], uint32(len(buffer)), class)
}

func openOrCreateFile(parent windows.Handle, name, path string, access uint32, private bool) (windows.Handle, error) {
	for {
		probe, err := tryOpenLeaf(parent, name, path, windows.FILE_READ_ATTRIBUTES, kindFile, true, false)
		if err != nil {
			return 0, err
		}
		if probe != 0 {
			closeHandle(probe)
			return openRelative(parent, name, path, access, false, kindFile, true, false, kindAny)
		}
		priv := kindAny
		if private {
			priv = kindFile
		}
		h, err := openRelative(parent, name, path, access, true, kindFile, true, false, priv)
		if err != nil {
			if isExistWin(err) || isExist(err) {
				continue
			}
			return 0, err
		}
		return h, nil
	}
}
