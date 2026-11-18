// Package commandtest runs one command the way the kander binary does, for tests that
// exercise a command of an implementation package without the whole command tree.
package commandtest

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
)

// Run executes c under a fresh root with args and returns the process exit code.
func Run(c *cobra.Command, args ...string) int {
	root := command.NewRoot()
	root.AddCommand(c)
	return command.Execute(root, append([]string{c.Name()}, args...))
}

// Stub replaces what every command of the tree does when it runs with a call of record, and
// keeps the rest as it is: the parsing, the argument validation (Args) and the checks that run
// before the command body (PreRunE). A command line that the real command refuses is refused,
// and one it accepts reaches record with the arguments the command saw. Groups, help, and a
// command that takes its arguments verbatim keep their own bodies.
func Stub(root *cobra.Command, record func(c *cobra.Command, args []string)) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && c.Annotations[command.GroupAnnotation] != "true" && c.Name() != "help" && !c.DisableFlagParsing {
			c.RunE = func(c *cobra.Command, args []string) error {
				record(c, args)
				return nil
			}
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// Capture runs f with the process stdout and stderr redirected and returns what f returned and
// wrote. Commands that print through os.Stdout and os.Stderr, and the error report of Run, are
// observed this way.
func Capture(f func() int) (code int, stdout, stderr string) {
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	read := func(r *os.File) <-chan string {
		done := make(chan string, 1)
		go func() { data, _ := io.ReadAll(r); _ = r.Close(); done <- string(data) }()
		return done
	}
	outDone, errDone := read(outR), read(errR)
	code = f()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return code, <-outDone, <-errDone
}
