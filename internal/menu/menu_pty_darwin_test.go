//go:build darwin

package menu

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"testing"
	"unsafe"
)

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []uintptr{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, master.Fd(), operation, 0); errno != 0 {
			_ = master.Close()
			t.Fatal(errno)
		}
	}
	name, err := ptsName(master)
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	_ = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 40, Col: 120})
	return master, slave
}
func ptsName(master *os.File) (string, error) {
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, master.Fd(), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end <= 0 {
		return "", errors.New("kernel returned an invalid PTY name")
	}
	return string(name[:end]), nil
}
