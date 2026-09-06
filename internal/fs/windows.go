//go:build windows

package fs

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
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

func mapNTError(op, path string, err error) error {
	if err == nil {
		return nil
	}
	var st windows.NTStatus
	if errors.As(err, &st) {
		switch st {
		case statusReparsePointEncountered, statusIOReparseTagNotHandled, statusStoppedOnSymlink:
			return failClosed(op, path, "reparse point is not allowed")
		}
		errno := st.Errno()
		if errno == errorCantResolveFilename {
			return failClosed(op, path, "reparse point is not allowed")
		}
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

func tryOpenLeaf(
	parent windows.Handle,
	name, path string,
	access uint32,
	expected objectKind,
	shareWrite, shareDelete bool,
) (windows.Handle, error) {
	probe, err := openRelative(parent, name, path, windows.FILE_READ_ATTRIBUTES, false, kindAny, shareWrite, shareDelete || access&windows.DELETE != 0, kindAny)
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
			// The directory leaf is opened with shared delete: the lease handle of a private temporary directory holds DELETE the whole time,
			// so reopening that directory without FILE_SHARE_DELETE in the share mode would hit a sharing violation.
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
		// Same as above: any component of the chain may be a private temporary directory holding DELETE.
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
	var chunks []byte
	buf := make([]byte, 1<<20)
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
	if err := windows.FlushFileBuffers(handle); err != nil {
		return wrap("flush", path, err)
	}
	return nil
}

func renameHandle(handle, targetParent windows.Handle, targetPath string, replace bool) error {
	name := filepath.Base(targetPath)
	if err := validateComponent(name, targetPath); err != nil {
		return err
	}
	encoded, err := windows.UTF16FromString(name)
	if err != nil {
		return wrap("rename", targetPath, err)
	}
	fileNameLen := len(encoded)*2 - 2
	var dummy fileRenameInformation
	bufferSize := int(unsafe.Offsetof(dummy.FileName)) + fileNameLen
	buffer := make([]byte, bufferSize)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	if replace {
		info.ReplaceIfExists = 1
	}
	info.RootDirectory = targetParent
	info.FileNameLength = uint32(fileNameLen)
	copy(unsafe.Slice(&info.FileName[0], fileNameLen/2), encoded[:len(encoded)-1])
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(handle, &iosb, &buffer[0], uint32(bufferSize), windows.FileRenameInformation)
	if err != nil {
		if !replace && isExistWin(err) {
			return existError("rename", targetPath, "rename target already exists")
		}
		return mapNTError("rename", targetPath, err)
	}
	return nil
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
