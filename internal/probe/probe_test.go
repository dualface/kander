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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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

	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
		withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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

	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
		return Result{Code: 1, Stderr: "fake pane not found"}, nil
	})
	_, err := ProbeHerdrPane("herdr", "w1:p9", 0)
	if err == nil || !strings.Contains(err.Error(), "pane 不存在") {
		t.Fatalf("err=%v", err)
	}
}

func TestHerdrProbePreservesDeadline(t *testing.T) {
	resetLang(t)
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
		return Result{}, context.DeadlineExceeded
	})
	_, err := ProbeHerdrPane("herdr", "w1:p9", time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestTmuxContainerProbe(t *testing.T) {
	resetLang(t)
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
		return Result{Stdout: "$1\tsess\t@1\t1\n"}, nil
	})
	facts, err := ProbeTmuxContainer("tmux", "%1")
	if err != nil || facts.SessionID != "$1" || facts.PaneCount != "1" {
		t.Fatalf("%+v %v", facts, err)
	}
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
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
	withRun(t, func(ctx context.Context, program string, args []string) (Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing deadline")
		}
		timeout := time.Until(deadline)
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

func TestContextBudgetDefaultsAndInheritance(t *testing.T) {
	for _, duration := range []time.Duration{0, 25 * time.Millisecond, 30 * time.Second} {
		parent := context.Background()
		parentCancel := func() {}
		if duration != 0 {
			parent, parentCancel = context.WithTimeout(parent, duration)
		}
		ctx, cancel := WithDefaultTimeout(parent)
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing default deadline")
		}
		if duration == 0 {
			if remaining := time.Until(deadline); remaining <= 0 || remaining > DefaultCommandTimeout {
				t.Fatalf("remaining=%s", remaining)
			}
		} else if inherited, _ := parent.Deadline(); deadline != inherited {
			t.Fatalf("deadline reset: %s != %s", deadline, inherited)
		}
		cancel()
		parentCancel()
	}
}

func TestHerdrProbePreservesCancellation(t *testing.T) {
	withRun(t, func(context.Context, string, []string) (Result, error) { return Result{}, context.Canceled })
	_, err := ProbeHerdrPaneContext(context.Background(), "herdr", "w1:p1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancellationMessagesInAllLanguages(t *testing.T) {
	for _, test := range []struct{ language, deadline, canceled string }{
		{"cn", "探测期限已耗尽", "探测已取消"},
		{"en", "Probe deadline exhausted", "Probe canceled"},
		{"ja", "プローブの期限を超過", "プローブをキャンセル"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Setenv(config.EnvLang, test.language)
			t.Setenv(config.EnvLangCLI, "1")
			config.ApplyLanguageArgument([]string{"kander", "--lang", test.language})
			defer config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
			for _, item := range []struct {
				err  error
				want string
			}{{context.DeadlineExceeded, test.deadline}, {context.Canceled, test.canceled}} {
				detail := FailureDetail(item.err)
				if !strings.Contains(detail, item.want) || !strings.Contains(detail, item.err.Error()) {
					t.Fatalf("detail=%q", detail)
				}
			}
		})
	}
}
