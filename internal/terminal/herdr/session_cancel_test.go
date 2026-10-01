//go:build !windows

package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/terminal"
)

func TestSessionReportCancellationClosesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		var request map[string]any
		_ = json.NewDecoder(conn).Decode(&request)
		received <- conn
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- reportSession(ctx, terminal.HookCall{Getenv: func(string) string { return path }, Report: &terminal.SessionReport{Agent: "codex", Pane: "p1", Reference: "s1", Now: time.Now, Deadline: time.Now().Add(10 * time.Second)}})
	}()
	select {
	case conn := <-received:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("socket ignored cancellation")
	}
}
