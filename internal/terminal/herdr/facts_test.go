package herdr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
}

func fakeConn(run terminal.Runner) terminal.Conn {
	return terminal.Conn{Program: "herdr", Run: run}
}

func paneFactsWithin(conn terminal.Conn, pane string, timeout time.Duration) (terminal.PaneFacts, error) {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	return New(os.Getenv, nil).PaneFacts(ctx, conn, pane)
}

func TestHerdrGonePreservesDetail(t *testing.T) {
	resetLang(t)
	detail := `{"error":{"code":"pane_not_found","message":"gone"}}`
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Code: 1, Stderr: detail}, nil
	})
	facts, err := paneFactsWithin(conn, "w1:p9", 0)
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
	_, err := paneFactsWithin(conn, "w1:p9", 0)
	if err == nil || !strings.Contains(err.Error(), "pane 不存在") {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesDeadline(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{}, context.DeadlineExceeded
	})
	_, err := paneFactsWithin(conn, "w1:p9", time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesCancellation(t *testing.T) {
	conn := fakeConn(func(context.Context, string, []string) (probe.Result, error) { return probe.Result{}, context.Canceled })
	_, err := New(os.Getenv, nil).PaneFacts(context.Background(), conn, "w1:p1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
