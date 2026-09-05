package probe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
}

func withRun(t *testing.T, fn runFunc) {
	t.Helper()
	orig := runCommand
	runCommand = fn
	t.Cleanup(func() { runCommand = orig })
}

func TestTmuxDisplayGonePreservesDetail(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{Code: 1, Stderr: "can't find pane: %9"}, nil
	})
	probe, err := ProbeTmuxPane("tmux", "%9")
	if err != nil {
		t.Fatal(err)
	}
	if probe.Facts != nil {
		t.Fatal("expected gone")
	}
	if probe.GoneDetail != "can't find pane: %9" {
		t.Fatalf("gone=%q", probe.GoneDetail)
	}
	facts, err := TmuxPaneFactsOf("tmux", "%9")
	if err != nil || facts != nil {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
}

func TestTmuxIdentityGonePreservesDetail(t *testing.T) {
	resetLang(t)
	calls := 0
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		calls++
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return Result{Code: 1, Stderr: "no server running"}, nil
	})
	probe, err := ProbeTmuxPane("tmux", "%9")
	if err != nil {
		t.Fatal(err)
	}
	if probe.Facts != nil || probe.GoneDetail != "no server running" {
		t.Fatalf("%+v", probe)
	}
}

func TestTmuxIdentityMissingAndFailureAreDistinct(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		return Result{Code: 1, Stderr: "invalid option: " + option}, nil
	})
	facts, err := TmuxPaneFactsOf("tmux", "%9")
	if err != nil || facts == nil || facts.SessionMarker != "" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}

	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return Result{Code: 1, Stderr: "transport failed"}, nil
	})
	_, err = TmuxPaneFactsOf("tmux", "%9")
	if err == nil || !strings.Contains(err.Error(), "transport failed") {
		t.Fatalf("err=%v", err)
	}

	for _, detail := range []string{"invalid option: -p", "unknown option: -v"} {
		withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
			if args[0] == "display-message" {
				return Result{Stdout: "codex\t0\t0\n"}, nil
			}
			return Result{Code: 1, Stderr: detail}, nil
		})
		_, err = TmuxPaneFactsOf("tmux", "%9")
		if err == nil || !strings.Contains(err.Error(), detail) {
			t.Fatalf("detail %s err=%v", detail, err)
		}
	}
}

func TestTmuxDualReadPrefersKanderThenOnevoke(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		if option == PaneSessionOption {
			return Result{Code: 1, Stderr: "invalid option: " + PaneSessionOption}, nil
		}
		return Result{Stdout: "legacy-session\n"}, nil
	})
	facts, err := TmuxPaneFactsOf("tmux", "%9")
	if err != nil || facts.SessionMarker != "legacy-session" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}

	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		option := args[len(args)-1]
		if option == PaneSessionOption {
			return Result{Stdout: "kander-session\n"}, nil
		}
		t.Fatal("should not read legacy when kander exists")
		return Result{}, nil
	})
	facts, err = TmuxPaneFactsOf("tmux", "%9")
	if err != nil || facts.SessionMarker != "kander-session" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

func TestHerdrGonePreservesDetail(t *testing.T) {
	resetLang(t)
	detail := `{"error":{"code":"pane_not_found","message":"gone"}}`
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{Code: 1, Stderr: detail}, nil
	})
	probe, err := ProbeHerdrPane("herdr", "w1:p9", 0)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Pane != nil || probe.GoneDetail != detail {
		t.Fatalf("%+v", probe)
	}
	pane, err := HerdrProbePane("herdr", "w1:p9", 0)
	if err != nil || pane != nil {
		t.Fatalf("pane=%v err=%v", pane, err)
	}
}

func TestHerdrOtherFailureRaises(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{Code: 1, Stderr: "fake pane not found"}, nil
	})
	_, err := ProbeHerdrPane("herdr", "w1:p9", 0)
	if err == nil || !strings.Contains(err.Error(), "pane 不存在") {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesDeadline(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{}, context.DeadlineExceeded
	})
	_, err := ProbeHerdrPane("herdr", "w1:p9", time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestTmuxContainerProbe(t *testing.T) {
	resetLang(t)
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{Stdout: "$1\tsess\t@1\t1\n"}, nil
	})
	facts, err := ProbeTmuxContainer("tmux", "%1")
	if err != nil || facts.SessionID != "$1" || facts.PaneCount != "1" {
		t.Fatalf("%+v %v", facts, err)
	}
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		return Result{Code: 1, Stderr: "boom"}, nil
	})
	_, err = ProbeTmuxContainer("tmux", "%1")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestTmuxProbePropagatesCallerTimeout(t *testing.T) {
	resetLang(t)
	want := 37 * time.Millisecond
	withRun(t, func(program string, args []string, timeout time.Duration) (Result, error) {
		if timeout <= 0 || timeout > want {
			t.Fatalf("timeout=%s want (0,%s]", timeout, want)
		}
		if args[0] == "display-message" {
			return Result{Stdout: "codex\t0\t0\n"}, nil
		}
		return Result{Stdout: "session\n"}, nil
	})
	if _, err := ProbeTmuxPaneWithin("tmux", "%9", want); err != nil {
		t.Fatal(err)
	}
}
