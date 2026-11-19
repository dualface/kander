//go:build windows

package worker

import (
	"os"
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
		time.Sleep(100 * time.Millisecond)
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
