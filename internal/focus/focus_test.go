package focus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func TestWindowFocus(t *testing.T) {
	for _, tc := range []struct {
		name, address, fault, message string
		success                       bool
		want                          []string
	}{
		{"herdr", "herdr:w1:t2:w1:p3", "", "focus.success", true, []string{"probe-herdr w1:p3", "tab focus w1:t2", "pane-focus w1:p3"}},
		{"short-herdr", "herdr:t2:p3", "", "focus.success", true, []string{"probe-herdr p3", "tab focus t2", "pane-focus p3"}},
		{"tmux", "tmux:$1:@2:%3", "", "focus.success", true, []string{"probe-tmux %3", "select-window -t $1:@2", "select-pane -t %3", "switch-client -t $1"}},
		{"tmux-session", "tmux-session:project:@2:%3", "", "focus.success", true, []string{"probe-tmux %3", "select-window -t project:@2", "select-pane -t %3", "switch-client -t project"}},
		{"herdr-gone", "herdr:t2:p3", "gone", "focus.closed", false, []string{"probe-herdr p3"}},
		{"tmux-gone", "tmux:$1:@2:%3", "gone", "focus.closed", false, []string{"probe-tmux %3"}},
		{"tmux-dead", "tmux:$1:@2:%3", "dead", "focus.closed", false, []string{"probe-tmux %3"}},
		{"empty", "", "", "focus.no_window", false, nil},
		{"foreground", "foreground", "", "focus.unsupported", false, nil},
		{"console", "console:::", "", "focus.unsupported", false, nil},
		{"malformed", "herdr:w1:t2:p3", "", "focus.invalid_window", false, nil},
		{"empty-pane", "tmux:$1:@2:", "", "focus.invalid_window", false, nil},
		{"control", "herdr:t2:p3\n", "", "focus.invalid_window", false, nil},
		{"herdr-missing", "herdr:t2:p3", "missing", "focus.command_missing", false, nil},
		{"tmux-missing", "tmux:$1:@2:%3", "missing", "focus.command_missing", false, nil},
		{"outside-tmux", "tmux:$1:@2:%3", "outside", "focus.outside_tmux", false, nil},
		{"herdr-probe-error", "herdr:t2:p3", "probe", "focus.probe_failed", false, []string{"probe-herdr p3"}},
		{"tmux-probe-error", "tmux:$1:@2:%3", "probe", "focus.probe_failed", false, []string{"probe-tmux %3"}},
		{"tab-error", "herdr:t2:p3", "tab", "focus.switch_failed", false, []string{"probe-herdr p3", "tab focus t2"}},
		{"pane-best-effort", "herdr:t2:p3", "socket", "focus.tab_only", true, []string{"probe-herdr p3", "tab focus t2", "pane-focus p3"}},
		{"select-error", "tmux:$1:@2:%3", "select-window", "focus.switch_failed", false, []string{"probe-tmux %3", "select-window -t $1:@2"}},
		{"pane-error", "tmux:$1:@2:%3", "select-pane", "focus.switch_failed", false, []string{"probe-tmux %3", "select-window -t $1:@2", "select-pane -t %3"}},
		{"switch-error", "tmux:$1:@2:%3", "switch-client", "focus.switch_failed", false, []string{"probe-tmux %3", "select-window -t $1:@2", "select-pane -t %3", "switch-client -t $1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			check := func(ctx context.Context) {
				t.Helper()
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
			}
			e := executor{
				lookup: func(name string) (string, error) {
					if tc.fault == "missing" {
						return "", errors.New("missing")
					}
					return name, nil
				},
				getenv: func(key string) string {
					if tc.fault == "outside" {
						return ""
					}
					return "test"
				},
				run: func(ctx context.Context, name string, args []string) (probe.Result, error) {
					check(ctx)
					calls = append(calls, strings.Join(args, " "))
					if args[0] == tc.fault {
						return probe.Result{Code: 1, Stderr: "failed"}, nil
					}
					return probe.Result{}, nil
				},
				herdr: func(ctx context.Context, name, pane string) (probe.HerdrPaneProbe, error) {
					check(ctx)
					calls = append(calls, "probe-herdr "+pane)
					if tc.fault == "probe" {
						return probe.HerdrPaneProbe{}, errors.New("failed")
					}
					if tc.fault == "gone" {
						return probe.HerdrPaneProbe{GoneDetail: "gone"}, nil
					}
					return probe.HerdrPaneProbe{Pane: map[string]any{"pane_id": pane}}, nil
				},
				tmux: func(ctx context.Context, name, pane string) (probe.TmuxPaneProbe, error) {
					check(ctx)
					calls = append(calls, "probe-tmux "+pane)
					if tc.fault == "probe" {
						return probe.TmuxPaneProbe{}, errors.New("failed")
					}
					if tc.fault == "gone" {
						return probe.TmuxPaneProbe{GoneDetail: "gone"}, nil
					}
					dead := "0"
					if tc.fault == "dead" {
						dead = "1"
					}
					return probe.TmuxPaneProbe{Facts: &probe.TmuxPaneFacts{Dead: dead}}, nil
				},
				paneFocus: func(ctx context.Context, socket, pane string) error {
					check(ctx)
					calls = append(calls, "pane-focus "+pane)
					if tc.fault == "socket" {
						return errors.New("failed")
					}
					return nil
				},
			}
			got := e.focus(context.Background(), tc.address)
			// Compare the localized prefix when a notice also carries external diagnostics.
			want := strings.Split(config.Text(tc.message, "DETAIL"), "DETAIL")[0]
			if got.Success != tc.success || !strings.HasPrefix(got.Message, want) {
				t.Fatalf("result=%+v; want success=%v, prefix=%q", got, tc.success, want)
			}
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("calls=%q; want=%q", calls, tc.want)
			}
		})
	}
}

func TestExecuteDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result probe.Result
		err    error
		want   string
	}{
		{"launch", probe.Result{}, errors.New("spawn failed"), "spawn failed"},
		{"stdout", probe.Result{Code: 1, Stdout: "rejected"}, nil, "rejected"},
		{"exit", probe.Result{Code: 7}, nil, "exit 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := executor{run: func(context.Context, string, []string) (probe.Result, error) { return tc.result, tc.err }}
			err := e.execute(context.Background(), "herdr", []string{"tab"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
