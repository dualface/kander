//go:build unix

package fs

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A FIFO or another special file must fail the open instead of blocking the
// reader: the leaf opens non-blocking and requireRegular rejects it.
func TestOpenRegularFileIfExistsRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := OpenRegularFileIfExists(dir, fifo)
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO must not open as a regular file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO open blocked past the bound")
	}
}
