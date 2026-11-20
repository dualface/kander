//go:build windows

package worker

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/windows"
)

// TestWriteExecutableWaitsForABusyHandle pins the race behind the intermittent
// "Access is denied" of the replacement helper: the worker opens the
// executable without sharing DELETE, and a rename that lands in that window is
// refused until the handle closes.
//
// The test runs in a synctest bubble so the release is tied to the helper's
// own wait. The fake clock moves only while every goroutine of the bubble is
// blocked, and file system calls do not move it, so the first rename always
// meets the open handle at time zero. The handle closes 100ms of fake time
// later, between two retries of internal/fs, and a helper that does not wait
// for it fails.
func TestWriteExecutableWaitsForABusyHandle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &rig{harness: &harness{t: t}}
		r.exe = filepath.Join(t.TempDir(), "kander")
		r.writeExecutable("v1")
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
		go func() {
			time.Sleep(100 * time.Millisecond)
			if err := windows.CloseHandle(handle); err != nil {
				t.Error(err)
			}
		}()
		started := time.Now()
		r.writeExecutable("v2")
		if waited := time.Since(started); waited < 100*time.Millisecond {
			t.Fatalf("the replacement returned after %v, before the handle closed", waited)
		}
		got, err := os.ReadFile(r.exe)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "v2" {
			t.Fatalf("got %q, want v2", got)
		}
	})
}
