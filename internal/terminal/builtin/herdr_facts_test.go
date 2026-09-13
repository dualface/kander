package builtin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func herdrPaneFactsWithin(conn terminal.Conn, pane string, timeout time.Duration) (terminal.PaneFacts, error) {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	return herdrBackendForTest().PaneFacts(ctx, conn, pane)
}

func TestHerdrGonePreservesDetail(t *testing.T) {
	resetLang(t)
	detail := `{"error":{"code":"pane_not_found","message":"gone"}}`
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Code: 1, Stderr: detail}, nil
	})
	facts, err := herdrPaneFactsWithin(conn, "w1:p9", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.Gone || facts.GoneDetail != detail {
		t.Fatalf("%+v", facts)
	}
}

func TestHerdrOtherFailureRaises(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Code: 1, Stderr: "fake pane not found"}, nil
	})
	_, err := herdrPaneFactsWithin(conn, "w1:p9", 0)
	if err == nil || !strings.Contains(err.Error(), "pane 不存在") {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesDeadline(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{}, context.DeadlineExceeded
	})
	_, err := herdrPaneFactsWithin(conn, "w1:p9", time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesCancellation(t *testing.T) {
	conn := fakeConn(func(context.Context, string, []string) (probe.Result, error) { return probe.Result{}, context.Canceled })
	_, err := herdrBackendForTest().PaneFacts(context.Background(), conn, "w1:p1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func herdrBackendForTest() terminal.Backend {
	backend, err := DefinitionBackend("herdr", "herdr", os.Getenv)
	if err != nil {
		panic(err)
	}
	return backend
}
