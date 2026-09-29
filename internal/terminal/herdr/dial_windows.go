//go:build windows

package herdr

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const pipePrefix = `\\.\pipe\`

// pipeBusyPoll is the retry interval while every server instance of the pipe is busy.
const pipeBusyPoll = 20 * time.Millisecond

// pipeName maps HERDR_SOCKET_PATH to the herdr named pipe. A value that already names a pipe is used
// unchanged.
func pipeName(socket string) string {
	if len(socket) >= len(pipePrefix) && strings.EqualFold(socket[:len(pipePrefix)], pipePrefix) {
		return socket
	}
	return pipePrefix + socket
}

func dialPlatform(ctx context.Context, socket string) (net.Conn, error) {
	name := pipeName(socket)
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: "pipe", Addr: pipeAddr(name), Err: err}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, &net.OpError{Op: "dial", Net: "pipe", Addr: pipeAddr(name), Err: err}
		}
		// FILE_FLAG_OVERLAPPED lets os.NewFile attach the handle to the runtime poller, which is what
		// makes deadlines and Close-based cancellation work on the returned connection.
		handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			file := os.NewFile(uintptr(handle), name)
			if file == nil {
				_ = windows.CloseHandle(handle)
				return nil, &net.OpError{Op: "dial", Net: "pipe", Addr: pipeAddr(name), Err: windows.ERROR_INVALID_HANDLE}
			}
			return &pipeConn{File: file, addr: pipeAddr(name)}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, &net.OpError{Op: "dial", Net: "pipe", Addr: pipeAddr(name), Err: &os.PathError{Op: "open", Path: name, Err: err}}
		}
		// Every server instance is busy; retry until one is free or the context ends.
		timer := time.NewTimer(pipeBusyPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// pipeConn adapts a named pipe client file to net.Conn. Read, Write, Close, and the deadline methods
// come from os.File.
type pipeConn struct {
	*os.File
	addr pipeAddr
}

func (c *pipeConn) LocalAddr() net.Addr  { return c.addr }
func (c *pipeConn) RemoteAddr() net.Addr { return c.addr }
