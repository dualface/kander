package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/menu"
)

func TestStartupMaintenanceExemptions(t *testing.T) {
	root := NewRoot()
	for _, tt := range []struct {
		args   []string
		exempt bool
	}{
		{[]string{"version"}, true},
		{[]string{"uninstall"}, true},
		{[]string{"guard-write"}, true},
		{[]string{"doctor"}, false},
		{[]string{"init"}, false},
		{[]string{"config"}, false},
	} {
		c, _, err := root.Find(tt.args)
		if err != nil || c == root {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if got := startupExempt(c); got != tt.exempt {
			t.Errorf("%v: exempt=%v want %v", tt.args, got, tt.exempt)
		}
	}
	if startupExempt(root) {
		t.Fatal("bare kander must run the startup maintenance")
	}
	// The server group and the worker come from other packages; their shape decides.
	server := &cobra.Command{Use: "server"}
	start := &cobra.Command{Use: "start", RunE: func(*cobra.Command, []string) error { return nil }}
	start.Flags().Bool("foreground", false, "")
	server.AddCommand(start)
	worker := &cobra.Command{Use: "worker", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(server, worker)
	if startupExempt(start) {
		t.Fatal("a detached server start must run the startup maintenance")
	}
	if err := start.Flags().Set("foreground", "true"); err != nil {
		t.Fatal(err)
	}
	if !startupExempt(start) || !startupExempt(worker) {
		t.Fatal("the foreground server and the worker are exempt")
	}
}

// stubRefresh replaces the version refresh with one that counts its calls and reports outcome.
func stubRefresh(t *testing.T, outcome menu.VersionRefresh) *int {
	t.Helper()
	calls := new(int)
	old := refreshForVersion
	refreshForVersion = func() menu.VersionRefresh { *calls++; return outcome }
	t.Cleanup(func() { refreshForVersion = old })
	return calls
}

// The exempt commands, help in every form, and bare kander (the TUI refreshes itself) never run
// the version refresh; another command runs it before its body.
func TestVersionRefreshSkipsExemptCommands(t *testing.T) {
	resetLang(t)
	calls := stubRefresh(t, menu.VersionRefresh{Status: menu.VersionRefreshCurrent})
	oldRunner := DefaultRunner
	DefaultRunner = func() int { return 0 }
	t.Cleanup(func() { DefaultRunner = oldRunner })
	for _, args := range [][]string{
		{"kander", "version"}, {"kander", "help"}, {"kander", "help", "doctor"}, {"kander", "--help"},
		{"kander", "doctor", "--help"}, {"kander", "guard-write", "--help"}, {"kander"},
	} {
		if code, _, stderr := captureRun(t, args); code != 0 {
			t.Fatalf("%v: code=%d stderr=%q", args, code, stderr)
		}
		if *calls != 0 {
			t.Fatalf("%v ran the version refresh", args)
		}
	}
	root := NewRoot()
	for _, args := range [][]string{{"uninstall"}, {"guard-write"}, {"version"}} {
		c, _, err := root.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		startupMaintenance(c)
		if *calls != 0 {
			t.Fatalf("%v ran the version refresh", args)
		}
	}
	worker := &cobra.Command{Use: "worker", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(worker)
	startupMaintenance(worker)
	if *calls != 0 {
		t.Fatal("the worker ran the version refresh")
	}
	doctor, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	startupMaintenance(doctor)
	if *calls != 1 {
		t.Fatalf("doctor ran the version refresh %d times", *calls)
	}
}

// With --json the outcome of the refresh is a notice of the result object and changes none of
// the command's own fields; without --json it is one line on stderr and stdout is unchanged.
func TestVersionRefreshOnlyAddsNotices(t *testing.T) {
	resetLang(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.EnvConfig, filepath.Join(home, "config.json"))
	t.Chdir(t.TempDir())
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	run := func(outcome menu.VersionRefresh, args ...string) (map[string]any, string, string) {
		t.Helper()
		calls := stubRefresh(t, outcome)
		code, stdout, stderr := captureRun(t, append([]string{"kander"}, args...))
		if code != 0 || *calls != 1 {
			t.Fatalf("%v: code=%d calls=%d stderr=%q", args, code, *calls, stderr)
		}
		var object map[string]any
		if slices.Contains(args, "--json") {
			if err := json.Unmarshal([]byte(stdout), &object); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, stdout)
			}
		}
		return object, stdout, stderr
	}
	refreshed := menu.VersionRefresh{Status: menu.VersionRefreshDone, Running: "2.0.0", Elapsed: time.Second}
	plain, _, _ := run(menu.VersionRefresh{Status: menu.VersionRefreshCurrent}, "config", "--json")
	withRefresh, _, _ := run(refreshed, "config", "--json")
	notices, _ := withRefresh["notices"].([]any)
	if len(notices) != 1 || notices[0].(map[string]any)["code"] != menu.NoticeVersionRefreshed {
		t.Fatalf("notices = %v", withRefresh["notices"])
	}
	delete(withRefresh, "notices")
	delete(plain, "notices")
	if !reflect.DeepEqual(plain, withRefresh) {
		t.Fatalf("the refresh changed the result object:\n%v\n%v", plain, withRefresh)
	}
	_, plainOut, _ := run(menu.VersionRefresh{Status: menu.VersionRefreshCurrent}, "config")
	_, refreshedOut, stderr := run(refreshed, "config")
	if refreshedOut != plainOut || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "2.0.0") {
		t.Fatalf("stdout changed or stderr is not one line: %q", stderr)
	}
}
