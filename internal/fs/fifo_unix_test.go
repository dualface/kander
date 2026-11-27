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

// requireFIFORefusal runs op against a FIFO without a writer: it must fail
// within the bound and leave the FIFO where it was.
func requireFIFORefusal(t *testing.T, op func(root, path string) error) {
	t.Helper()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() { done <- op(dir, fifo) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO target was accepted as a regular file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the FIFO open blocked past the bound")
	}
	var st unix.Stat_t
	if err := unix.Lstat(fifo, &st); err != nil {
		t.Fatalf("the FIFO is gone: %v", err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFIFO {
		t.Fatalf("%s is no longer a FIFO: mode %o", fifo, st.Mode)
	}
}

func TestWriteTextAtomicRefusesFIFOTargetWithoutBlocking(t *testing.T) {
	requireFIFORefusal(t, func(root, path string) error {
		return WriteTextAtomic(root, path, "replacement\n", true)
	})
}

func TestRemoveRegularFileIfExistsRefusesFIFOWithoutBlocking(t *testing.T) {
	requireFIFORefusal(t, func(root, path string) error {
		_, err := RemoveRegularFileIfExists(root, path)
		return err
	})
}
