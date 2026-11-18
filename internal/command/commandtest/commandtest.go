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
	root := command.NewRoot(func() string { return "kander" })
	root.AddCommand(c)
	return command.Execute(root, append([]string{c.Name()}, args...))
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
