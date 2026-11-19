//go:build windows

package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestWriteExecutableWaitsForABusyHandle pins the race behind the intermittent
// "Access is denied" of the replacement helper: the worker opens the
// executable without sharing DELETE, and a rename that lands in that window is
// refused until the handle closes.
func TestWriteExecutableWaitsForABusyHandle(t *testing.T) {
	r := newRig(t)
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(r.exe),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	t.Cleanup(func() { <-released })
	go func() {
		defer close(released)
		// The hold starts only once the helper has written its replacement
		// next to the executable, so the first rename is made while the handle
		// is open however long the scheduler delays the call. The deadline
		// only keeps a helper that writes no sibling from hanging the test.
		// The 250ms hold stays below the last retry of internal/fs (630ms in
		// total) and leaves a bare rename that stalls for up to that long
		// after the sibling appears still refused.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !hasReplacementSibling(r.exe) {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(250 * time.Millisecond)
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	}()
	r.writeExecutable("v2")
	got, err := os.ReadFile(r.exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("got %q, want v2", got)
	}
}

// hasReplacementSibling reports whether the directory of exe holds an entry
// other than the executable and its v1 hard link.
func hasReplacementSibling(exe string) bool {
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if name := entry.Name(); name != filepath.Base(exe) && name != filepath.Base(exe)+".v1" {
			return true
		}
	}
	return false
}
