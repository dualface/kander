//go:build windows

package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestPipeNameMapping(t *testing.T) {
	for _, tc := range []struct{ socket, want string }{
		{`C:\Users\me\AppData\Roaming\herdr\herdr.sock`, `\\.\pipe\C:\Users\me\AppData\Roaming\herdr\herdr.sock`},
		{`herdr.sock`, `\\.\pipe\herdr.sock`},
		{`\\.\pipe\herdr`, `\\.\pipe\herdr`},
		{`\\.\PIPE\herdr`, `\\.\PIPE\herdr`},
	} {
		if got := pipeName(tc.socket); got != tc.want {
			t.Errorf("pipeName(%q) = %q, want %q", tc.socket, got, tc.want)
		}
	}
}

// pipeServer is a one-instance named pipe server standing in for herdr. The socket value is what
// HERDR_SOCKET_PATH would hold: a file path that is never created, whose pipe is `\\.\pipe\` + path.
type pipeServer struct {
	socket string
	handle windows.Handle
}

func newPipeServer(t *testing.T) *pipeServer {
	t.Helper()
	socket := filepath.Join(t.TempDir(), fmt.Sprintf("herdr-%d.sock", time.Now().UnixNano()))
	name, err := windows.UTF16PtrFromString(pipeName(socket))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &pipeServer{socket: socket, handle: handle}
}

// serve accepts one client, reads one JSON line, and answers with respond(request) unless it returns
// "". It keeps the pipe open until release is closed.
func (s *pipeServer) serve(respond func(map[string]any) string, release <-chan struct{}) <-chan map[string]any {
	requests := make(chan map[string]any, 1)
	go func() {
		defer close(requests)
		if err := windows.ConnectNamedPipe(s.handle, nil); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			_ = windows.CloseHandle(s.handle)
			return
		}
		file := os.NewFile(uintptr(s.handle), "pipe-server")
		defer file.Close()
		line, _ := bufio.NewReader(file).ReadBytes('\n')
		var request map[string]any
		_ = json.Unmarshal(line, &request)
		requests <- request
		if reply := respond(request); reply != "" {
			_, _ = file.Write([]byte(reply + "\n"))
		}
		<-release
	}()
	return requests
}

func TestFocusPaneOverNamedPipe(t *testing.T) {
	server := newPipeServer(t)
	release := make(chan struct{})
	defer close(release)
	requests := server.serve(func(map[string]any) string {
		return `{"id":"kander-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p3"}}}`
	}, release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := focusPane(ctx, server.socket, "w1:p3"); err != nil {
		t.Fatalf("focus over named pipe: %v", err)
	}
	request := <-requests
	if request["method"] != "pane.focus" || request["params"].(map[string]any)["pane_id"] != "w1:p3" {
		t.Fatalf("request=%v", request)
	}
}

func TestNamedPipeDeadlineStopsSilentServer(t *testing.T) {
	server := newPipeServer(t)
	release := make(chan struct{})
	defer close(release)
	server.serve(func(map[string]any) string { return "" }, release)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := focusPane(ctx, server.socket, "w1:p3")
	if err == nil {
		t.Fatal("silent server answered")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("deadline ignored: %v after %v", err, elapsed)
	}
}

func TestNamedPipeDialErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.sock")
	conn, err := dialChannel(context.Background(), missing)
	if err == nil {
		conn.Close()
		t.Fatal("dial to a missing pipe succeeded")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("missing pipe error lost its cause: %v", err)
	}

	// The only instance is taken, so a second client sees ERROR_PIPE_BUSY until its context ends.
	server := newPipeServer(t)
	release := make(chan struct{})
	defer close(release)
	server.serve(func(map[string]any) string { return "" }, release)
	first, err := dialChannel(context.Background(), server.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	second, err := dialChannel(ctx, server.socket)
	if err == nil {
		second.Close()
		t.Fatal("second client connected to a one-instance pipe")
	}
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("busy pipe did not honor the context: %v after %v", err, time.Since(started))
	}
}
