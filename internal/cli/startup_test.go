package cli

import (
	"testing"

	"github.com/spf13/cobra"
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
