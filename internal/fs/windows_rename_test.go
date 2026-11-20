//go:build windows

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileSupportsPosixUnlinkRename is FILE_SUPPORTS_POSIX_UNLINK_RENAME, which the x/sys release in use does not define.
const fileSupportsPosixUnlinkRename = 0x00000400

const (
	shareWithoutDelete = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE
	shareWithDelete    = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
)

func newRenameTarget(t *testing.T) (root, target string) {
	t.Helper()
	root = t.TempDir()
	target = filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, target
}

func openContender(t *testing.T, path string, share uint32) windows.Handle {
	t.Helper()
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ,
		share,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func readTarget(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWindowsRenameRetriesUntilContenderCloses(t *testing.T) {
	root, target := newRenameTarget(t)
	contender := openContender(t, target, shareWithoutDelete)
	retries := renameStats.busyRetries.Load()
	// The contender is released about 200ms after the replacement was refused as busy once, so the success below can
	// only come from a retry, however long the setup before the first rename takes.
	done := make(chan struct{})
	var released sync.WaitGroup
	released.Add(1)
	go func() {
		defer released.Done()
		for renameStats.busyRetries.Load() == retries {
			select {
			case <-done:
				closeHandle(contender)
				return
			case <-time.After(time.Millisecond):
			}
		}
		time.Sleep(200 * time.Millisecond)
		closeHandle(contender)
	}()
	err := WriteTextAtomic(root, target, "new", true)
	close(done)
	released.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if renameStats.busyRetries.Load() <= retries {
		t.Fatal("the replacement succeeded without a busy retry")
	}
	if got := readTarget(t, target); got != "new" {
		t.Fatalf("got %q, want new content", got)
	}
}

func TestWindowsRenameFailsWhileContenderStaysOpen(t *testing.T) {
	root, target := newRenameTarget(t)
	contender := openContender(t, target, shareWithoutDelete)
	started := time.Now()
	err := WriteTextAtomic(root, target, "new", true)
	elapsed := time.Since(started)
	closeHandle(contender)
	if err == nil {
		t.Fatal("replacement succeeded while a handle without DELETE sharing is open")
	}
	if !IsBusyFile(err) {
		t.Fatalf("got %v, want a busy error", err)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("the retries took %v, want less than 5s", elapsed)
	}
	if got := readTarget(t, target); got != "old" {
		t.Fatalf("got %q, want the original content", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file %q was left behind", entry.Name())
		}
	}
}

func TestWindowsRenameSucceedsAgainstContenderSharingDelete(t *testing.T) {
	root, target := newRenameTarget(t)
	contender := openContender(t, target, shareWithDelete)
	defer closeHandle(contender)
	var flags uint32
	if err := windows.GetVolumeInformationByHandle(contender, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		t.Fatal(err)
	}
	if flags&fileSupportsPosixUnlinkRename == 0 {
		t.Skipf("the volume does not support POSIX rename semantics (file system flags 0x%x)", flags)
	}
	fallbacks := renameStats.classicFallbacks.Load()
	if err := WriteTextAtomic(root, target, "new", true); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != "new" {
		t.Fatalf("got %q, want new content", got)
	}
	if renameStats.classicFallbacks.Load() != fallbacks {
		t.Fatal("the volume reports POSIX rename support, but the replacement used the classic rename")
	}
}

func TestWindowsRenameUsesExInformationClass(t *testing.T) {
	var info fileRenameInformationEx
	if got := unsafe.Offsetof(info.RootDirectory); got != 8 {
		t.Fatalf("RootDirectory offset = %d, want 8", got)
	}
	if got := unsafe.Offsetof(info.FileNameLength); got != 16 {
		t.Fatalf("FileNameLength offset = %d, want 16", got)
	}
	if got := unsafe.Offsetof(info.FileName); got != 20 {
		t.Fatalf("FileName offset = %d, want 20", got)
	}

	root, target := newRenameTarget(t)
	calls := renameStats.exCalls.Load()
	fallbacks := renameStats.classicFallbacks.Load()
	if err := WriteTextAtomic(root, target, "new", true); err != nil {
		t.Fatal(err)
	}
	if renameStats.exCalls.Load() <= calls {
		t.Fatal("the replacement did not use the FileRenameInformationEx class")
	}
	// A volume without POSIX rename support legitimately falls back, so the fallback is only checked where it is declared.
	handle := openContender(t, target, shareWithDelete)
	defer closeHandle(handle)
	var flags uint32
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		t.Fatal(err)
	}
	if flags&fileSupportsPosixUnlinkRename != 0 && renameStats.classicFallbacks.Load() != fallbacks {
		t.Fatal("the volume reports POSIX rename support, but the replacement used the classic rename")
	}
}

func TestWindowsRenameBusyClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		busy bool
	}{
		{"access denied", windows.STATUS_ACCESS_DENIED, true},
		{"sharing violation", windows.STATUS_SHARING_VIOLATION, true},
		{"reparse point", statusReparsePointEncountered, false},
		{"name collision", windows.STATUS_OBJECT_NAME_COLLISION, false},
		{"missing name", windows.STATUS_OBJECT_NAME_NOT_FOUND, false},
		{"not an NT status", errors.New("other"), false},
	} {
		if got := isBusyRename(tc.err); got != tc.busy {
			t.Errorf("%s: isBusyRename = %v, want %v", tc.name, got, tc.busy)
		}
	}
}
