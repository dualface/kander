package launch

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/dualface/kander/v2/internal/probe"
)

func TestRegisteredOrcaWorkspace(t *testing.T) {
	const project = "/project with spaces"
	for _, tc := range []struct {
		name   string
		result probe.Result
		want   bool
	}{
		{"local", probe.Result{Stdout: `{"ok":true,"result":{"worktree":{"address":{"environment":"local","worktree":"folder:abc"}}}}`}, true},
		{"unregistered", probe.Result{Code: 1, Stdout: `{"ok":false,"error":{"code":"selector_not_found"}}`}, false},
		{"unaddressable", probe.Result{Stdout: `{"ok":true,"result":{"worktree":{"address":null}}}`}, false},
		{"remote", probe.Result{Stdout: `{"ok":true,"result":{"worktree":{"address":{"environment":"remote","worktree":"folder:abc"}}}}`}, false},
		{"invalid output", probe.Result{Stdout: "invalid"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, program string, args []string) (probe.Result, error) {
				want := []string{"worktree", "show", "--environment", "local", "--worktree", "path:" + project, "--json"}
				if program != "orca" || !slices.Equal(args, want) {
					t.Fatalf("unexpected command: %s %q", program, args)
				}
				return tc.result, nil
			}
			if got := registeredOrcaWorkspace(t.Context(), run, "orca", project); got != tc.want {
				t.Fatalf("registeredOrcaWorkspace = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOrcaAutoLive(t *testing.T) {
	project := os.Getenv("KANDER_E2E_ORCA_PROJECT")
	if project == "" {
		t.Skip("set KANDER_E2E_ORCA_PROJECT inside a local Orca terminal")
	}
	if got, err := resolveStartLauncher("auto", project); err != nil || got != "orca" {
		t.Fatalf("registered project: launcher = %q, error = %v", got, err)
	}
	if got, err := resolveStartLauncher("auto", t.TempDir()); err == nil || got == "orca" {
		t.Fatalf("unregistered project: launcher = %q, error = %v", got, err)
	}
	t.Setenv("TMUX", "/tmp/fake-tmux-socket,111,0")
	if got, err := resolveStartLauncher("auto", project); err != nil || got != "tmux" {
		t.Fatalf("nested tmux: launcher = %q, error = %v", got, err)
	}
	t.Setenv("TMUX", "")
	t.Setenv("ORCA_TERMINAL_HANDLE", "")
	if got, err := resolveStartLauncher("auto", project); err == nil || got == "orca" {
		t.Fatalf("outside Orca: launcher = %q, error = %v", got, err)
	}
}
