package tmux

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
	return terminal.Conn{Program: "tmux", Run: run}
}

func paneFactsWithin(conn terminal.Conn, pane string, timeout time.Duration) (terminal.PaneFacts, error) {
	ctx, cancel := probe.TimeoutContext(timeout)
	defer cancel()
	return New(Name, os.Getenv).PaneFacts(ctx, conn, pane)
}

func TestTmuxDisplayGonePreservesDetail(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Code: 1, Stderr: "can't find pane: %9"}, nil
	})
	facts, err := paneFactsWithin(conn, "%9", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.Gone {
		t.Fatal("expected gone")
	}
	if facts.GoneDetail != "can't find pane: %9" {
		t.Fatalf("gone=%q", facts.GoneDetail)
	}
}

func TestTmuxIdentityGonePreservesDetail(t *testing.T) {
	resetLang(t)
	calls := 0
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		calls++
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return probe.Result{Code: 1, Stderr: "no server running"}, nil
	})
	facts, err := paneFactsWithin(conn, "%9", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.Gone || facts.GoneDetail != "no server running" {
		t.Fatalf("%+v", facts)
	}
}

func TestTmuxIdentityMissingAndFailureAreDistinct(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		return probe.Result{Code: 1, Stderr: "invalid option: " + option}, nil
	})
	facts, err := paneFactsWithin(conn, "%9", 0)
	if err != nil || facts.Gone || facts.SessionMarker != "" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}

	conn = fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return probe.Result{Code: 1, Stderr: "transport failed"}, nil
	})
	_, err = paneFactsWithin(conn, "%9", 0)
	if err == nil || !strings.Contains(err.Error(), "transport failed") {
		t.Fatalf("err=%v", err)
	}

	for _, detail := range []string{"invalid option: -p", "unknown option: -v"} {
		conn = fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
			if args[0] == "display-message" {
				return probe.Result{Stdout: "codex\t0\t0\n"}, nil
			}
			return probe.Result{Code: 1, Stderr: detail}, nil
		})
		_, err = paneFactsWithin(conn, "%9", 0)
		if err == nil || !strings.Contains(err.Error(), detail) {
			t.Fatalf("detail %s err=%v", detail, err)
		}
	}
}

func TestTmuxDualReadPrefersKanderThenOnevoke(t *testing.T) {
	resetLang(t)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		if option == paneSessionOption {
			return probe.Result{Code: 1, Stderr: "invalid option: " + paneSessionOption}, nil
		}
		return probe.Result{Stdout: "legacy-session\n"}, nil
	})
	facts, err := paneFactsWithin(conn, "%9", 0)
	if err != nil || facts.SessionMarker != "legacy-session" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}

	conn = fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		if option == paneSessionOption {
			return probe.Result{Stdout: "kander-session\n"}, nil
		}
		t.Fatal("should not read legacy when kander exists")
		return probe.Result{}, nil
	})
	facts, err = paneFactsWithin(conn, "%9", 0)
	if err != nil || facts.SessionMarker != "kander-session" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

func TestTmuxContainerProbe(t *testing.T) {
	resetLang(t)
	backend := New(Name, os.Getenv)
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Stdout: "$1\tsess\t@1\t1\n"}, nil
	})
	topology, err := backend.Topology(context.Background(), conn, terminal.Address{Pane: "%1"})
	if err != nil || topology.Session != "$1" || topology.PaneCount != "1" {
		t.Fatalf("%+v %v", topology, err)
	}
	conn = fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		return probe.Result{Code: 1, Stderr: "boom"}, nil
	})
	_, err = backend.Topology(context.Background(), conn, terminal.Address{Pane: "%1"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestTmuxProbePropagatesCallerTimeout(t *testing.T) {
	resetLang(t)
	want := 37 * time.Millisecond
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing deadline")
		}
		timeout := time.Until(deadline)
		if timeout <= 0 || timeout > want {
			t.Fatalf("timeout=%s want (0,%s]", timeout, want)
		}
		if args[0] == "display-message" {
			return probe.Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return probe.Result{Stdout: "session\n"}, nil
	})
	if _, err := paneFactsWithin(conn, "%9", want); err != nil {
		t.Fatal(err)
	}
}

func TestTmuxExpiredFactsDoNotStartMarkerProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	conn := fakeConn(func(ctx context.Context, program string, args []string) (probe.Result, error) {
		calls++
		cancel()
		return probe.Result{Stdout: "codex\t0\t0\n"}, nil
	})
	_, err := New(Name, os.Getenv).PaneFacts(ctx, conn, "%1")
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
