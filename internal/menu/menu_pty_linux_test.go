//go:build linux

package menu

import (
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"testing"
	"unsafe"
)

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := int(master.Fd())
	var unlock int32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCSPTLCK), uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	var n uint32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	name := "/dev/pts/" + strconv.FormatUint(uint64(n), 10)
	slave, err = os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	ws := unix.Winsize{Row: 40, Col: 120}
	_ = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &ws)
	return master, slave
}

func ptsName(master *os.File) (string, error) {
	var n uint32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(master.Fd()), uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		return "", errno
	}
	return "/dev/pts/" + strconv.FormatUint(uint64(n), 10), nil
}
